package deepgram

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/leamout/contracts/ai"
)

const verifyEndpoint = "https://api.deepgram.com/v1/auth/token"

// Provider implements Deepgram Flux streaming speech-to-text.
type Provider struct {
	HTTPClient *http.Client
}

func (Provider) Descriptor() ai.Descriptor {
	return ai.Descriptor{
		ID:   "deepgram",
		Name: "Deepgram",
		Kind: ai.KindSTT,
		Capabilities: []ai.Capability{
			ai.CapabilityStreaming,
			ai.CapabilityTurnDetection,
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
		return fmt.Errorf("deepgram credential is required")
	}
	client := p.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, verifyEndpoint, nil)
	if err != nil {
		return fmt.Errorf("create Deepgram credential verification request: %w", err)
	}
	req.Header.Set("Authorization", "Token "+credential)
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("verify Deepgram credential: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("verify Deepgram credential: HTTP %d", resp.StatusCode)
	}
	return nil
}

func (p Provider) StartSTT(ctx context.Context, request ai.STTRequest) (ai.STTStream, error) {
	cfg, err := decodeConfig(request.Runtime.Config)
	if err != nil {
		return nil, err
	}
	if language := strings.TrimSpace(request.Language); language != "" && cfg.Model == "flux-general-multi" {
		cfg.LanguageHints = append(append([]string(nil), cfg.LanguageHints...), language)
	}
	return newClient(p.HTTPClient).start(ctx, request.Runtime.Credential, cfg, request.Format)
}

var _ ai.STT = Provider{}
var _ ai.ConfigValidator = Provider{}
var _ ai.CredentialVerifier = Provider{}
