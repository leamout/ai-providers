// Package google implements Google Cloud Speech-to-Text streaming recognition.
package google

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"

	speechpb "cloud.google.com/go/speech/apiv1/speechpb"
	"github.com/leamout/sdk/ai"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
)

const DefaultEndpoint = "speech.googleapis.com:443"

type Config struct {
	Endpoint       string `json:"endpoint,omitempty"`
	Language       string `json:"language,omitempty"`
	Model          string `json:"model,omitempty"`
	ProjectID      string `json:"project_id,omitempty"`
	InterimResults bool   `json:"interim_results"`
}

// STT accepts a Google OAuth access token in Runtime.Credential. Client may be
// supplied to reuse an authenticated gRPC connection (for example with ADC).
type STT struct{ Client speechpb.SpeechClient }

func (STT) Descriptor() ai.Descriptor {
	return ai.Descriptor{ID: "google", Name: "Google", Kind: ai.KindSTT, Capabilities: []ai.Capability{ai.CapabilityStreaming}}
}
func config(b json.RawMessage) (Config, error) {
	c := Config{Endpoint: DefaultEndpoint, Language: "en-US", InterimResults: true}
	if len(b) > 0 {
		if e := json.Unmarshal(b, &c); e != nil {
			return c, e
		}
	}
	if c.Endpoint == "" || strings.ContainsAny(c.Endpoint, "/?#@ ") {
		return c, errors.New("google endpoint must be a gRPC hostname with optional port")
	}
	if c.Language == "" {
		return c, errors.New("google language is required")
	}
	return c, nil
}
func (STT) ValidateConfig(b json.RawMessage) error { _, e := config(b); return e }

type stream struct {
	rpc    speechpb.Speech_StreamingRecognizeClient
	conn   *grpc.ClientConn
	ctx    context.Context
	cancel context.CancelFunc
	format ai.AudioFormat
	events chan ai.STTEvent
	mu     sync.Mutex
	final  bool
	once   sync.Once
}

func (p STT) StartSTT(ctx context.Context, r ai.STTRequest) (ai.STTStream, error) {
	if ctx == nil {
		return nil, errors.New("context is required")
	}
	c, e := config(r.Runtime.Config)
	if e != nil {
		return nil, e
	}
	if e := r.Format.Validate(); e != nil {
		return nil, e
	}
	if r.Format.Channels != 1 {
		return nil, errors.New("google STT requires mono audio")
	}
	if r.Format.SampleRateHz < 8000 || r.Format.SampleRateHz > 48000 {
		return nil, errors.New("google sample rate must be 8000–48000 Hz")
	}
	encoding := speechpb.RecognitionConfig_LINEAR16
	switch r.Format.Encoding {
	case ai.AudioEncodingPCM16LE:
	case ai.AudioEncodingMuLaw:
		encoding = speechpb.RecognitionConfig_MULAW
	default:
		return nil, errors.New("google streaming supports PCM16 or mulaw")
	}
	if r.Language != "" {
		c.Language = r.Language
	}
	client := p.Client
	var conn *grpc.ClientConn
	if client == nil {
		if strings.TrimSpace(r.Runtime.Credential) == "" {
			return nil, errors.New("google OAuth access token or authenticated client is required")
		}
		conn, e = grpc.NewClient(c.Endpoint, grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS12})))
		if e != nil {
			return nil, e
		}
		client = speechpb.NewSpeechClient(conn)
	}
	streamCtx, cancel := context.WithCancel(ctx)
	if r.Runtime.Credential != "" {
		streamCtx = metadata.AppendToOutgoingContext(streamCtx, "authorization", "Bearer "+r.Runtime.Credential)
	}
	if c.ProjectID != "" {
		streamCtx = metadata.AppendToOutgoingContext(streamCtx, "x-goog-user-project", c.ProjectID)
	}
	rpc, e := client.StreamingRecognize(streamCtx)
	if e != nil {
		cancel()
		if conn != nil {
			conn.Close()
		}
		return nil, e
	}
	s := &stream{rpc: rpc, conn: conn, ctx: streamCtx, cancel: cancel, format: r.Format, events: make(chan ai.STTEvent, 32)}
	e = rpc.Send(&speechpb.StreamingRecognizeRequest{StreamingRequest: &speechpb.StreamingRecognizeRequest_StreamingConfig{StreamingConfig: &speechpb.StreamingRecognitionConfig{Config: &speechpb.RecognitionConfig{Encoding: encoding, SampleRateHertz: int32(r.Format.SampleRateHz), LanguageCode: c.Language, Model: c.Model, AudioChannelCount: 1}, InterimResults: c.InterimResults}}})
	if e != nil {
		s.Close(ctx)
		return nil, e
	}
	go s.read()
	return s, nil
}
func (s *stream) SendAudio(ctx context.Context, f ai.AudioFrame) error {
	if ctx == nil {
		return errors.New("context is required")
	}
	if e := ctx.Err(); e != nil {
		return e
	}
	if e := f.Validate(); e != nil {
		return e
	}
	if f.Format != s.format {
		return errors.New("google audio format changed")
	}
	if len(f.Data) > 25<<10 {
		return errors.New("google streaming audio message exceeds 25 KiB")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.final || s.ctx.Err() != nil {
		return errors.New("google stream is closed or finalized")
	}
	// gRPC send uses the session context. Cancel the session if this call
	// times out while blocked in flow control.
	stop := context.AfterFunc(ctx, s.cancel)
	defer stop()
	return s.rpc.Send(&speechpb.StreamingRecognizeRequest{StreamingRequest: &speechpb.StreamingRecognizeRequest_AudioContent{AudioContent: f.Data}})
}

// Finalize half-closes the RPC; create another stream to recognize more audio.
func (s *stream) Finalize(ctx context.Context) error {
	if ctx == nil {
		return errors.New("context is required")
	}
	if e := ctx.Err(); e != nil {
		return e
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.final {
		return nil
	}
	s.final = true
	return s.rpc.CloseSend()
}
func (s *stream) Events() <-chan ai.STTEvent { return s.events }
func (s *stream) Close(ctx context.Context) error {
	s.once.Do(func() {
		s.cancel()
		if s.conn != nil {
			s.conn.Close()
		}
	})
	return nil
}
func (s *stream) emit(e ai.STTEvent) bool {
	select {
	case s.events <- e:
		return true
	case <-s.ctx.Done():
		return false
	}
}
func (s *stream) read() {
	defer close(s.events)
	defer s.Close(context.Background())
	for {
		v, e := s.rpc.Recv()
		if e != nil {
			if e != io.EOF && s.ctx.Err() == nil {
				s.emit(ai.STTEvent{Type: ai.STTEventError, Err: e})
			}
			return
		}
		if v.Error != nil && v.Error.Code != 0 {
			s.emit(ai.STTEvent{Type: ai.STTEventError, Err: errors.New(v.Error.Message)})
			return
		}
		for _, result := range v.Results {
			if len(result.Alternatives) == 0 {
				continue
			}
			kind := ai.STTEventTranscriptDelta
			if result.IsFinal {
				kind = ai.STTEventTranscriptFinal
			}
			if !s.emit(ai.STTEvent{Type: kind, Text: result.Alternatives[0].Transcript}) {
				return
			}
		}
	}
}

var _ ai.STT = STT{}
var _ ai.ConfigValidator = STT{}
