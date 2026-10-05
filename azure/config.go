// Package azure implements Azure Speech adapters.
package azure

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

const (
	DefaultSTTLanguage = "en-US"
	DefaultTTSLanguage = "en-US"
	DefaultTTSVoice    = "en-US-JennyNeural"
)

var regionPattern = regexp.MustCompile(`^[a-z0-9-]+$`)

type STTConfig struct {
	Region   string `json:"region"`
	Endpoint string `json:"endpoint,omitempty"`
	Language string `json:"language,omitempty"`
}

type TTSConfig struct {
	Region   string `json:"region"`
	Endpoint string `json:"endpoint,omitempty"`
	Voice    string `json:"voice,omitempty"`
	Language string `json:"language,omitempty"`
}

func decodeSTTConfig(raw json.RawMessage) (STTConfig, error) {
	cfg := STTConfig{Language: DefaultSTTLanguage}
	if len(raw) != 0 {
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return STTConfig{}, fmt.Errorf("decode Azure STT config: %w", err)
		}
	}
	if strings.TrimSpace(cfg.Endpoint) == "" {
		if !regionPattern.MatchString(strings.TrimSpace(cfg.Region)) {
			return STTConfig{}, fmt.Errorf("azure region is required")
		}
		cfg.Endpoint = "wss://" + cfg.Region + ".stt.speech.microsoft.com/speech/recognition/conversation/cognitiveservices/v1"
	}
	if strings.TrimSpace(cfg.Language) == "" {
		cfg.Language = DefaultSTTLanguage
	}
	return cfg, validateSTTConfig(cfg)
}

func validateSTTConfig(cfg STTConfig) error {
	if err := validateEndpoint(cfg.Endpoint, "wss", "azure STT"); err != nil {
		return err
	}
	if strings.TrimSpace(cfg.Language) == "" {
		return fmt.Errorf("azure STT language is required")
	}
	return nil
}

func decodeTTSConfig(raw json.RawMessage) (TTSConfig, error) {
	cfg := TTSConfig{
		Voice:    DefaultTTSVoice,
		Language: DefaultTTSLanguage,
	}
	if len(raw) != 0 {
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return TTSConfig{}, fmt.Errorf("decode Azure TTS config: %w", err)
		}
	}
	if strings.TrimSpace(cfg.Endpoint) == "" {
		if !regionPattern.MatchString(strings.TrimSpace(cfg.Region)) {
			return TTSConfig{}, fmt.Errorf("azure region is required")
		}
		cfg.Endpoint = "https://" + cfg.Region + ".tts.speech.microsoft.com/cognitiveservices/v1"
	}
	if strings.TrimSpace(cfg.Voice) == "" {
		cfg.Voice = DefaultTTSVoice
	}
	if strings.TrimSpace(cfg.Language) == "" {
		cfg.Language = DefaultTTSLanguage
	}
	return cfg, validateTTSConfig(cfg)
}

func validateTTSConfig(cfg TTSConfig) error {
	if err := validateEndpoint(cfg.Endpoint, "https", "azure TTS"); err != nil {
		return err
	}
	if strings.TrimSpace(cfg.Voice) == "" {
		return fmt.Errorf("azure TTS voice is required")
	}
	if strings.TrimSpace(cfg.Language) == "" {
		return fmt.Errorf("azure TTS language is required")
	}
	return nil
}

func validateEndpoint(value, scheme, name string) error {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Scheme != scheme || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return fmt.Errorf("%s endpoint must be an absolute %s URL", name, strings.ToUpper(scheme))
	}
	return nil
}
