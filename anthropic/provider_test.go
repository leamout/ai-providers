package anthropic

import (
	"encoding/json"
	"testing"

	"github.com/leamout/sdk/ai"
)

func TestDescriptor(t *testing.T) {
	descriptor := (Provider{}).Descriptor()
	if err := descriptor.Validate(); err != nil {
		t.Fatal(err)
	}
	if descriptor.ID != "anthropic" {
		t.Fatalf("unexpected ID %q", descriptor.ID)
	}
	if descriptor.Kind != ai.KindLLM {
		t.Fatalf("unexpected kind %q", descriptor.Kind)
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
	if cfg.MaxTokens != DefaultMaxTokens {
		t.Fatalf("unexpected max tokens %d", cfg.MaxTokens)
	}
}

func TestValidateConfigRejectsInvalidValues(t *testing.T) {
	provider := Provider{}
	cases := []json.RawMessage{
		json.RawMessage(`{`),
		json.RawMessage(`{"endpoint":"http://api.anthropic.com/v1/messages"}`),
		json.RawMessage(`{"max_tokens":-1}`),
		json.RawMessage(`{"temperature":1.5}`),
	}
	for _, raw := range cases {
		if err := provider.ValidateConfig(raw); err == nil {
			t.Fatalf("expected invalid config %s to fail", string(raw))
		}
	}
}

func TestBuildRequestMapsToolsAndSystemMessages(t *testing.T) {
	cfg, err := decodeConfig(nil)
	if err != nil {
		t.Fatal(err)
	}
	input := ai.LLMRequest{
		Instructions: "global instructions",
		Messages: []ai.Message{
			{Role: ai.RoleSystem, Content: "system message"},
			{Role: ai.RoleUser, Content: "find account"},
			{
				Role: ai.RoleAssistant,
				ToolCalls: []ai.ToolCall{
					{
						ID:        "call-1",
						Name:      "lookup",
						Arguments: json.RawMessage(`{"id":1}`),
					},
				},
			},
			{Role: ai.RoleTool, ToolCallID: "call-1", Content: `{"name":"Ada"}`},
		},
		Tools: []ai.ToolDefinition{
			{
				Name:       "lookup",
				Parameters: json.RawMessage(`{"type":"object"}`),
			},
		},
	}

	payload, err := buildRequest(cfg, input)
	if err != nil {
		t.Fatal(err)
	}
	if payload.System != "global instructions\n\nsystem message" {
		t.Fatalf("unexpected system prompt %q", payload.System)
	}
	if len(payload.Messages) != 3 {
		t.Fatalf("unexpected messages count %d", len(payload.Messages))
	}
	if len(payload.Tools) != 1 || payload.Tools[0].Name != "lookup" {
		t.Fatalf("unexpected tools %#v", payload.Tools)
	}
}
