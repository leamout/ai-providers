package groq

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/leamout/contracts/ai"
)

func TestGenerateTranslatesRequestAndEvents(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost {
			t.Fatalf("method = %s", req.Method)
		}
		if got := req.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("Authorization = %q", got)
		}
		var payload completionRequest
		if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if payload.Model != "test-model" || !payload.Stream || !payload.StreamOptions.IncludeUsage {
			t.Fatalf("unexpected payload: %#v", payload)
		}
		if len(payload.Messages) != 2 || payload.Messages[0].Role != "system" || payload.Messages[1].Role != "user" {
			t.Fatalf("unexpected messages: %#v", payload.Messages)
		}
		if len(payload.Tools) != 1 || payload.Tools[0].Function.Name != "lookup" {
			t.Fatalf("unexpected tools: %#v", payload.Tools)
		}

		body := strings.Join([]string{
			`data: {"id":"resp-1","choices":[{"delta":{"content":"hello"}}]}`,
			"",
			`data: {"id":"resp-1","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call-1","function":{"name":"lookup","arguments":"{\"id\":"}}]}}]}`,
			"",
			`data: {"id":"resp-1","choices":[],"usage":{"prompt_tokens":2,"completion_tokens":3,"total_tokens":5}}`,
			"",
			`data: [DONE]`,
			"",
		}, "\n")
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(body)),
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		}, nil
	})}

	provider := Provider{HTTPClient: client}
	stream, err := provider.Generate(context.Background(), ai.LLMRequest{
		Runtime: ai.Runtime{
			Credential: "test-key",
			Config:     json.RawMessage(`{"endpoint":"https://example.test/v1/chat/completions","model":"test-model"}`),
		},
		Instructions: "be concise",
		Messages:     []ai.Message{{Role: ai.RoleUser, Content: "hi"}},
		Tools: []ai.ToolDefinition{{
			Name:       "lookup",
			Parameters: json.RawMessage(`{"type":"object"}`),
		}},
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	defer func() { _ = stream.Close() }()

	var events []ai.LLMEvent
	for event := range stream.Events() {
		events = append(events, event)
	}
	if len(events) != 4 {
		t.Fatalf("event count = %d, want 4: %#v", len(events), events)
	}
	if events[0].TextDelta != "hello" {
		t.Fatalf("text delta = %q", events[0].TextDelta)
	}
	if events[1].ToolCallID != "call-1" || events[1].ToolName != "lookup" {
		t.Fatalf("unexpected tool event: %#v", events[1])
	}
	if events[2].TotalTokens != 5 {
		t.Fatalf("usage = %#v", events[2])
	}
	if !events[3].Done {
		t.Fatalf("final event = %#v", events[3])
	}
}
