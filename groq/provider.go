package groq

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/leamout/sdk/ai"
)

const verifyEndpoint = "https://api.groq.com/openai/v1/models"

// Provider implements Groq streaming language-model generation.
type Provider struct {
	HTTPClient *http.Client
}

func (Provider) Descriptor() ai.Descriptor {
	return ai.Descriptor{
		ID:   "groq",
		Name: "Groq",
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
		return fmt.Errorf("groq credential is required")
	}
	client := p.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, verifyEndpoint, nil)
	if err != nil {
		return fmt.Errorf("create Groq credential verification request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+credential)
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("verify Groq credential: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("verify Groq credential: HTTP %d", resp.StatusCode)
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
