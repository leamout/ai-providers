package elevenlabs

import (
	"encoding/json"
	"testing"

	"github.com/leamout/contracts/ai"
)

func TestDescriptor(t *testing.T) {
	d := (Provider{}).Descriptor()
	if err := d.Validate(); err != nil {
		t.Fatal(err)
	}
	if d.ID != "elevenlabs" || d.Kind != ai.KindTTS {
		t.Fatalf("unexpected descriptor: %+v", d)
	}
	if !d.Supports(ai.CapabilityStreaming) {
		t.Fatal("expected streaming capability")
	}
}

func TestConfig(t *testing.T) {
	cfg, err := decodeConfig(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Endpoint != DefaultEndpoint || cfg.Model != DefaultModel {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}

	if _, err := decodeConfig(json.RawMessage(`{"endpoint":"http://insecure.test"}`)); err == nil {
		t.Fatal("expected insecure endpoint to fail")
	}
}

func TestOutputFormat(t *testing.T) {
	tests := []struct {
		name   string
		format ai.AudioFormat
		want   string
	}{
		{
			name: "pcm",
			format: ai.AudioFormat{
				Encoding:     ai.AudioEncodingPCM16LE,
				SampleRateHz: 24000,
				Channels:     1,
			},
			want: "pcm_24000",
		},
		{
			name: "mulaw",
			format: ai.AudioFormat{
				Encoding:     ai.AudioEncodingMuLaw,
				SampleRateHz: 8000,
				Channels:     1,
			},
			want: "ulaw_8000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := outputFormat(tt.format)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}

	_, err := outputFormat(ai.AudioFormat{
		Encoding:     ai.AudioEncodingPCM16LE,
		SampleRateHz: 12345,
		Channels:     1,
	})
	if err == nil {
		t.Fatal("expected unsupported sample rate to fail")
	}
}
