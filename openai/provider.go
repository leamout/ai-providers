// Package openai implements OpenAI language-model adapters.
package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/leamout/contracts/ai"
)

const verifyEndpoint = "https://api.openai.com/v1/models"

// Provider implements OpenAI streaming language-model generation.
type Provider struct {
	HTTPClient *http.Client
}

func (Provider) Descriptor() ai.Descriptor {
	return ai.Descriptor{
		ID:   "openai",
		Name: "OpenAI",
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
	credential = strings.TrimSpace(credential)
	if credential == "" {
		return fmt.Errorf("openai credential is required")
	}

	client := p.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, verifyEndpoint, nil)
	if err != nil {
		return fmt.Errorf("create OpenAI credential verification request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+credential)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("verify OpenAI credential: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("verify OpenAI credential: HTTP %d", resp.StatusCode)
	}
	return nil
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
