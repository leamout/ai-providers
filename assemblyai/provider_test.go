package assemblyai

import (
	"encoding/json"
	"testing"

	"github.com/leamout/sdk/ai"
)

func TestDescriptor(t *testing.T) {
	descriptor := (Provider{}).Descriptor()
	if err := descriptor.Validate(); err != nil {
		t.Fatalf("validate descriptor: %v", err)
	}
	if descriptor.ID != "assemblyai" {
		t.Fatalf("unexpected provider id %q", descriptor.ID)
	}
	if descriptor.Kind != ai.KindSTT {
		t.Fatalf("unexpected provider kind %q", descriptor.Kind)
	}
	if !descriptor.Supports(ai.CapabilityStreaming) {
		t.Fatal("streaming capability is required")
	}
	if !descriptor.Supports(ai.CapabilityTurnDetection) {
		t.Fatal("turn detection capability is required")
	}
}

func TestDecodeConfigDefaults(t *testing.T) {
	cfg, err := decodeConfig(nil)
	if err != nil {
		t.Fatalf("decode config: %v", err)
	}
	if cfg.Endpoint != DefaultEndpoint {
		t.Fatalf("unexpected endpoint %q", cfg.Endpoint)
	}
	if cfg.SpeechModel != DefaultSpeechModel {
		t.Fatalf("unexpected speech model %q", cfg.SpeechModel)
	}
}

func TestValidateConfig(t *testing.T) {
	tests := []struct {
		name string
		raw  json.RawMessage
	}{
		{name: "invalid json", raw: json.RawMessage(`{`)},
		{name: "insecure endpoint", raw: json.RawMessage(`{"endpoint":"ws://example.com/v3/ws"}`)},
		{name: "endpoint with user info", raw: json.RawMessage(`{"endpoint":"wss://user@example.com/v3/ws"}`)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := (Provider{}).ValidateConfig(test.raw); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestValidateAudioFormat(t *testing.T) {
	valid := ai.AudioFormat{
		Encoding:     ai.AudioEncodingPCM16LE,
		SampleRateHz: 16000,
		Channels:     1,
	}
	if err := validateAudioFormat(valid); err != nil {
		t.Fatalf("validate audio format: %v", err)
	}

	invalid := []ai.AudioFormat{
		{Encoding: ai.AudioEncodingPCM16LE, SampleRateHz: 16000, Channels: 2},
		{Encoding: ai.AudioEncodingALaw, SampleRateHz: 8000, Channels: 1},
		{Encoding: ai.AudioEncodingPCM16LE, SampleRateHz: 4000, Channels: 1},
	}
	for _, format := range invalid {
		if err := validateAudioFormat(format); err == nil {
			t.Fatalf("accepted invalid audio format %+v", format)
		}
	}
}
