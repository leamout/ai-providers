// Package azure implements Azure Speech adapters.
package azure

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/leamout/ai-providers/internal/transport"
	"github.com/leamout/sdk/ai"
)

var regionPattern = regexp.MustCompile(`^[a-z0-9-]+$`)

type TTSConfig struct {
	Region   string `json:"region"`
	Endpoint string `json:"endpoint,omitempty"`
	Voice    string `json:"voice,omitempty"`
	Language string `json:"language,omitempty"`
}
type TTS struct{ HTTPClient *http.Client }

func (TTS) Descriptor() ai.Descriptor {
	return ai.Descriptor{ID: "azure", Name: "Azure", Kind: ai.KindTTS, Capabilities: []ai.Capability{ai.CapabilityStreaming}}
}
func ttsConfig(raw json.RawMessage) (TTSConfig, error) {
	c := TTSConfig{Voice: "en-US-JennyNeural", Language: "en-US"}
	if len(raw) > 0 {
		if e := json.Unmarshal(raw, &c); e != nil {
			return c, e
		}
	}
	if c.Endpoint == "" {
		if !regionPattern.MatchString(c.Region) {
			return c, errors.New("azure region is required")
		}
		c.Endpoint = "https://" + c.Region + ".tts.speech.microsoft.com/cognitiveservices/v1"
	}
	if e := transport.Endpoint(c.Endpoint, "https"); e != nil {
		return c, e
	}
	if c.Voice == "" || c.Language == "" {
		return c, errors.New("azure voice and language are required")
	}
	return c, nil
}
func (TTS) ValidateConfig(b json.RawMessage) error { _, e := ttsConfig(b); return e }
func azureFormat(f ai.AudioFormat) (string, error) {
	if e := f.Validate(); e != nil {
		return "", e
	}
	if f.Channels != 1 {
		return "", errors.New("azure TTS requires mono audio")
	}
	switch f.Encoding {
	case ai.AudioEncodingPCM16LE:
		switch f.SampleRateHz {
		case 8000, 16000, 24000, 48000:
			return fmt.Sprintf("raw-%dkhz-16bit-mono-pcm", f.SampleRateHz/1000), nil
		}
	case ai.AudioEncodingMuLaw:
		if f.SampleRateHz == 8000 {
			return "raw-8khz-8bit-mono-mulaw", nil
		}
	case ai.AudioEncodingALaw:
		if f.SampleRateHz == 8000 {
			return "raw-8khz-8bit-mono-alaw", nil
		}
	}
	return "", errors.New("unsupported azure TTS format")
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
		return nil, errors.New("azure subscription key is required")
	}
	if r.Voice != "" {
		c.Voice = r.Voice
	}
	if r.Language != "" {
		c.Language = r.Language
	}
	output, e := azureFormat(r.Format)
	if e != nil {
		return nil, e
	}
	escape := func(v string) string { var b bytes.Buffer; _ = xml.EscapeText(&b, []byte(v)); return b.String() }
	return transport.NewTTS(ctx, r.Format, func(ctx context.Context, text string) (io.ReadCloser, error) {
		ssml := `<speak version="1.0" xmlns="http://www.w3.org/2001/10/synthesis" xml:lang="` + escape(c.Language) + `"><voice name="` + escape(c.Voice) + `">` + escape(text) + `</voice></speak>`
		req, e := http.NewRequestWithContext(ctx, http.MethodPost, c.Endpoint, strings.NewReader(ssml))
		if e != nil {
			return nil, e
		}
		req.Header.Set("Content-Type", "application/ssml+xml")
		req.Header.Set("Ocp-Apim-Subscription-Key", r.Runtime.Credential)
		req.Header.Set("X-Microsoft-OutputFormat", output)
		req.Header.Set("User-Agent", "leamout-ai-providers")
		resp, e := transport.Client(p.HTTPClient).Do(req)
		if e != nil {
			return nil, e
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			resp.Body.Close()
			return nil, fmt.Errorf("azure TTS HTTP %d", resp.StatusCode)
		}
		return resp.Body, nil
	}), nil
}

// Credential verification is configuration-dependent (region/endpoint), so no
// Config-independent CredentialVerifier is advertised by Azure adapters.
var _ ai.TTS = TTS{}
var _ ai.ConfigValidator = TTS{}
