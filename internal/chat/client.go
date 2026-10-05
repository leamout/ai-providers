package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/leamout/sdk/ai"
)

type client struct {
	httpClient *http.Client
}

func newClient(httpClient *http.Client) *client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &client{httpClient: httpClient}
}

func (c *client) generate(ctx context.Context, credential string, cfg Config, request ai.LLMRequest) (*stream, error) {
	if ctx == nil {
		return nil, fmt.Errorf("openai context is required")
	}
	credential = strings.TrimSpace(credential)
	if credential == "" {
		return nil, fmt.Errorf("openai credential is required")
	}
	if len(request.Messages) == 0 && strings.TrimSpace(request.Instructions) == "" {
		return nil, fmt.Errorf("openai messages are required")
	}

	messages := make([]message, 0, len(request.Messages)+1)
	if instructions := strings.TrimSpace(request.Instructions); instructions != "" {
		messages = append(messages, message{Role: string(ai.RoleSystem), Content: instructions})
	}
	for _, source := range request.Messages {
		native := message{
			Role:       string(source.Role),
			Content:    source.Content,
			ToolCallID: source.ToolCallID,
		}
		for _, call := range source.ToolCalls {
			native.ToolCalls = append(native.ToolCalls, messageCall{
				ID:   call.ID,
				Type: "function",
				Function: functionCall{
					Name:      call.Name,
					Arguments: string(call.Arguments),
				},
			})
		}
		messages = append(messages, native)
	}

	tools := make([]tool, 0, len(request.Tools))
	for _, source := range request.Tools {
		tools = append(tools, tool{
			Type: "function",
			Function: functionDefinition{
				Name:        source.Name,
				Description: source.Description,
				Parameters:  source.Parameters,
			},
		})
	}

	payload, err := json.Marshal(completionRequest{
		Model:               cfg.Model,
		Messages:            messages,
		Stream:              true,
		StreamOptions:       streamOptions{IncludeUsage: true},
		Temperature:         cfg.Temperature,
		MaxCompletionTokens: cfg.MaxCompletionTokens,
		Tools:               tools,
	})
	if err != nil {
		return nil, fmt.Errorf("encode OpenAI request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.Endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create OpenAI request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+credential)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("start OpenAI stream: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer func() { _ = resp.Body.Close() }()
		return nil, fmt.Errorf("start OpenAI stream: HTTP %d", resp.StatusCode)
	}

	result := newStream(ctx, resp.Body)
	go result.readLoop()
	return result, nil
}
