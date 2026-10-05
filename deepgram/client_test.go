package deepgram

import (
	"net/url"
	"testing"

	"github.com/leamout/sdk/ai"
)

func TestBuildEndpoint(t *testing.T) {
	threshold := 0.8
	cfg := Config{
		Model:        "flux-general-en",
		EOTThreshold: &threshold,
		EOTTimeoutMS: 7000,
	}
	endpoint, err := buildEndpoint(cfg, ai.AudioFormat{
		Encoding:     ai.AudioEncodingMuLaw,
		SampleRateHz: 8000,
		Channels:     1,
	})
	if err != nil {
		t.Fatalf("buildEndpoint() error = %v", err)
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}
	query := parsed.Query()
	if got := query.Get("model"); got != "flux-general-en" {
		t.Fatalf("model = %q", got)
	}
	if got := query.Get("encoding"); got != "mulaw" {
		t.Fatalf("encoding = %q", got)
	}
	if got := query.Get("sample_rate"); got != "8000" {
		t.Fatalf("sample_rate = %q", got)
	}
	if got := query.Get("eot_threshold"); got != "0.8" {
		t.Fatalf("eot_threshold = %q", got)
	}
	if got := query.Get("eot_timeout_ms"); got != "7000" {
		t.Fatalf("eot_timeout_ms = %q", got)
	}
}

func TestValidateAudioFormatRejectsStereo(t *testing.T) {
	err := validateAudioFormat(ai.AudioFormat{
		Encoding:     ai.AudioEncodingPCM16LE,
		SampleRateHz: 16000,
		Channels:     2,
	})
	if err == nil {
		t.Fatal("validateAudioFormat() error = nil, want error")
	}
}
