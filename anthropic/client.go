package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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

func (c *client) generate(
	ctx context.Context,
	credential string,
	cfg Config,
	input ai.LLMRequest,
) (*stream, error) {
	if ctx == nil {
		return nil, fmt.Errorf("anthropic context is required")
	}
	credential = strings.TrimSpace(credential)
	if credential == "" {
		return nil, fmt.Errorf("anthropic credential is required")
	}

	payload, err := buildRequest(cfg, input)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode Anthropic request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.Endpoint, bytes.NewReader(encoded))
	if err != nil {
		return nil, fmt.Errorf("create Anthropic request: %w", err)
	}
	setHeaders(req, credential)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("start Anthropic stream: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		defer func() { _ = resp.Body.Close() }()
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		return nil, fmt.Errorf("start Anthropic stream: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	result := newStream(ctx, resp.Body)
	go result.readLoop()
	return result, nil
}

func (c *client) verifyCredential(ctx context.Context, credential string) error {
	if ctx == nil {
		return fmt.Errorf("anthropic context is required")
	}
	credential = strings.TrimSpace(credential)
	if credential == "" {
		return fmt.Errorf("anthropic credential is required")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, DefaultVerifyEndpoint, nil)
	if err != nil {
		return fmt.Errorf("create Anthropic credential verification request: %w", err)
	}
	setHeaders(req, credential)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("verify Anthropic credential: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("verify Anthropic credential: HTTP %d", resp.StatusCode)
	}
	return nil
}

func setHeaders(req *http.Request, credential string) {
	req.Header.Set("X-Api-Key", credential)
	req.Header.Set("Anthropic-Version", APIVersion)
}

func buildRequest(cfg Config, input ai.LLMRequest) (request, error) {
	system := make([]string, 0, 2)
	if value := strings.TrimSpace(input.Instructions); value != "" {
		system = append(system, value)
	}

	messages := make([]message, 0, len(input.Messages))
	for _, source := range input.Messages {
		if source.Role == ai.RoleSystem {
			if value := strings.TrimSpace(source.Content); value != "" {
				system = append(system, value)
			}
			continue
		}

		native := message{}
		switch source.Role {
		case ai.RoleUser, ai.RoleAssistant:
			native.Role = string(source.Role)
			if source.Content != "" {
				native.Content = append(native.Content, contentBlock{Type: "text", Text: source.Content})
			}
			for _, call := range source.ToolCalls {
				if strings.TrimSpace(call.ID) == "" || strings.TrimSpace(call.Name) == "" {
					return request{}, fmt.Errorf("anthropic tool call requires id and name")
				}
				if !json.Valid(call.Arguments) {
					return request{}, fmt.Errorf("anthropic tool call arguments must be valid JSON")
				}
				native.Content = append(native.Content, contentBlock{
					Type:  "tool_use",
					ID:    call.ID,
					Name:  call.Name,
					Input: call.Arguments,
				})
			}
		case ai.RoleTool:
			if strings.TrimSpace(source.ToolCallID) == "" {
				return request{}, fmt.Errorf("anthropic tool result requires tool_call_id")
			}
			native.Role = string(ai.RoleUser)
			native.Content = append(native.Content, contentBlock{
				Type:      "tool_result",
				ToolUseID: source.ToolCallID,
				Content:   source.Content,
			})
		default:
			return request{}, fmt.Errorf("anthropic unsupported role %q", source.Role)
		}

		if len(native.Content) == 0 {
			return request{}, fmt.Errorf("anthropic message content is required")
		}
		messages = append(messages, native)
	}
	if len(messages) == 0 {
		return request{}, fmt.Errorf("anthropic messages are required")
	}

	tools := make([]tool, 0, len(input.Tools))
	for _, source := range input.Tools {
		if strings.TrimSpace(source.Name) == "" {
			return request{}, fmt.Errorf("anthropic tool name is required")
		}
		if len(source.Parameters) == 0 || !json.Valid(source.Parameters) {
			return request{}, fmt.Errorf("anthropic tool parameters must be valid JSON")
		}
		tools = append(tools, tool{
			Name:        source.Name,
			Description: source.Description,
			InputSchema: source.Parameters,
		})
	}

	return request{
		Model:       cfg.Model,
		MaxTokens:   cfg.MaxTokens,
		Messages:    messages,
		Stream:      true,
		System:      strings.Join(system, "\n\n"),
		Tools:       tools,
		Temperature: cfg.Temperature,
	}, nil
}
