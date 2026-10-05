package azure

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/leamout/ai-providers/internal/transport"
	"github.com/leamout/sdk/ai"
)

type STTConfig struct {
	Region   string `json:"region"`
	Endpoint string `json:"endpoint,omitempty"`
	Language string `json:"language,omitempty"`
}
type STT struct{ HTTPClient *http.Client }

func (STT) Descriptor() ai.Descriptor {
	return ai.Descriptor{ID: "azure", Name: "Azure", Kind: ai.KindSTT, Capabilities: []ai.Capability{ai.CapabilityStreaming, ai.CapabilityTurnDetection}}
}
func sttConfig(b json.RawMessage) (STTConfig, error) {
	c := STTConfig{Language: "en-US"}
	if len(b) > 0 {
		if e := json.Unmarshal(b, &c); e != nil {
			return c, e
		}
	}
	if c.Endpoint == "" {
		if !regionPattern.MatchString(c.Region) {
			return c, errors.New("azure region is required")
		}
		c.Endpoint = "wss://" + c.Region + ".stt.speech.microsoft.com/speech/recognition/conversation/cognitiveservices/v1"
	}
	if e := transport.Endpoint(c.Endpoint, "wss"); e != nil {
		return c, e
	}
	if c.Language == "" {
		return c, errors.New("azure language is required")
	}
	return c, nil
}
func (STT) ValidateConfig(b json.RawMessage) error { _, e := sttConfig(b); return e }

type sttStream struct {
	ws        *transport.WS
	format    ai.AudioFormat
	events    chan ai.STTEvent
	done      chan struct{}
	mu        sync.Mutex
	requestID string
	final     bool
}

