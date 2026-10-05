package deepgram

import (
	"encoding/json"
	"testing"

	"github.com/leamout/sdk/ai"
)

func TestDescriptor(t *testing.T) {
	d := (Provider{}).Descriptor()
	if d.ID != "deepgram" || d.Name != "Deepgram" || d.Kind != ai.KindSTT {
		t.Fatalf("unexpected descriptor: %#v", d)
	}
	if !d.Supports(ai.CapabilityStreaming) || !d.Supports(ai.CapabilityTurnDetection) {
		t.Fatalf("missing expected capabilities: %#v", d.Capabilities)
	}
}

func TestValidateConfig(t *testing.T) {
	provider := Provider{}

	if err := provider.ValidateConfig(nil); err != nil {
		t.Fatalf("ValidateConfig(nil) error = %v", err)
	}

	valid := json.RawMessage(`{"model":"flux-general-multi","language_hints":["en"],"eot_timeout_ms":5000}`)
	if err := provider.ValidateConfig(valid); err != nil {
		t.Fatalf("ValidateConfig(valid) error = %v", err)
	}

	invalid := json.RawMessage(`{"model":"flux-general-en","language_hints":["es"]}`)
	if err := provider.ValidateConfig(invalid); err == nil {
		t.Fatal("ValidateConfig(invalid) error = nil, want error")
	}
}

func TestMapTurnMessage(t *testing.T) {
	events := mapTurnMessage(turnMessage{
		Type:       "TurnInfo",
		Event:      "EndOfTurn",
		RequestID:  "req-1",
		Transcript: "hello world",
	})
	if len(events) != 2 {
		t.Fatalf("len(events) = %d, want 2", len(events))
	}
	if events[0].Type != ai.STTEventTranscriptFinal || events[0].Text != "hello world" {
		t.Fatalf("unexpected transcript event: %#v", events[0])
	}
	if events[1].Type != ai.STTEventSpeechStopped {
		t.Fatalf("unexpected stopped event: %#v", events[1])
	}
}
