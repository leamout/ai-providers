package assemblyai

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/leamout/ai-providers/internal/transport"
	"github.com/leamout/contracts/ai"
)

// Provider implements AssemblyAI streaming speech-to-text.
type Provider struct {
	HTTPClient       *http.Client
	AllowedEndpoints []string
}

func (Provider) Descriptor() ai.Descriptor {
	return ai.Descriptor{
		ID:   "assemblyai",
		Name: "AssemblyAI",
		Kind: ai.KindSTT,
		Capabilities: []ai.Capability{
			ai.CapabilityStreaming,
			ai.CapabilityTurnDetection,
		},
	}
}

func (p Provider) ValidateConfig(raw json.RawMessage) error {
	cfg, err := decodeConfig(raw)
	if err != nil {
		return err
	}
	return p.validateEndpoint(cfg.Endpoint)
}

func (p Provider) validateEndpoint(endpoint string) error {
	return transport.AllowedEndpoint(endpoint, DefaultEndpoint, p.AllowedEndpoints)
}

func (p Provider) VerifyCredential(ctx context.Context, credential string) error {
	return newClient(p.HTTPClient).verifyCredential(ctx, credential)
}

func (p Provider) StartSTT(ctx context.Context, request ai.STTRequest) (ai.STTStream, error) {
	cfg, err := decodeConfig(request.Runtime.Config)
	if err != nil {
		return nil, err
	}
	if err := p.validateEndpoint(cfg.Endpoint); err != nil {
		return nil, err
	}
	return newClient(p.HTTPClient).start(ctx, request.Runtime.Credential, cfg, request)
}

var _ ai.STT = Provider{}
var _ ai.ConfigValidator = Provider{}
var _ ai.CredentialVerifier = Provider{}
