// Package anthropic implements Anthropic's streaming Messages API.
package anthropic

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/leamout/sdk/ai"
)

// Provider implements Anthropic streaming language-model generation.
type Provider struct {
	HTTPClient *http.Client
}

func (Provider) Descriptor() ai.Descriptor {
	return ai.Descriptor{
		ID:   "anthropic",
		Name: "Anthropic",
		Kind: ai.KindLLM,
		Capabilities: []ai.Capability{
			ai.CapabilityStreaming,
			ai.CapabilityToolCalling,
			ai.CapabilityUsage,
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

func (p Provider) Generate(ctx context.Context, request ai.LLMRequest) (ai.LLMStream, error) {
	cfg, err := decodeConfig(request.Runtime.Config)
	if err != nil {
		return nil, err
	}
	return newClient(p.HTTPClient).generate(ctx, request.Runtime.Credential, cfg, request)
}

var _ ai.LLM = Provider{}
var _ ai.ConfigValidator = Provider{}
var _ ai.CredentialVerifier = Provider{}
