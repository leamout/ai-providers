package anthropic

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

const (
	DefaultEndpoint       = "https://api.anthropic.com/v1/messages"
	DefaultVerifyEndpoint = "https://api.anthropic.com/v1/models"
	DefaultModel          = "claude-sonnet-4-5"
	DefaultMaxTokens      = 4096
	APIVersion            = "2023-06-01"
)

// Config contains Anthropic-specific runtime options.
type Config struct {
	Endpoint    string   `json:"endpoint,omitempty"`
	Model       string   `json:"model,omitempty"`
	MaxTokens   int      `json:"max_tokens,omitempty"`
	Temperature *float64 `json:"temperature,omitempty"`
}

func decodeConfig(raw json.RawMessage) (Config, error) {
	cfg := Config{
		Endpoint:  DefaultEndpoint,
		Model:     DefaultModel,
		MaxTokens: DefaultMaxTokens,
	}
	if len(raw) != 0 {
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return Config{}, fmt.Errorf("decode Anthropic config: %w", err)
		}
	}
	if strings.TrimSpace(cfg.Endpoint) == "" {
		cfg.Endpoint = DefaultEndpoint
	}
	if strings.TrimSpace(cfg.Model) == "" {
		cfg.Model = DefaultModel
	}
	if cfg.MaxTokens == 0 {
		cfg.MaxTokens = DefaultMaxTokens
	}
	return cfg, validateConfig(cfg)
}

func validateConfig(cfg Config) error {
	parsed, err := url.Parse(strings.TrimSpace(cfg.Endpoint))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return fmt.Errorf("anthropic endpoint must be an absolute HTTPS URL")
	}
	if strings.TrimSpace(cfg.Model) == "" {
		return fmt.Errorf("anthropic model is required")
	}
	if cfg.MaxTokens <= 0 {
		return fmt.Errorf("anthropic max_tokens must be positive")
	}
	if cfg.Temperature != nil && (*cfg.Temperature < 0 || *cfg.Temperature > 1) {
		return fmt.Errorf("anthropic temperature must be between 0 and 1")
	}
	return nil
}
