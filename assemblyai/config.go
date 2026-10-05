// Package assemblyai implements AssemblyAI Universal Streaming transcription.
package assemblyai

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

const (
	DefaultEndpoint       = "wss://streaming.assemblyai.com/v3/ws"
	DefaultVerifyEndpoint = "https://api.assemblyai.com/v2/transcript?limit=1"
	DefaultSpeechModel    = "universal-streaming-english"
)

// Config contains AssemblyAI-specific runtime options.
type Config struct {
	Endpoint    string `json:"endpoint,omitempty"`
	SpeechModel string `json:"speech_model,omitempty"`
	FormatTurns bool   `json:"format_turns,omitempty"`
}

func decodeConfig(raw json.RawMessage) (Config, error) {
	cfg := Config{
		Endpoint:    DefaultEndpoint,
		SpeechModel: DefaultSpeechModel,
	}
	if len(raw) != 0 {
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return Config{}, fmt.Errorf("decode AssemblyAI config: %w", err)
		}
	}
	if strings.TrimSpace(cfg.Endpoint) == "" {
		cfg.Endpoint = DefaultEndpoint
	}
	if strings.TrimSpace(cfg.SpeechModel) == "" {
		cfg.SpeechModel = DefaultSpeechModel
	}
	return cfg, validateConfig(cfg)
}

func validateConfig(cfg Config) error {
	parsed, err := url.Parse(strings.TrimSpace(cfg.Endpoint))
	if err != nil || parsed.Scheme != "wss" || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return fmt.Errorf("assemblyai endpoint must be an absolute WSS URL")
	}
	if strings.TrimSpace(cfg.SpeechModel) == "" {
		return fmt.Errorf("assemblyai speech_model is required")
	}
	return nil
}
