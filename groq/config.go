// Package groq implements Groq's streaming chat completion API.
package groq

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

const (
	DefaultEndpoint = "https://api.groq.com/openai/v1/chat/completions"
	DefaultModel    = "llama-3.3-70b-versatile"
)

// Config contains Groq-specific runtime options.
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
			return Config{}, fmt.Errorf("decode Groq config: %w", err)
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
	endpoint := strings.TrimSpace(cfg.Endpoint)
	if endpoint == "" {
		endpoint = DefaultEndpoint
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return fmt.Errorf("groq endpoint must be an absolute HTTPS URL")
	}
	if strings.TrimSpace(cfg.Model) == "" {
		return fmt.Errorf("groq model is required")
	}
	if cfg.Temperature != nil && (*cfg.Temperature < 0 || *cfg.Temperature > 2) {
		return fmt.Errorf("groq temperature must be between 0 and 2")
	}
	if cfg.MaxCompletionTokens < 0 {
		return fmt.Errorf("groq max_completion_tokens must not be negative")
	}
	return nil
}
