// Package elevenlabs implements ElevenLabs streaming text-to-speech.
package elevenlabs

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

const (
	DefaultEndpoint       = "wss://api.elevenlabs.io/v1/text-to-speech"
	DefaultVerifyEndpoint = "https://api.elevenlabs.io/v1/user"
	DefaultModel          = "eleven_flash_v2_5"
)

// Config contains ElevenLabs-specific runtime options.
type Config struct {
	Language string `json:"language,omitempty"`
	Endpoint string `json:"endpoint,omitempty"`
	Model    string `json:"model,omitempty"`
	VoiceID  string `json:"voice_id,omitempty"`
}

func decodeConfig(raw json.RawMessage) (Config, error) {
	cfg := Config{
		Endpoint: DefaultEndpoint,
		Model:    DefaultModel,
	}
	if len(raw) != 0 {
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return Config{}, fmt.Errorf("decode ElevenLabs config: %w", err)
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
	if err != nil || parsed.Scheme != "wss" || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return fmt.Errorf("elevenlabs endpoint must be an absolute WSS URL")
	}
	if strings.TrimSpace(cfg.Model) == "" {
		return fmt.Errorf("elevenlabs model is required")
	}
	return nil
}
