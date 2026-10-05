package openai

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/leamout/contracts/ai"
)

func TestStreamMapsOpenAIEvents(t *testing.T) {
	body := strings.Join([]string{
		`data: {"id":"resp-1","choices":[{"delta":{"content":"hello","tool_calls":[{"index":0,"id":"call-1","function":{"name":"lookup","arguments":"{\"id\":"}}]}}]}`,
		"",
		`data: {"id":"resp-1","choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"1}"}}]}}]}`,
		"",
		`data: {"id":"resp-1","usage":{"prompt_tokens":2,"completion_tokens":3,"total_tokens":5}}`,
		"",
		`data: [DONE]`,
		"",
	}, "\n")

	stream := newStream(context.Background(), io.NopCloser(strings.NewReader(body)))
	go stream.readLoop()

	var text string
	var toolID string
	var toolName string
	var toolArguments string
	var totalTokens int
	var done bool

	for event := range stream.Events() {
		if event.Err != nil {
			t.Fatal(event.Err)
		}
		text += event.TextDelta
		if event.ToolCallID != "" {
			toolID = event.ToolCallID
		}
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
	if toolID != "call-1" || toolName != "lookup" {
		t.Fatalf("unexpected tool metadata id=%q name=%q", toolID, toolName)
	}
	if toolArguments != `{"id":1}` {
		t.Fatalf("unexpected tool arguments %q", toolArguments)
	}
	if totalTokens != 5 {
		t.Fatalf("unexpected total tokens %d", totalTokens)
	}
	if !done {
		t.Fatal("expected done event")
	}
}

func TestStreamEmitsMalformedEventError(t *testing.T) {
	stream := newStream(context.Background(), io.NopCloser(strings.NewReader("data: {invalid}\n\n")))
	go stream.readLoop()

	var gotErr error
	for event := range stream.Events() {
		if event.Err != nil {
			gotErr = event.Err
		}
	}
	if gotErr == nil {
		t.Fatal("expected malformed stream error")
	}
}

func TestStreamImplementsLLMStream(t *testing.T) {
	var _ ai.LLMStream = (*stream)(nil)
}
