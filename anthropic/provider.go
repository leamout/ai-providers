// Package anthropic implements the streaming Anthropic Messages API.
package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/leamout/ai-providers/internal/transport"
	"github.com/leamout/sdk/ai"
)

const DefaultEndpoint = "https://api.anthropic.com/v1/messages"
const DefaultModel = "claude-sonnet-4-5"

type Config struct {
	Endpoint    string   `json:"endpoint,omitempty"`
	Model       string   `json:"model,omitempty"`
	MaxTokens   int      `json:"max_tokens,omitempty"`
	Temperature *float64 `json:"temperature,omitempty"`
}
type Provider struct{ HTTPClient *http.Client }

func (Provider) Descriptor() ai.Descriptor {
	return ai.Descriptor{ID: "anthropic", Name: "Anthropic", Kind: ai.KindLLM, Capabilities: []ai.Capability{ai.CapabilityStreaming, ai.CapabilityToolCalling, ai.CapabilityUsage}}
}
func config(raw json.RawMessage) (Config, error) {
	c := Config{Endpoint: DefaultEndpoint, Model: DefaultModel, MaxTokens: 4096}
	if len(raw) > 0 {
		if e := json.Unmarshal(raw, &c); e != nil {
			return c, e
		}
	}
	if e := transport.Endpoint(c.Endpoint, "https"); e != nil {
		return c, e
	}
	if strings.TrimSpace(c.Model) == "" || c.MaxTokens <= 0 {
		return c, errors.New("anthropic model and positive max_tokens are required")
	}
	if c.Temperature != nil && (*c.Temperature < 0 || *c.Temperature > 1) {
		return c, errors.New("anthropic temperature must be between 0 and 1")
	}
	return c, nil
}
func (Provider) ValidateConfig(raw json.RawMessage) error { _, e := config(raw); return e }
func headers(key string) http.Header {
	return http.Header{"X-Api-Key": {key}, "Anthropic-Version": {"2023-06-01"}, "Accept": {"text/event-stream"}}
}
func (p Provider) VerifyCredential(ctx context.Context, key string) error {
	if strings.TrimSpace(key) == "" {
		return errors.New("anthropic credential is required")
	}
	return transport.Verify(ctx, p.HTTPClient, "https://api.anthropic.com/v1/models", headers(key))
}
func (p Provider) Generate(ctx context.Context, r ai.LLMRequest) (ai.LLMStream, error) {
	c, e := config(r.Runtime.Config)
	if e != nil {
		return nil, e
	}
	if strings.TrimSpace(r.Runtime.Credential) == "" {
		return nil, errors.New("anthropic credential is required")
	}
	system := []string{}
	if r.Instructions != "" {
		system = append(system, r.Instructions)
	}
	messages := []any{}
	for _, m := range r.Messages {
		if m.Role == ai.RoleSystem {
			system = append(system, m.Content)
			continue
		}
		role := string(m.Role)
		blocks := []any{}
		if m.Role == ai.RoleTool {
			if m.ToolCallID == "" {
				return nil, errors.New("tool result requires tool_call_id")
			}
			role = "user"
			blocks = append(blocks, map[string]any{"type": "tool_result", "tool_use_id": m.ToolCallID, "content": m.Content})
		} else {
			if m.Role != ai.RoleUser && m.Role != ai.RoleAssistant {
				return nil, fmt.Errorf("unsupported role %q", m.Role)
			}
			if m.Content != "" {
				blocks = append(blocks, map[string]any{"type": "text", "text": m.Content})
			}
			for _, call := range m.ToolCalls {
				if !json.Valid(call.Arguments) {
					return nil, errors.New("invalid tool arguments")
				}
				blocks = append(blocks, map[string]any{"type": "tool_use", "id": call.ID, "name": call.Name, "input": call.Arguments})
			}
		}
		if len(blocks) == 0 {
			return nil, errors.New("empty anthropic message")
		}
		messages = append(messages, map[string]any{"role": role, "content": blocks})
	}
	if len(messages) == 0 {
		return nil, errors.New("anthropic messages are required")
	}
	tools := []any{}
	for _, t := range r.Tools {
		tools = append(tools, map[string]any{"name": t.Name, "description": t.Description, "input_schema": t.Parameters})
	}
	payload := map[string]any{"model": c.Model, "max_tokens": c.MaxTokens, "messages": messages, "stream": true}
	if len(system) > 0 {
		payload["system"] = strings.Join(system, "\n\n")
	}
	if len(tools) > 0 {
		payload["tools"] = tools
	}
	if c.Temperature != nil {
		payload["temperature"] = *c.Temperature
	}
	resp, e := transport.JSON(ctx, p.HTTPClient, http.MethodPost, c.Endpoint, headers(r.Runtime.Credential), payload)
	if e != nil {
		return nil, e
	}
	id := ""
	input, output := 0, 0
	toolArgs := map[int]bool{}
	return transport.NewLLM(ctx, resp.Body, func(b []byte, emit func(ai.LLMEvent) bool) (bool, error) {
		var v struct {
			Type    string `json:"type"`
			Index   int    `json:"index"`
			Message struct {
				ID    string `json:"id"`
				Usage struct {
					Input  int `json:"input_tokens"`
					Output int `json:"output_tokens"`
				} `json:"usage"`
			} `json:"message"`
			Block struct {
				Type string `json:"type"`
				ID   string `json:"id"`
				Name string `json:"name"`
				Text string `json:"text"`
			} `json:"content_block"`
			Delta struct {
				Type string `json:"type"`
				Text string `json:"text"`
				JSON string `json:"partial_json"`
			} `json:"delta"`
			Usage struct {
				Output int `json:"output_tokens"`
			} `json:"usage"`
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if e := json.Unmarshal(b, &v); e != nil {
			return false, e
		}
		switch v.Type {
		case "error":
			return false, fmt.Errorf("anthropic stream: %s", v.Error.Message)
		case "message_start":
			id = v.Message.ID
			input = v.Message.Usage.Input
			output = v.Message.Usage.Output
		case "content_block_start":
			if v.Block.Type == "tool_use" {
				toolArgs[v.Index] = false
				emit(ai.LLMEvent{ResponseID: id, ToolIndex: v.Index, ToolCallID: v.Block.ID, ToolName: v.Block.Name})
			} else if v.Block.Text != "" {
				emit(ai.LLMEvent{ResponseID: id, TextDelta: v.Block.Text})
			}
		case "content_block_delta":
			if v.Delta.Type == "text_delta" {
				emit(ai.LLMEvent{ResponseID: id, TextDelta: v.Delta.Text})
			} else if v.Delta.Type == "input_json_delta" {
				toolArgs[v.Index] = true
				emit(ai.LLMEvent{ResponseID: id, ToolIndex: v.Index, ToolArguments: []byte(v.Delta.JSON)})
			}
		case "content_block_stop":
			if hasArgs, ok := toolArgs[v.Index]; ok && !hasArgs {
				emit(ai.LLMEvent{ResponseID: id, ToolIndex: v.Index, ToolArguments: []byte("{}")})
			}
			delete(toolArgs, v.Index)
		case "message_delta":
			output = v.Usage.Output
			emit(ai.LLMEvent{ResponseID: id, InputTokens: input, OutputTokens: output, TotalTokens: input + output})
		case "message_stop":
			emit(ai.LLMEvent{ResponseID: id, Done: true})
			return true, nil
		}
		return false, nil
	}), nil
}

var _ ai.LLM = Provider{}
var _ ai.ConfigValidator = Provider{}
var _ ai.CredentialVerifier = Provider{}
