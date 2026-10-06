// Package gemini implements Gemini Live realtime speech-to-speech sessions.
package gemini

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/leamout/contracts/ai"
)

const verifyEndpoint = "https://generativelanguage.googleapis.com/v1beta/models"

// RealtimeProvider implements the Gemini Live realtime contract.
type RealtimeProvider struct {
	HTTPClient *http.Client
}

func (RealtimeProvider) Descriptor() ai.Descriptor {
	return ai.Descriptor{
		ID:   "gemini",
		Name: "Gemini",
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
	credential = strings.TrimSpace(credential)
	if credential == "" {
		return fmt.Errorf("gemini credential is required")
	}

	client := p.HTTPClient
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
		return fmt.Errorf("create Gemini credential verification request: %w", err)
	}
	req.Header.Set("x-goog-api-key", credential)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("verify Gemini credential: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < http.StatusOK ||
		resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf(
			"verify Gemini credential: HTTP %d",
			resp.StatusCode,
		)
	}

	return nil
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

var _ ai.Realtime = RealtimeProvider{}
var _ ai.ConfigValidator = RealtimeProvider{}
var _ ai.CredentialVerifier = RealtimeProvider{}
