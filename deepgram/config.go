package deepgram

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

const (
	DefaultEndpoint = "wss://api.deepgram.com/v2/listen"
	DefaultModel    = "flux-general-en"
)

// Config contains Deepgram Flux-specific runtime options.
type Config struct {
	Endpoint          string   `json:"endpoint,omitempty"`
	Model             string   `json:"model,omitempty"`
	LanguageHints     []string `json:"language_hints,omitempty"`
	EOTThreshold      *float64 `json:"eot_threshold,omitempty"`
	EagerEOTThreshold *float64 `json:"eager_eot_threshold,omitempty"`
	EOTTimeoutMS      int      `json:"eot_timeout_ms,omitempty"`
}

func decodeConfig(raw json.RawMessage) (Config, error) {
	cfg := Config{}
	if len(raw) != 0 {
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return Config{}, fmt.Errorf("decode Deepgram config: %w", err)
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
	if err != nil || parsed.Scheme != "wss" || parsed.Host == "" {
		return fmt.Errorf("deepgram endpoint must be an absolute wss URL")
	}

	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		model = DefaultModel
	}
	if model != "flux-general-en" && model != "flux-general-multi" {
		return fmt.Errorf("unsupported Deepgram Flux model %q", model)
	}
	if len(cfg.LanguageHints) > 0 && model != "flux-general-multi" {
		return fmt.Errorf("deepgram language hints require flux-general-multi")
	}
	if cfg.EOTThreshold != nil && (*cfg.EOTThreshold < 0.5 || *cfg.EOTThreshold > 1.0) {
		return fmt.Errorf("deepgram eot_threshold must be between 0.5 and 1.0")
	}
	if cfg.EagerEOTThreshold != nil && (*cfg.EagerEOTThreshold < 0.3 || *cfg.EagerEOTThreshold > 0.9) {
		return fmt.Errorf("deepgram eager_eot_threshold must be between 0.3 and 0.9")
	}
	if cfg.EOTThreshold != nil && cfg.EagerEOTThreshold != nil && *cfg.EagerEOTThreshold > *cfg.EOTThreshold {
		return fmt.Errorf("deepgram eager_eot_threshold must not exceed eot_threshold")
	}
	if cfg.EOTTimeoutMS != 0 && (cfg.EOTTimeoutMS < 500 || cfg.EOTTimeoutMS > 60000) {
		return fmt.Errorf("deepgram eot_timeout_ms must be between 500 and 60000")
	}
	return nil
}
