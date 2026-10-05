// Package openai implements OpenAI language-model and text-to-speech adapters.
package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/leamout/sdk/ai"
)

const verifyEndpoint = "https://api.openai.com/v1/models"

type LLM struct {
	HTTPClient *http.Client
}

type TTS struct {
	HTTPClient *http.Client
}

func (LLM) Descriptor() ai.Descriptor {
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

func (TTS) Descriptor() ai.Descriptor {
	return ai.Descriptor{
		ID:           "openai",
		Name:         "OpenAI",
		Kind:         ai.KindTTS,
		Capabilities: []ai.Capability{ai.CapabilityStreaming},
	}
}

func (LLM) ValidateConfig(raw json.RawMessage) error {
	_, err := decodeLLMConfig(raw)
	return err
}

func (TTS) ValidateConfig(raw json.RawMessage) error {
	_, err := decodeTTSConfig(raw)
	return err
}

func (p LLM) VerifyCredential(ctx context.Context, credential string) error {
	return verifyCredential(ctx, p.HTTPClient, credential)
}

func (p TTS) VerifyCredential(ctx context.Context, credential string) error {
	return verifyCredential(ctx, p.HTTPClient, credential)
}

func verifyCredential(ctx context.Context, client *http.Client, credential string) error {
	credential = strings.TrimSpace(credential)
	if credential == "" {
		return fmt.Errorf("openai credential is required")
	}
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

func (p LLM) Generate(ctx context.Context, request ai.LLMRequest) (ai.LLMStream, error) {
	cfg, err := decodeLLMConfig(request.Runtime.Config)
	if err != nil {
		return nil, err
	}
	return newClient(p.HTTPClient).generate(ctx, request.Runtime.Credential, cfg, request)
}

func (p TTS) StartTTS(ctx context.Context, request ai.TTSRequest) (ai.TTSStream, error) {
	cfg, err := decodeTTSConfig(request.Runtime.Config)
	if err != nil {
		return nil, err
	}
	if voice := strings.TrimSpace(request.Voice); voice != "" {
		cfg.Voice = voice
	}
	return newClient(p.HTTPClient).startTTS(ctx, request.Runtime.Credential, cfg, request.Format)
}

var _ ai.LLM = LLM{}
var _ ai.TTS = TTS{}
var _ ai.ConfigValidator = LLM{}
var _ ai.ConfigValidator = TTS{}
var _ ai.CredentialVerifier = LLM{}
var _ ai.CredentialVerifier = TTS{}
