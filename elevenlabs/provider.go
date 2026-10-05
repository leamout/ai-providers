// Package elevenlabs implements ElevenLabs streaming text-to-speech.
package elevenlabs

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/leamout/contracts/ai"
)

// Provider implements ElevenLabs streaming TTS.
type Provider struct {
	HTTPClient *http.Client
}

func (Provider) Descriptor() ai.Descriptor {
	return ai.Descriptor{
		ID:   "elevenlabs",
		Name: "ElevenLabs",
		Kind: ai.KindTTS,
		Capabilities: []ai.Capability{
			ai.CapabilityStreaming,
		},
	}
}

func (Provider) ValidateConfig(raw json.RawMessage) error {
	_, err := decodeConfig(raw)
	return err
}

func (p Provider) VerifyCredential(ctx context.Context, credential string) error {
	return newClient(p.HTTPClient).verifyCredential(ctx, credential)
}

func (p Provider) StartTTS(ctx context.Context, request ai.TTSRequest) (ai.TTSStream, error) {
	cfg, err := decodeConfig(request.Runtime.Config)
	if err != nil {
		return nil, err
	}
	return newClient(p.HTTPClient).start(ctx, request.Runtime.Credential, cfg, request)
}

var _ ai.TTS = Provider{}
var _ ai.ConfigValidator = Provider{}
var _ ai.CredentialVerifier = Provider{}
