package openai

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

const (
	DefaultLLMEndpoint = "https://api.openai.com/v1/chat/completions"
	DefaultLLMModel    = "gpt-4.1-mini"
)

// LLMConfig contains OpenAI-specific language-model runtime options.
type LLMConfig struct {
	Endpoint            string   `json:"endpoint,omitempty"`
	Model               string   `json:"model,omitempty"`
	Temperature         *float64 `json:"temperature,omitempty"`
	MaxCompletionTokens int      `json:"max_completion_tokens,omitempty"`
}

func decodeLLMConfig(raw json.RawMessage) (LLMConfig, error) {
	cfg := LLMConfig{}
	if len(raw) != 0 {
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return LLMConfig{}, fmt.Errorf("decode OpenAI LLM config: %w", err)
		}
	}
	if strings.TrimSpace(cfg.Endpoint) == "" {
		cfg.Endpoint = DefaultLLMEndpoint
	}
	if strings.TrimSpace(cfg.Model) == "" {
		cfg.Model = DefaultLLMModel
	}
	return cfg, validateLLMConfig(cfg)
}

func validateLLMConfig(cfg LLMConfig) error {
	parsed, err := url.Parse(strings.TrimSpace(cfg.Endpoint))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return fmt.Errorf("openai LLM endpoint must be an absolute HTTPS URL")
	}
	if strings.TrimSpace(cfg.Model) == "" {
		return fmt.Errorf("openai LLM model is required")
	}
	if cfg.Temperature != nil && (*cfg.Temperature < 0 || *cfg.Temperature > 2) {
		return fmt.Errorf("openai LLM temperature must be between 0 and 2")
	}
	if cfg.MaxCompletionTokens < 0 {
		return fmt.Errorf("openai LLM max_completion_tokens must not be negative")
	}
	return nil
}
