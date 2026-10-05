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
	DefaultTTSEndpoint = "https://api.openai.com/v1/audio/speech"
	DefaultTTSModel    = "gpt-4o-mini-tts"
	DefaultTTSVoice    = "alloy"
)

type LLMConfig struct {
	Endpoint            string   `json:"endpoint,omitempty"`
	Model               string   `json:"model,omitempty"`
	Temperature         *float64 `json:"temperature,omitempty"`
	MaxCompletionTokens int      `json:"max_completion_tokens,omitempty"`
}

type TTSConfig struct {
	Endpoint string  `json:"endpoint,omitempty"`
	Model    string  `json:"model,omitempty"`
	Voice    string  `json:"voice,omitempty"`
	Speed    float64 `json:"speed,omitempty"`
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
	if err := validateHTTPSEndpoint(cfg.Endpoint, "openai LLM"); err != nil {
		return err
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

func decodeTTSConfig(raw json.RawMessage) (TTSConfig, error) {
	cfg := TTSConfig{
		Endpoint: DefaultTTSEndpoint,
		Model:    DefaultTTSModel,
		Voice:    DefaultTTSVoice,
		Speed:    1,
	}
	if len(raw) != 0 {
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return TTSConfig{}, fmt.Errorf("decode OpenAI TTS config: %w", err)
		}
	}
	if strings.TrimSpace(cfg.Endpoint) == "" {
		cfg.Endpoint = DefaultTTSEndpoint
	}
	if strings.TrimSpace(cfg.Model) == "" {
		cfg.Model = DefaultTTSModel
	}
	if strings.TrimSpace(cfg.Voice) == "" {
		cfg.Voice = DefaultTTSVoice
	}
	if cfg.Speed == 0 {
		cfg.Speed = 1
	}
	return cfg, validateTTSConfig(cfg)
}

func validateTTSConfig(cfg TTSConfig) error {
	if err := validateHTTPSEndpoint(cfg.Endpoint, "openai TTS"); err != nil {
		return err
	}
	if strings.TrimSpace(cfg.Model) == "" {
		return fmt.Errorf("openai TTS model is required")
	}
	if strings.TrimSpace(cfg.Voice) == "" {
		return fmt.Errorf("openai TTS voice is required")
	}
	if cfg.Speed < 0.25 || cfg.Speed > 4 {
		return fmt.Errorf("openai TTS speed must be between 0.25 and 4")
	}
	return nil
}

func validateHTTPSEndpoint(value, name string) error {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return fmt.Errorf("%s endpoint must be an absolute HTTPS URL", name)
	}
	return nil
}
