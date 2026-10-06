package cartesia

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

const (
	DefaultEndpoint         = "wss://api.cartesia.ai/tts/websocket"
	DefaultAPIVersion       = "2026-08-14"
	DefaultModel            = "sonic-3.6"
	DefaultMaxBufferDelayMS = 3000
)

// Config contains Cartesia-specific runtime options.
type Config struct {
	MaxBufferDelayMS *int   `json:"max_buffer_delay_ms,omitempty"`
	Endpoint         string `json:"endpoint,omitempty"`
	APIVersion       string `json:"api_version,omitempty"`
	Model            string `json:"model,omitempty"`
	VoiceID          string `json:"voice_id,omitempty"`
	Language         string `json:"language,omitempty"`
}

func decodeConfig(raw json.RawMessage) (Config, error) {
	cfg := Config{}
	if len(raw) != 0 {
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return Config{}, fmt.Errorf("decode Cartesia config: %w", err)
		}
	}
	if strings.TrimSpace(cfg.Endpoint) == "" {
		cfg.Endpoint = DefaultEndpoint
	}
	if strings.TrimSpace(cfg.APIVersion) == "" {
		cfg.APIVersion = DefaultAPIVersion
	}
	if strings.TrimSpace(cfg.Model) == "" {
		cfg.Model = DefaultModel
	}
	if cfg.MaxBufferDelayMS == nil {
		delay := DefaultMaxBufferDelayMS
		cfg.MaxBufferDelayMS = &delay
	}
	return cfg, validateConfig(cfg)
}

func validateConfig(cfg Config) error {
	if cfg.MaxBufferDelayMS != nil && (*cfg.MaxBufferDelayMS < 0 || *cfg.MaxBufferDelayMS > 5000) {
		return fmt.Errorf("cartesia max_buffer_delay_ms must be between 0 and 5000")
	}
	endpoint := strings.TrimSpace(cfg.Endpoint)
	if endpoint == "" {
		endpoint = DefaultEndpoint
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme != "wss" || parsed.Host == "" {
		return fmt.Errorf("cartesia endpoint must be an absolute wss URL")
	}
	if strings.TrimSpace(cfg.APIVersion) == "" {
		return fmt.Errorf("cartesia API version is required")
	}
	if strings.TrimSpace(cfg.Model) == "" {
		return fmt.Errorf("cartesia model is required")
	}
	return nil
}
