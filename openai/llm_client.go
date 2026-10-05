package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/leamout/sdk/ai"
)

type llmClient struct {
	httpClient *http.Client
}

func newLLMClient(httpClient *http.Client) *llmClient {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &llmClient{httpClient: httpClient}
}

func (c *llmClient) generate(
	ctx context.Context,
	credential string,
	cfg LLMConfig,
	request ai.LLMRequest,
) (*llmStream, error) {
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

	messages := make([]llmMessage, 0, len(request.Messages)+1)
	if instructions := strings.TrimSpace(request.Instructions); instructions != "" {
		messages = append(messages, llmMessage{Role: string(ai.RoleSystem), Content: instructions})
	}
	for _, source := range request.Messages {
		native := llmMessage{
			Role:       string(source.Role),
			Content:    source.Content,
			ToolCallID: source.ToolCallID,
		}
		for _, call := range source.ToolCalls {
			native.ToolCalls = append(native.ToolCalls, llmMessageCall{
				ID:   call.ID,
				Type: "function",
				Function: llmFunctionCall{
					Name:      call.Name,
					Arguments: string(call.Arguments),
				},
			})
		}
		messages = append(messages, native)
	}

	tools := make([]llmTool, 0, len(request.Tools))
	for _, source := range request.Tools {
		tools = append(tools, llmTool{
			Type: "function",
			Function: llmFunctionDefinition{
				Name:        source.Name,
				Description: source.Description,
				Parameters:  source.Parameters,
			},
		})
	}

	payload, err := json.Marshal(llmCompletionRequest{
		Model:               cfg.Model,
		Messages:            messages,
		Stream:              true,
		StreamOptions:       llmStreamOptions{IncludeUsage: true},
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

	result := newLLMStream(ctx, resp.Body)
	go result.readLoop()
	return result, nil
}
