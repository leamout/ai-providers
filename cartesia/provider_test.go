package cartesia

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/leamout/contracts/ai"
)

func TestDescriptor(t *testing.T) {
	d := (Provider{}).Descriptor()
	if d.ID != "cartesia" || d.Name != "Cartesia" || d.Kind != ai.KindTTS {
		t.Fatalf("unexpected descriptor: %+v", d)
	}
	if !d.Supports(ai.CapabilityStreaming) {
		t.Fatal("expected streaming capability")
	}
}

func TestValidateConfig(t *testing.T) {
	provider := Provider{}
	if err := provider.ValidateConfig(nil); err != nil {
		t.Fatalf("default config should be valid: %v", err)
	}
	if err := provider.ValidateConfig(json.RawMessage(`{"endpoint":"http://example.com"}`)); err == nil {
		t.Fatal("expected invalid endpoint error")
	}
}

func TestVerifyCredential(t *testing.T) {
	var apiKey, version string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiKey = r.Header.Get("X-API-Key")
		version = r.Header.Get("Cartesia-Version")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	old := verifyEndpoint
	_ = old
	provider := Provider{HTTPClient: &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		req.URL.Scheme = "http"
		req.URL.Host = server.Listener.Addr().String()
		return http.DefaultTransport.RoundTrip(req)
	})}}
	if err := provider.VerifyCredential(context.Background(), "secret"); err != nil {
		t.Fatalf("verify credential: %v", err)
	}
	if apiKey != "secret" {
		t.Fatalf("unexpected API key header %q", apiKey)
	}
	if version != DefaultAPIVersion {
		t.Fatalf("unexpected version header %q", version)
	}
}

func TestValidateAudioFormat(t *testing.T) {
	if err := validateAudioFormat(ai.AudioFormat{Encoding: ai.AudioEncodingPCM16LE, SampleRateHz: 24000, Channels: 1}); err != nil {
		t.Fatalf("expected PCM16 mono audio to be valid: %v", err)
	}
	if err := validateAudioFormat(ai.AudioFormat{Encoding: ai.AudioEncodingMuLaw, SampleRateHz: 8000, Channels: 1}); err == nil {
		t.Fatal("expected mu-law to be rejected")
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
