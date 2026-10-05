package anthropic

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/leamout/sdk/ai"
)

func TestStreamMapsAnthropicEvents(t *testing.T) {
	body := strings.Join([]string{
		`data: {"type":"message_start","message":{"id":"msg-1","usage":{"input_tokens":2,"output_tokens":0}}}`,
		"",
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello"}}`,
		"",
		`data: {"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"call-1","name":"lookup"}}`,
		"",
		`data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"id\":1}"}}`,
		"",
		`data: {"type":"message_delta","usage":{"output_tokens":3}}`,
		"",
		`data: {"type":"message_stop"}`,
		"",
	}, "\n")

	stream := newStream(context.Background(), io.NopCloser(strings.NewReader(body)))
	go stream.readLoop()

	var text string
	var toolName string
	var toolArguments string
	var totalTokens int
	var done bool

	for event := range stream.Events() {
		if event.Err != nil {
			t.Fatal(event.Err)
		}
		text += event.TextDelta
		if event.ToolName != "" {
			toolName = event.ToolName
		}
		toolArguments += string(event.ToolArguments)
		if event.TotalTokens != 0 {
			totalTokens = event.TotalTokens
		}
		done = done || event.Done
	}

	if text != "hello" {
		t.Fatalf("unexpected text %q", text)
	}
	if toolName != "lookup" {
		t.Fatalf("unexpected tool name %q", toolName)
	}
	if toolArguments != `{"id":1}` {
		t.Fatalf("unexpected tool arguments %q", toolArguments)
	}
	if totalTokens != 5 {
		t.Fatalf("unexpected token total %d", totalTokens)
	}
	if !done {
		t.Fatal("expected done event")
	}
}

func TestStreamImplementsLLMStream(t *testing.T) {
	var _ ai.LLMStream = (*stream)(nil)
}
