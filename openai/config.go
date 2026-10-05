package openai

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

const (
	DefaultEndpoint = "https://api.openai.com/v1/chat/completions"
	DefaultModel    = "gpt-4.1-mini"
)

// Config contains OpenAI-specific runtime options.
type Config struct {
	Endpoint            string   `json:"endpoint,omitempty"`
	Model               string   `json:"model,omitempty"`
	Temperature         *float64 `json:"temperature,omitempty"`
	MaxCompletionTokens int      `json:"max_completion_tokens,omitempty"`
}

func decodeConfig(raw json.RawMessage) (Config, error) {
	cfg := Config{}
	if len(raw) != 0 {
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return Config{}, fmt.Errorf("decode OpenAI config: %w", err)
		}
	}
	if strings.TrimSpace(cfg.Endpoint) == "" {
		cfg.Endpoint = DefaultEndpoint
	}
	if strings.TrimSpace(cfg.Model) == "" {
		cfg.Model = DefaultModel
	}
	return cfg, validateConfig(cfg)
}

func validateConfig(cfg Config) error {
	parsed, err := url.Parse(strings.TrimSpace(cfg.Endpoint))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return fmt.Errorf("openai endpoint must be an absolute HTTPS URL")
	}
	if strings.TrimSpace(cfg.Model) == "" {
		return fmt.Errorf("openai model is required")
	}
	if cfg.Temperature != nil && (*cfg.Temperature < 0 || *cfg.Temperature > 2) {
		return fmt.Errorf("openai temperature must be between 0 and 2")
	}
	if cfg.MaxCompletionTokens < 0 {
		return fmt.Errorf("openai max_completion_tokens must not be negative")
	}
	return nil
}
