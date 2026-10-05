// Package assemblyai implements AssemblyAI Universal Streaming transcription.
package assemblyai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/coder/websocket"
	"github.com/leamout/ai-providers/internal/transport"
	"github.com/leamout/sdk/ai"
)

const DefaultEndpoint = "wss://streaming.assemblyai.com/v3/ws"

type Config struct {
	Endpoint    string `json:"endpoint,omitempty"`
	SpeechModel string `json:"speech_model,omitempty"`
	FormatTurns bool   `json:"format_turns,omitempty"`
}
type Provider struct{ HTTPClient *http.Client }

func (Provider) Descriptor() ai.Descriptor {
	return ai.Descriptor{ID: "assemblyai", Name: "AssemblyAI", Kind: ai.KindSTT, Capabilities: []ai.Capability{ai.CapabilityStreaming, ai.CapabilityTurnDetection}}
}
func config(raw json.RawMessage) (Config, error) {
	c := Config{Endpoint: DefaultEndpoint, SpeechModel: "universal-streaming-english"}
	if len(raw) > 0 {
		if e := json.Unmarshal(raw, &c); e != nil {
			return c, e
		}
	}
	if e := transport.Endpoint(c.Endpoint, "wss"); e != nil {
		return c, e
	}
	if c.SpeechModel == "" {
		return c, errors.New("assemblyai speech_model is required")
	}
	return c, nil
}
func (Provider) ValidateConfig(b json.RawMessage) error { _, e := config(b); return e }
func (p Provider) VerifyCredential(ctx context.Context, key string) error {
	if strings.TrimSpace(key) == "" {
		return errors.New("assemblyai credential is required")
	}
	return transport.Verify(ctx, p.HTTPClient, "https://api.assemblyai.com/v2/transcript?limit=1", http.Header{"Authorization": {key}})
}

type stream struct {
	ws          *transport.WS
	format      ai.AudioFormat
	events      chan ai.STTEvent
	done        chan struct{}
	mu          sync.Mutex
	closing     bool
	formatTurns bool
}

func (p Provider) StartSTT(ctx context.Context, r ai.STTRequest) (ai.STTStream, error) {
	c, e := config(r.Runtime.Config)
	if e != nil {
		return nil, e
	}
	if strings.TrimSpace(r.Runtime.Credential) == "" {
		return nil, errors.New("assemblyai credential is required")
	}
	if e := r.Format.Validate(); e != nil {
		return nil, e
	}
	if r.Format.Channels != 1 || (r.Format.Encoding != ai.AudioEncodingPCM16LE && r.Format.Encoding != ai.AudioEncodingMuLaw) {
		return nil, errors.New("assemblyai requires mono PCM16 or mulaw")
	}
	if r.Format.SampleRateHz < 8000 || r.Format.SampleRateHz > 48000 {
		return nil, errors.New("assemblyai sample rate must be 8000–48000 Hz")
	}
	if r.Language != "" {
		return nil, errors.New("assemblyai language is selected by speech_model, not request language")
	}
	u, _ := url.Parse(c.Endpoint)
	q := u.Query()
	q.Set("sample_rate", strconv.Itoa(r.Format.SampleRateHz))
	encoding := "pcm_s16le"
	if r.Format.Encoding == ai.AudioEncodingMuLaw {
		encoding = "pcm_mulaw"
	}
	q.Set("encoding", encoding)
	q.Set("speech_model", c.SpeechModel)
	q.Set("format_turns", strconv.FormatBool(c.FormatTurns))
	u.RawQuery = q.Encode()
	ws, e := transport.Dial(ctx, p.HTTPClient, u.String(), http.Header{"Authorization": {r.Runtime.Credential}})
	if e != nil {
		return nil, e
	}
	s := &stream{ws: ws, format: r.Format, formatTurns: c.FormatTurns, events: make(chan ai.STTEvent, 32), done: make(chan struct{})}
	go s.read()
	return s, nil
}
func (s *stream) SendAudio(ctx context.Context, f ai.AudioFrame) error {
	if e := f.Validate(); e != nil {
		return e
	}
	if f.Format != s.format {
		return errors.New("assemblyai audio format changed")
	}
	d := f.Duration()
	if d < 50e6 || d > 1000e6 {
		return errors.New("assemblyai audio chunks must contain 50–1000 ms")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		return errors.New("assemblyai stream is closing")
	}
	return s.ws.Write(ctx, websocket.MessageBinary, f.Data)
}
func (s *stream) Finalize(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		return errors.New("assemblyai stream is closing")
	}
	return s.ws.JSON(ctx, map[string]string{"type": "ForceEndpoint"})
}
func (s *stream) Events() <-chan ai.STTEvent { return s.events }
func (s *stream) Close(ctx context.Context) error {
	if ctx == nil {
		return errors.New("context is required")
	}
	s.mu.Lock()
	if !s.closing {
		s.closing = true
		if e := s.ws.JSON(ctx, map[string]string{"type": "Terminate"}); e != nil {
			s.mu.Unlock()
			s.ws.Close()
			return e
		}
	}
	s.mu.Unlock()
	select {
	case <-s.done:
		return s.ws.Close()
	case <-ctx.Done():
		s.ws.Close()
		return ctx.Err()
	}
}
func (s *stream) emit(e ai.STTEvent) bool {
	select {
	case s.events <- e:
		return true
	case <-s.ws.Ctx.Done():
		return false
	}
}
func (s *stream) read() {
	defer close(s.done)
	defer close(s.events)
	defer s.ws.Close()
	id := ""
	for {
		var v struct {
			Type       string `json:"type"`
			ID         string `json:"id"`
			Transcript string `json:"transcript"`
			End        bool   `json:"end_of_turn"`
			Formatted  bool   `json:"turn_is_formatted"`
			Error      string `json:"error"`
		}
		if e := s.ws.Read(&v); e != nil {
			if s.ws.Ctx.Err() == nil {
				s.emit(ai.STTEvent{Type: ai.STTEventError, Err: e})
			}
			return
		}
		if v.Error != "" {
			s.emit(ai.STTEvent{Type: ai.STTEventError, Err: fmt.Errorf("assemblyai: %s", v.Error)})
			return
		}
		switch v.Type {
		case "Begin":
			id = v.ID
		case "Turn":
			if v.Transcript != "" {
				kind := ai.STTEventTranscriptDelta
				if v.End && (!s.formatTurns || v.Formatted) {
					kind = ai.STTEventTranscriptFinal
				}
				if !s.emit(ai.STTEvent{Type: kind, Text: v.Transcript, ProviderID: id}) {
					return
				}
			}
			if v.End && !v.Formatted {
				s.emit(ai.STTEvent{Type: ai.STTEventSpeechStopped, ProviderID: id})
			}
		case "Termination":
			return
		}
	}
}

var _ ai.STT = Provider{}
var _ ai.ConfigValidator = Provider{}
var _ ai.CredentialVerifier = Provider{}
