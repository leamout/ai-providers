package cartesia

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/leamout/contracts/ai"
)

const verifyEndpoint = "https://api.cartesia.ai/models"

// Provider implements Cartesia streaming text-to-speech.
type Provider struct {
	HTTPClient *http.Client
}

func (Provider) Descriptor() ai.Descriptor {
	return ai.Descriptor{
		ID:   "cartesia",
		Name: "Cartesia",
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
	credential = strings.TrimSpace(credential)
	if credential == "" {
		return fmt.Errorf("cartesia credential is required")
	}
	client := p.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, verifyEndpoint, nil)
	if err != nil {
		return fmt.Errorf("create Cartesia credential verification request: %w", err)
	}
	req.Header.Set("X-API-Key", credential)
	req.Header.Set("Cartesia-Version", DefaultAPIVersion)
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("verify Cartesia credential: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("verify Cartesia credential: HTTP %d", resp.StatusCode)
	}
	return nil
}

func (p Provider) StartTTS(ctx context.Context, request ai.TTSRequest) (ai.TTSStream, error) {
	cfg, err := decodeConfig(request.Runtime.Config)
	if err != nil {
		return nil, err
	}
	if voice := strings.TrimSpace(request.Voice); voice != "" {
		cfg.VoiceID = voice
	}
	if language := strings.TrimSpace(request.Language); language != "" {
		cfg.Language = language
	}
	if strings.TrimSpace(cfg.VoiceID) == "" {
		return nil, fmt.Errorf("cartesia voice ID is required")
	}
	return newClient(p.HTTPClient).start(ctx, request.Runtime.Credential, cfg, request.Format)
}

var _ ai.TTS = Provider{}
var _ ai.ConfigValidator = Provider{}
var _ ai.CredentialVerifier = Provider{}
