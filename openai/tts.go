package openai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/leamout/ai-providers/internal/transport"
	"github.com/leamout/sdk/ai"
)

type TTSConfig struct {
	Endpoint string  `json:"endpoint,omitempty"`
	Model    string  `json:"model,omitempty"`
	Voice    string  `json:"voice,omitempty"`
	Speed    float64 `json:"speed,omitempty"`
}
type TTS struct{ HTTPClient *http.Client }

func (TTS) Descriptor() ai.Descriptor {
	return ai.Descriptor{ID: "openai", Name: "OpenAI", Kind: ai.KindTTS, Capabilities: []ai.Capability{ai.CapabilityStreaming}}
}
func ttsConfig(b json.RawMessage) (TTSConfig, error) {
	c := TTSConfig{Endpoint: "https://api.openai.com/v1/audio/speech", Model: "gpt-4o-mini-tts", Voice: "alloy", Speed: 1}
	if len(b) > 0 {
		if e := json.Unmarshal(b, &c); e != nil {
			return c, e
		}
	}
	if e := transport.Endpoint(c.Endpoint, "https"); e != nil {
		return c, e
	}
	if strings.TrimSpace(c.Model) == "" || strings.TrimSpace(c.Voice) == "" {
		return c, errors.New("openai model and voice are required")
	}
	if c.Speed < 0.25 || c.Speed > 4 {
		return c, errors.New("openai speed must be between 0.25 and 4")
	}
	return c, nil
}
func (TTS) ValidateConfig(b json.RawMessage) error { _, e := ttsConfig(b); return e }
func (p TTS) VerifyCredential(ctx context.Context, key string) error {
	return (LLM{HTTPClient: p.HTTPClient}).VerifyCredential(ctx, key)
}
func (p TTS) StartTTS(ctx context.Context, r ai.TTSRequest) (ai.TTSStream, error) {
	if ctx == nil {
		return nil, errors.New("context is required")
	}
	c, e := ttsConfig(r.Runtime.Config)
	if e != nil {
		return nil, e
	}
	if strings.TrimSpace(r.Runtime.Credential) == "" {
		return nil, errors.New("openai credential is required")
	}
	if r.Format != (ai.AudioFormat{Encoding: ai.AudioEncodingPCM16LE, SampleRateHz: 24000, Channels: 1}) {
		return nil, errors.New("openai TTS requires PCM16 mono at 24000 Hz")
	}
	if r.Voice != "" {
		c.Voice = r.Voice
	}
	return transport.NewTTS(ctx, r.Format, func(ctx context.Context, text string) (io.ReadCloser, error) {
		resp, e := transport.JSON(ctx, p.HTTPClient, http.MethodPost, c.Endpoint, http.Header{"Authorization": {"Bearer " + r.Runtime.Credential}}, map[string]any{"model": c.Model, "voice": c.Voice, "input": text, "response_format": "pcm", "speed": c.Speed})
		if e != nil {
			return nil, e
		}
		return resp.Body, nil
	}), nil
}

var _ ai.TTS = TTS{}
var _ ai.ConfigValidator = TTS{}
var _ ai.CredentialVerifier = TTS{}
