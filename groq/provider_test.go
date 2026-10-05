package groq

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/leamout/sdk/ai"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestDescriptor(t *testing.T) {
	d := (Provider{}).Descriptor()
	if d.ID != "groq" || d.Name != "Groq" || d.Kind != ai.KindLLM {
		t.Fatalf("unexpected descriptor: %#v", d)
	}
	if !d.Supports(ai.CapabilityStreaming) || !d.Supports(ai.CapabilityToolCalling) || !d.Supports(ai.CapabilityUsage) {
		t.Fatalf("missing expected capabilities: %#v", d.Capabilities)
	}
}

func TestValidateConfig(t *testing.T) {
	provider := Provider{}
	if err := provider.ValidateConfig(json.RawMessage(`{"model":"llama-3.3-70b-versatile","temperature":1}`)); err != nil {
		t.Fatalf("ValidateConfig() error = %v", err)
	}
	if err := provider.ValidateConfig(json.RawMessage(`{"temperature":3}`)); err == nil {
		t.Fatal("ValidateConfig() error = nil, want error")
	}
}

func TestVerifyCredential(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if got := req.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("Authorization = %q", got)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"data":[]}`)),
			Header:     make(http.Header),
		}, nil
	})}
	if err := (Provider{HTTPClient: client}).VerifyCredential(context.Background(), "test-key"); err != nil {
		t.Fatalf("VerifyCredential() error = %v", err)
	}
}