func (p STT) StartSTT(ctx context.Context, r ai.STTRequest) (ai.STTStream, error) {
	c, e := sttConfig(r.Runtime.Config)
	if e != nil {
		return nil, e
	}
	if strings.TrimSpace(r.Runtime.Credential) == "" {
		return nil, errors.New("azure subscription key is required")
	}
	if r.Format.Encoding != ai.AudioEncodingPCM16LE || r.Format.Channels != 1 || (r.Format.SampleRateHz != 8000 && r.Format.SampleRateHz != 16000) {
		return nil, errors.New("azure STT requires PCM16 mono at 8000 or 16000 Hz")
	}
	if r.Language != "" {
		c.Language = r.Language
	}
	u, _ := url.Parse(c.Endpoint)
	q := u.Query()
	q.Set("language", c.Language)
	q.Set("format", "simple")
	u.RawQuery = q.Encode()
	ws, e := transport.Dial(ctx, p.HTTPClient, u.String(), http.Header{"Ocp-Apim-Subscription-Key": {r.Runtime.Credential}})
	if e != nil {
		return nil, e
	}
	s := &sttStream{ws: ws, format: r.Format, events: make(chan ai.STTEvent, 32), done: make(chan struct{})}
	if e := s.begin(ctx); e != nil {
		ws.Close()
		return nil, e
	}
	go s.read()
	return s, nil
}
func (s *sttStream) header(path, content string) string {
	return "Path: " + path + "\r\nX-RequestId: " + s.requestID + "\r\nX-Timestamp: " + time.Now().UTC().Format(time.RFC3339Nano) + "\r\nContent-Type: " + content + "\r\n\r\n"
}
func (s *sttStream) text(ctx context.Context, path string, v any) error {
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	return s.ws.Write(ctx, websocket.MessageText, append([]byte(s.header(path, "application/json")), b...))
}
func (s *sttStream) audio(ctx context.Context, b []byte) error {
	h := []byte(s.header("audio", "audio/x-wav"))
	wire := make([]byte, 2, len(h)+len(b)+2)
	binary.BigEndian.PutUint16(wire, uint16(len(h)))
	wire = append(wire, h...)
	wire = append(wire, b...)
	return s.ws.Write(ctx, websocket.MessageBinary, wire)
}
func (s *sttStream) begin(ctx context.Context) error {
	id := make([]byte, 16)
	if _, e := rand.Read(id); e != nil {
		return e
	}
	s.requestID = hex.EncodeToString(id)
	if e := s.text(ctx, "speech.config", map[string]any{"context": map[string]any{"system": map[string]string{"name": "leamout-ai-providers", "version": "1.0", "build": "Go"}}}); e != nil {
		return e
	}
	if e := s.text(ctx, "speech.context", map[string]any{}); e != nil {
		return e
	}
	return s.audio(ctx, waveHeader(s.format.SampleRateHz))
}
func waveHeader(rate int) []byte {
	b := make([]byte, 44)
	copy(b, "RIFF")
	binary.LittleEndian.PutUint32(b[4:], 0)
	copy(b[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(b[16:], 16)
	binary.LittleEndian.PutUint16(b[20:], 1)
	binary.LittleEndian.PutUint16(b[22:], 1)
	binary.LittleEndian.PutUint32(b[24:], uint32(rate))
	binary.LittleEndian.PutUint32(b[28:], uint32(rate*2))
	binary.LittleEndian.PutUint16(b[32:], 2)
	binary.LittleEndian.PutUint16(b[34:], 16)
	copy(b[36:], "data")
	binary.LittleEndian.PutUint32(b[40:], 0)
	return b
}
func (s *sttStream) SendAudio(ctx context.Context, f ai.AudioFrame) error {
	if e := f.Validate(); e != nil {
		return e
	}
	if f.Format != s.format {
		return errors.New("azure audio format changed")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.final {
		return errors.New("azure audio finalized")
	}
	return s.audio(ctx, f.Data)
}

// Finalize sends audio end-of-stream; recognition results remain readable.
func (s *sttStream) Finalize(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.final {
		return nil
	}
	if e := s.audio(ctx, nil); e != nil {
		return e
	}
	s.final = true
	return nil
}
func (s *sttStream) Events() <-chan ai.STTEvent { return s.events }
func (s *sttStream) Close(ctx context.Context) error {
	if ctx == nil {
		return errors.New("context is required")
	}
	if e := s.Finalize(ctx); e != nil {
		s.ws.Close()
		return e
	}
	select {
	case <-s.done:
		return s.ws.Close()
	case <-ctx.Done():
		s.ws.Close()
		return ctx.Err()
	}
}
func (s *sttStream) emit(e ai.STTEvent) bool {
	select {
	case s.events <- e:
		return true
	case <-s.ws.Ctx.Done():
		return false
	}
}
func (s *sttStream) read() {
	defer close(s.done)
	defer close(s.events)
	defer s.ws.Close()
	for {
		k, b, e := s.ws.Conn.Read(s.ws.Ctx)
		if e != nil {
			if s.ws.Ctx.Err() == nil {
				s.emit(ai.STTEvent{Type: ai.STTEventError, Err: e})
			}
			return
		}
		if k != websocket.MessageText {
			s.emit(ai.STTEvent{Type: ai.STTEventError, Err: errors.New("azure returned non-text event")})
			return
		}
		h, body, ok := strings.Cut(string(b), "\r\n\r\n")
		if !ok {
			s.emit(ai.STTEvent{Type: ai.STTEventError, Err: errors.New("invalid azure event framing")})
			return
		}
		path, id := "", ""
		for _, line := range strings.Split(h, "\r\n") {
			key, value, _ := strings.Cut(line, ":")
			switch strings.ToLower(strings.TrimSpace(key)) {
			case "path":
				path = strings.ToLower(strings.TrimSpace(value))
			case "x-requestid":
				id = strings.TrimSpace(value)
			}
		}
		event := ai.STTEvent{ProviderID: id}
		switch path {
		case "speech.startdetected":
			event.Type = ai.STTEventSpeechStarted
		case "speech.enddetected":
			event.Type = ai.STTEventSpeechStopped
		case "speech.hypothesis", "speech.fragment", "speech.phrase":
			var v struct {
				Text    string `json:"Text"`
				Display string `json:"DisplayText"`
				Status  string `json:"RecognitionStatus"`
			}
			if e := json.Unmarshal([]byte(body), &v); e != nil {
				s.emit(ai.STTEvent{Type: ai.STTEventError, Err: e})
				return
			}
			if path == "speech.phrase" {
				if v.Status != "Success" {
					if v.Status == "NoMatch" || v.Status == "InitialSilenceTimeout" || v.Status == "EndOfDictation" {
						continue
					}
					s.emit(ai.STTEvent{Type: ai.STTEventError, Err: fmt.Errorf("azure recognition: %s", v.Status)})
					return
				}
				event.Type = ai.STTEventTranscriptFinal
				event.Text = v.Display
			} else {
				event.Type = ai.STTEventTranscriptDelta
				event.Text = v.Text
			}
		case "turn.end":
			s.mu.Lock()
			if s.final {
				s.mu.Unlock()
				return
			}
			e := s.begin(s.ws.Ctx)
			s.mu.Unlock()
			if e != nil {
				s.emit(ai.STTEvent{Type: ai.STTEventError, Err: e})
				return
			}
			continue
		default:
			continue
		}
		if !s.emit(event) {
			return
		}
	}
}

var _ ai.STT = STT{}
var _ ai.ConfigValidator = STT{}
