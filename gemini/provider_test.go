package gemini

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/leamout/contracts/ai"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestRealtimeDescriptor(t *testing.T) {
	descriptor := (RealtimeProvider{}).Descriptor()
	if err := descriptor.Validate(); err != nil {
		t.Fatal(err)
	}
	if descriptor.ID != "gemini" || descriptor.Kind != ai.KindRealtime {
		t.Fatalf("unexpected descriptor %#v", descriptor)
	}
	for _, capability := range []ai.Capability{
		ai.CapabilityStreaming,
		ai.CapabilityTurnDetection,
		ai.CapabilityToolCalling,
		ai.CapabilityUsage,
		ai.CapabilityBargeIn,
	} {
		if !descriptor.Supports(capability) {
			t.Fatalf("missing capability %q", capability)
		}
	}
}

func TestDecodeRealtimeConfigDefaults(t *testing.T) {
	cfg, err := decodeRealtimeConfig(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Endpoint != DefaultRealtimeEndpoint {
		t.Fatalf("unexpected endpoint %q", cfg.Endpoint)
	}
	if cfg.Model != DefaultRealtimeModel {
		t.Fatalf("unexpected model %q", cfg.Model)
	}
}

func TestValidateRealtimeConfigRejectsInvalidValues(t *testing.T) {
	provider := RealtimeProvider{}
	cases := []json.RawMessage{
		json.RawMessage(`{`),
		json.RawMessage(`{"endpoint":"http://generativelanguage.googleapis.com"}`),
		json.RawMessage(`{"endpoint":"wss://user@example.com/live"}`),
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
		if req.Header.Get("x-goog-api-key") != "key" {
			t.Fatalf("unexpected API key header %q", req.Header.Get("x-goog-api-key"))
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{}`)),
			Header:     make(http.Header),
		}, nil
	})}

	provider := RealtimeProvider{HTTPClient: client}
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

func TestSetupMessage(t *testing.T) {
	request := ai.RealtimeRequest{
		Instructions: "You are helpful.",
		Voice:        "Kore",
		Tools: []ai.ToolDefinition{{
			Name:        "lookup_order",
			Description: "Look up an order.",
			Parameters:  json.RawMessage(`{"type":"object"}`),
		}},
	}
	message := setupMessage(
		RealtimeConfig{Model: "gemini-3.8-live"},
		request,
	)
	if message.Setup == nil {
		t.Fatal("setup message is missing")
	}
	if message.Setup.Model != "models/gemini-3.8-live" {
		t.Fatalf("unexpected model %q", message.Setup.Model)
	}
	if len(message.Setup.Tools) != 1 ||
		len(message.Setup.Tools[0].FunctionDeclarations) != 1 {
		t.Fatal("tool declaration is missing")
	}
	if message.Setup.GenerationConfig.SpeechConfig == nil {
		t.Fatal("speech config is missing")
	}
}
