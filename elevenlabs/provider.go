// Package elevenlabs implements ElevenLabs streaming text-to-speech.
package elevenlabs

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/leamout/ai-providers/internal/transport"
	"github.com/leamout/sdk/ai"
)

const DefaultEndpoint = "wss://api.elevenlabs.io/v1/text-to-speech"
const DefaultModel = "eleven_flash_v2_5"

type Config struct {
	Language string `json:"language,omitempty"`
	Endpoint string `json:"endpoint,omitempty"`
	Model    string `json:"model,omitempty"`
	VoiceID  string `json:"voice_id,omitempty"`
}
type Provider struct{ HTTPClient *http.Client }

func (Provider) Descriptor() ai.Descriptor {
	return ai.Descriptor{ID: "elevenlabs", Name: "ElevenLabs", Kind: ai.KindTTS, Capabilities: []ai.Capability{ai.CapabilityStreaming}}
}
func config(raw json.RawMessage) (Config, error) {
	c := Config{Endpoint: DefaultEndpoint, Model: DefaultModel}
	if len(raw) > 0 {
		if e := json.Unmarshal(raw, &c); e != nil {
			return c, e
		}
	}
	if e := transport.Endpoint(c.Endpoint, "wss"); e != nil {
		return c, e
	}
	if c.Model == "" {
		return c, errors.New("elevenlabs model is required")
	}
	return c, nil
}
func (Provider) ValidateConfig(b json.RawMessage) error { _, e := config(b); return e }
func (p Provider) VerifyCredential(ctx context.Context, key string) error {
	if strings.TrimSpace(key) == "" {
		return errors.New("elevenlabs credential is required")
	}
	return transport.Verify(ctx, p.HTTPClient, "https://api.elevenlabs.io/v1/user", http.Header{"Xi-Api-Key": {key}})
}

type stream struct {
	ws     *transport.WS
	format ai.AudioFormat
	events chan ai.TTSEvent
	mu     sync.Mutex
	final  bool
}

func (p Provider) StartTTS(ctx context.Context, r ai.TTSRequest) (ai.TTSStream, error) {
	c, e := config(r.Runtime.Config)
	if e != nil {
		return nil, e
	}
	if strings.TrimSpace(r.Runtime.Credential) == "" {
		return nil, errors.New("elevenlabs credential is required")
	}
	if r.Voice != "" {
		c.VoiceID = r.Voice
	}
	if c.VoiceID == "" {
		return nil, errors.New("elevenlabs voice_id or request voice is required")
	}
	if e := r.Format.Validate(); e != nil {
		return nil, e
	}
	if r.Format.Channels != 1 {
		return nil, errors.New("elevenlabs requires mono audio")
	}
	output := ""
	switch r.Format.Encoding {
	case ai.AudioEncodingPCM16LE:
		switch r.Format.SampleRateHz {
		case 8000, 16000, 22050, 24000, 44100, 48000:
			output = "pcm_" + strconv.Itoa(r.Format.SampleRateHz)
		}
	case ai.AudioEncodingMuLaw:
		if r.Format.SampleRateHz == 8000 {
			output = "ulaw_8000"
		}
	case ai.AudioEncodingALaw:
		if r.Format.SampleRateHz == 8000 {
			output = "alaw_8000"
		}
	}
	if output == "" {
		return nil, errors.New("unsupported elevenlabs audio format")
	}
	endpoint := strings.TrimRight(c.Endpoint, "/") + "/" + url.PathEscape(c.VoiceID) + "/stream-input"
	u, _ := url.Parse(endpoint)
	q := u.Query()
	q.Set("model_id", c.Model)
	q.Set("output_format", output)
	if r.Language != "" {
		c.Language = r.Language
	}
	if c.Language != "" {
		q.Set("language_code", c.Language)
	}
	u.RawQuery = q.Encode()
	ws, e := transport.Dial(ctx, p.HTTPClient, u.String(), http.Header{"Xi-Api-Key": {r.Runtime.Credential}})
	if e != nil {
		return nil, e
	}
	if e := ws.JSON(ctx, map[string]any{"text": " "}); e != nil {
		ws.Close()
		return nil, e
	}
	s := &stream{ws: ws, format: r.Format, events: make(chan ai.TTSEvent, 32)}
	go s.read()
	return s, nil
}
func (s *stream) SendText(ctx context.Context, c ai.TextChunk) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.final {
		return errors.New("elevenlabs text already finalized")
	}
	if c.Text == "" && !c.Final {
		return errors.New("text or final marker is required")
	}
	if c.Text != "" {
		text := c.Text
		if !strings.HasSuffix(text, " ") {
			text += " "
		}
		if e := s.ws.JSON(ctx, map[string]any{"text": text, "try_trigger_generation": true}); e != nil {
			return e
		}
	}
	if c.Final {
		if e := s.ws.JSON(ctx, map[string]any{"text": ""}); e != nil {
			return e
		}
		s.final = true
	}
	return nil
}
func (s *stream) Events() <-chan ai.TTSEvent { return s.events }
func (s *stream) Close() error               { return s.ws.Close() }
func (s *stream) emit(e ai.TTSEvent) bool {
	select {
	case s.events <- e:
		return true
	case <-s.ws.Ctx.Done():
		return false
	}
}
func (s *stream) read() {
	defer close(s.events)
	defer s.Close()
	for {
		var v struct {
			Audio       string `json:"audio"`
			Final       bool   `json:"is_final"`
			LegacyFinal bool   `json:"isFinal"`
			Error       string `json:"error"`
			Message     string `json:"message"`
		}
		if e := s.ws.Read(&v); e != nil {
			if s.ws.Ctx.Err() == nil {
				s.emit(ai.TTSEvent{Err: e})
			}
			return
		}
		if v.Error != "" {
			s.emit(ai.TTSEvent{Err: fmt.Errorf("elevenlabs %s: %s", v.Error, v.Message)})
			return
		}
		if v.Audio != "" {
			b, e := base64.StdEncoding.DecodeString(v.Audio)
			if e != nil {
				s.emit(ai.TTSEvent{Err: e})
				return
			}
			f := ai.AudioFrame{Data: b, Format: s.format}
			if e := f.Validate(); e != nil {
				s.emit(ai.TTSEvent{Err: e})
				return
			}
			if !s.emit(ai.TTSEvent{Audio: f}) {
				return
			}
		}
		if v.Final || v.LegacyFinal {
			s.emit(ai.TTSEvent{Done: true})
			return
		}
	}
}

var _ ai.TTS = Provider{}
var _ ai.ConfigValidator = Provider{}
var _ ai.CredentialVerifier = Provider{}
