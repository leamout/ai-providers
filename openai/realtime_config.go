package openai

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

const (
	DefaultRealtimeEndpoint = "wss://api.openai.com/v1/realtime"
	DefaultRealtimeModel    = "gpt-realtime-2.1"
)

// RealtimeConfig contains OpenAI Realtime-specific runtime options.
type RealtimeConfig struct {
	Endpoint         string `json:"endpoint,omitempty"`
	Model            string `json:"model,omitempty"`
	SafetyIdentifier string `json:"safety_identifier,omitempty"`
}

func decodeRealtimeConfig(raw json.RawMessage) (RealtimeConfig, error) {
	cfg := RealtimeConfig{}
	if len(raw) != 0 {
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return RealtimeConfig{}, fmt.Errorf(
				"decode OpenAI Realtime config: %w",
				err,
			)
		}
	}

	if strings.TrimSpace(cfg.Endpoint) == "" {
		cfg.Endpoint = DefaultRealtimeEndpoint
	}
	if strings.TrimSpace(cfg.Model) == "" {
		cfg.Model = DefaultRealtimeModel
	}

	return cfg, validateRealtimeConfig(cfg)
}

func validateRealtimeConfig(cfg RealtimeConfig) error {
	parsed, err := url.Parse(strings.TrimSpace(cfg.Endpoint))
	if err != nil ||
		parsed.Scheme != "wss" ||
		parsed.Host == "" ||
		parsed.User != nil ||
		parsed.Fragment != "" {
		return fmt.Errorf(
			"openai realtime endpoint must be an absolute WSS URL",
		)
	}

	if strings.TrimSpace(cfg.Model) == "" {
		return fmt.Errorf("openai realtime model is required")
	}

	return nil
}
