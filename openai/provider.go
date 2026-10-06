// Package openai implements OpenAI language-model and realtime adapters.
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

func (p Provider) VerifyCredential(
	ctx context.Context,
	credential string,
) error {
	return verifyCredential(ctx, p.HTTPClient, credential)
}

func (p Provider) Generate(
	ctx context.Context,
	request ai.LLMRequest,
) (ai.LLMStream, error) {
	cfg, err := decodeConfig(request.Runtime.Config)
	if err != nil {
		return nil, err
	}

	return newClient(p.HTTPClient).generate(
		ctx,
		request.Runtime.Credential,
		cfg,
		request,
	)
}

// RealtimeProvider implements OpenAI Realtime speech-to-speech sessions.
type RealtimeProvider struct {
	HTTPClient *http.Client
}

func (RealtimeProvider) Descriptor() ai.Descriptor {
	return ai.Descriptor{
		ID:   "openai",
		Name: "OpenAI",
		Kind: ai.KindRealtime,
		Capabilities: []ai.Capability{
			ai.CapabilityStreaming,
			ai.CapabilityTurnDetection,
			ai.CapabilityToolCalling,
			ai.CapabilityUsage,
			ai.CapabilityBargeIn,
		},
	}
}

func (RealtimeProvider) ValidateConfig(raw json.RawMessage) error {
	_, err := decodeRealtimeConfig(raw)
	return err
}

func (p RealtimeProvider) VerifyCredential(
	ctx context.Context,
	credential string,
) error {
	return verifyCredential(ctx, p.HTTPClient, credential)
}

func (p RealtimeProvider) StartRealtime(
	ctx context.Context,
	request ai.RealtimeRequest,
) (ai.RealtimeStream, error) {
	cfg, err := decodeRealtimeConfig(request.Runtime.Config)
	if err != nil {
		return nil, err
	}

	return newClient(p.HTTPClient).start(ctx, cfg, request)
}

func verifyCredential(
	ctx context.Context,
	httpClient *http.Client,
	credential string,
) error {
	credential = strings.TrimSpace(credential)
	if credential == "" {
		return fmt.Errorf("openai credential is required")
	}

	client := httpClient
	if client == nil {
		client = http.DefaultClient
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		verifyEndpoint,
		nil,
	)
	if err != nil {
		return fmt.Errorf(
			"create OpenAI credential verification request: %w",
			err,
		)
	}
	req.Header.Set("Authorization", "Bearer "+credential)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("verify OpenAI credential: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < http.StatusOK ||
		resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf(
			"verify OpenAI credential: HTTP %d",
			resp.StatusCode,
		)
	}

	return nil
}

var _ ai.LLM = Provider{}
var _ ai.ConfigValidator = Provider{}
var _ ai.CredentialVerifier = Provider{}
var _ ai.Realtime = RealtimeProvider{}
var _ ai.ConfigValidator = RealtimeProvider{}
var _ ai.CredentialVerifier = RealtimeProvider{}
