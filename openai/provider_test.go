package openai

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
	descriptor := (Provider{}).Descriptor()
	if err := descriptor.Validate(); err != nil {
		t.Fatal(err)
	}
	if descriptor.ID != "openai" || descriptor.Kind != ai.KindLLM {
		t.Fatalf("unexpected descriptor %#v", descriptor)
	}
	for _, capability := range []ai.Capability{
		ai.CapabilityStreaming,
		ai.CapabilityToolCalling,
		ai.CapabilityUsage,
	} {
		if !descriptor.Supports(capability) {
			t.Fatalf("missing capability %q", capability)
		}
	}
}

func TestDecodeConfigDefaults(t *testing.T) {
	cfg, err := decodeConfig(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Endpoint != DefaultEndpoint {
		t.Fatalf("unexpected endpoint %q", cfg.Endpoint)
	}
	if cfg.Model != DefaultModel {
		t.Fatalf("unexpected model %q", cfg.Model)
	}
}

func TestValidateConfigRejectsInvalidValues(t *testing.T) {
	provider := Provider{}
	cases := []json.RawMessage{
		json.RawMessage(`{`),
		json.RawMessage(`{"endpoint":"http://api.openai.com/v1/chat/completions"}`),
		json.RawMessage(`{"max_completion_tokens":-1}`),
		json.RawMessage(`{"temperature":3}`),
	}
	for _, raw := range cases {
		if err := provider.ValidateConfig(raw); err == nil {
			t.Fatalf("expected invalid config %s to fail", string(raw))
		}
	}
}

func TestVerifyCredential(t *testing.T) {
	called := false
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		called = true
		if req.Method != http.MethodGet {
			t.Fatalf("unexpected method %s", req.Method)
		}
		if req.URL.String() != verifyEndpoint {
			t.Fatalf("unexpected URL %s", req.URL)
		}
		if req.Header.Get("Authorization") != "Bearer key" {
			t.Fatalf("unexpected authorization header %q", req.Header.Get("Authorization"))
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{}`)),
			Header:     make(http.Header),
		}, nil
	})}

	provider := Provider{HTTPClient: client}
	if err := provider.VerifyCredential(context.Background(), ""); err == nil {
		t.Fatal("expected empty credential to fail")
	}
	if called {
		t.Fatal("empty credential caused a network request")
	}
	if err := provider.VerifyCredential(context.Background(), "key"); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("expected credential verification request")
	}
}
