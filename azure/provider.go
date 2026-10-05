package azure

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/leamout/sdk/ai"
)

// STT implements Azure Speech-to-Text streaming recognition.
type STT struct {
	HTTPClient *http.Client
}

func (STT) Descriptor() ai.Descriptor {
	return ai.Descriptor{
		ID:   "azure",
		Name: "Azure",
		Kind: ai.KindSTT,
		Capabilities: []ai.Capability{
			ai.CapabilityStreaming,
			ai.CapabilityTurnDetection,
		},
	}
}

func (STT) ValidateConfig(raw json.RawMessage) error {
	_, err := decodeSTTConfig(raw)
	return err
}

func (p STT) StartSTT(ctx context.Context, request ai.STTRequest) (ai.STTStream, error) {
	cfg, err := decodeSTTConfig(request.Runtime.Config)
	if err != nil {
		return nil, err
	}
	return newClient(p.HTTPClient).startSTT(ctx, cfg, request)
}

// TTS implements Azure Speech text-to-speech synthesis.
type TTS struct {
	HTTPClient *http.Client
}

func (TTS) Descriptor() ai.Descriptor {
	return ai.Descriptor{
		ID:   "azure",
		Name: "Azure",
		Kind: ai.KindTTS,
		Capabilities: []ai.Capability{
			ai.CapabilityStreaming,
		},
	}
}

func (TTS) ValidateConfig(raw json.RawMessage) error {
	_, err := decodeTTSConfig(raw)
	return err
}

func (p TTS) StartTTS(ctx context.Context, request ai.TTSRequest) (ai.TTSStream, error) {
	cfg, err := decodeTTSConfig(request.Runtime.Config)
	if err != nil {
		return nil, err
	}
	return newClient(p.HTTPClient).startTTS(ctx, cfg, request)
}

// Azure credential verification depends on region or a configured endpoint, so
// the current config-independent ai.CredentialVerifier contract is not exposed.
var _ ai.STT = STT{}
var _ ai.TTS = TTS{}
var _ ai.ConfigValidator = STT{}
var _ ai.ConfigValidator = TTS{}
