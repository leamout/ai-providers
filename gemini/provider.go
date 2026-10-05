// Package gemini implements Google's Gemini streaming generateContent API.
package gemini

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/leamout/ai-providers/internal/transport"
	"github.com/leamout/sdk/ai"
)

const DefaultEndpoint = "https://generativelanguage.googleapis.com/v1beta"
const DefaultModel = "gemini-2.5-flash"

type Config struct {
	Endpoint        string   `json:"endpoint,omitempty"`
	Model           string   `json:"model,omitempty"`
	Temperature     *float64 `json:"temperature,omitempty"`
	MaxOutputTokens int      `json:"max_output_tokens,omitempty"`
}
type Provider struct{ HTTPClient *http.Client }

func (Provider) Descriptor() ai.Descriptor {
	return ai.Descriptor{ID: "gemini", Name: "Gemini", Kind: ai.KindLLM, Capabilities: []ai.Capability{ai.CapabilityStreaming, ai.CapabilityToolCalling, ai.CapabilityUsage}}
}
func config(raw json.RawMessage) (Config, error) {
	c := Config{Endpoint: DefaultEndpoint, Model: DefaultModel}
	if len(raw) > 0 {
		if e := json.Unmarshal(raw, &c); e != nil {
			return c, e
		}
	}
	if e := transport.Endpoint(c.Endpoint, "https"); e != nil {
		return c, e
	}
	if strings.TrimSpace(c.Model) == "" || strings.ContainsAny(c.Model, "/?#") {
		return c, errors.New("gemini model must be a model identifier")
	}
	if c.MaxOutputTokens < 0 {
		return c, errors.New("max_output_tokens must not be negative")
	}
	if c.Temperature != nil && (*c.Temperature < 0 || *c.Temperature > 2) {
		return c, errors.New("temperature must be between 0 and 2")
	}
	return c, nil
}
func (Provider) ValidateConfig(b json.RawMessage) error { _, e := config(b); return e }
func headers(key string) http.Header {
	return http.Header{"X-Goog-Api-Key": {key}, "Accept": {"text/event-stream"}}
}
func (p Provider) VerifyCredential(ctx context.Context, key string) error {
	if strings.TrimSpace(key) == "" {
		return errors.New("gemini credential is required")
	}
	return transport.Verify(ctx, p.HTTPClient, DefaultEndpoint+"/models", headers(key))
}
func (p Provider) Generate(ctx context.Context, r ai.LLMRequest) (ai.LLMStream, error) {
	c, e := config(r.Runtime.Config)
	if e != nil {
		return nil, e
	}
	if strings.TrimSpace(r.Runtime.Credential) == "" {
		return nil, errors.New("gemini credential is required")
	}
	system := []string{}
	if r.Instructions != "" {
		system = append(system, r.Instructions)
	}
	contents := []any{}
	names := map[string]string{}
	for _, m := range r.Messages {
		if m.Role == ai.RoleSystem {
			system = append(system, m.Content)
			continue
		}
		role := "user"
		if m.Role == ai.RoleAssistant {
			role = "model"
		} else if m.Role != ai.RoleUser && m.Role != ai.RoleTool {
			return nil, fmt.Errorf("unsupported role %q", m.Role)
		}
		parts := []any{}
		if m.Role == ai.RoleTool {
			name := names[m.ToolCallID]
			if name == "" {
				return nil, errors.New("gemini tool result must reference a previous tool call")
			}
			var result any
			if json.Unmarshal([]byte(m.Content), &result) != nil {
				result = map[string]any{"result": m.Content}
			}
			if _, ok := result.(map[string]any); !ok {
				result = map[string]any{"result": result}
			}
			parts = append(parts, map[string]any{"functionResponse": map[string]any{"name": name, "response": result}})
		} else {
			if m.Content != "" {
				parts = append(parts, map[string]any{"text": m.Content})
			}
			for _, call := range m.ToolCalls {
				if !json.Valid(call.Arguments) {
					return nil, errors.New("invalid tool arguments")
				}
				names[call.ID] = call.Name
				parts = append(parts, map[string]any{"functionCall": map[string]any{"name": call.Name, "args": call.Arguments}})
			}
		}
		if len(parts) == 0 {
			return nil, errors.New("empty gemini message")
		}
		contents = append(contents, map[string]any{"role": role, "parts": parts})
	}
	if len(contents) == 0 {
		return nil, errors.New("gemini messages are required")
	}
	// Disable thinking: the portable SDK cannot round-trip thought signatures.
	generation := map[string]any{"thinkingConfig": map[string]any{"thinkingBudget": 0}}
	if c.Temperature != nil {
		generation["temperature"] = *c.Temperature
	}
	if c.MaxOutputTokens > 0 {
		generation["maxOutputTokens"] = c.MaxOutputTokens
	}
	payload := map[string]any{"contents": contents, "generationConfig": generation}
	if len(system) > 0 {
		payload["systemInstruction"] = map[string]any{"parts": []any{map[string]any{"text": strings.Join(system, "\n\n")}}}
	}
	if len(r.Tools) > 0 {
		decl := []any{}
		for _, t := range r.Tools {
			decl = append(decl, map[string]any{"name": t.Name, "description": t.Description, "parameters": t.Parameters})
		}
		payload["tools"] = []any{map[string]any{"functionDeclarations": decl}}
	}
	endpoint := strings.TrimRight(c.Endpoint, "/") + "/models/" + url.PathEscape(c.Model) + ":streamGenerateContent?alt=sse"
	resp, e := transport.JSON(ctx, p.HTTPClient, http.MethodPost, endpoint, headers(r.Runtime.Credential), payload)
	if e != nil {
		return nil, e
	}
	index := 0
	return transport.NewLLM(ctx, resp.Body, func(b []byte, emit func(ai.LLMEvent) bool) (bool, error) {
		var v struct {
			ID         string `json:"responseId"`
			Candidates []struct {
				Finish  string `json:"finishReason"`
				Content struct {
					Parts []struct {
						Text    string `json:"text"`
						Thought bool   `json:"thought"`
						Call    *struct {
							Name string          `json:"name"`
							Args json.RawMessage `json:"args"`
						} `json:"functionCall"`
					} `json:"parts"`
				} `json:"content"`
			} `json:"candidates"`
			Usage *struct {
				Input  int `json:"promptTokenCount"`
				Output int `json:"candidatesTokenCount"`
				Total  int `json:"totalTokenCount"`
			} `json:"usageMetadata"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
			Feedback struct {
				Block string `json:"blockReason"`
			} `json:"promptFeedback"`
		}
		if e := json.Unmarshal(b, &v); e != nil {
			return false, e
		}
		if v.Error != nil {
			return false, fmt.Errorf("gemini stream: %s", v.Error.Message)
		}
		if v.Feedback.Block != "" {
			return false, fmt.Errorf("gemini prompt blocked: %s", v.Feedback.Block)
		}
		done := false
		for _, candidate := range v.Candidates {
			for _, p := range candidate.Content.Parts {
				if p.Thought {
					continue
				}
				if p.Text != "" {
					emit(ai.LLMEvent{ResponseID: v.ID, TextDelta: p.Text})
				}
				if p.Call != nil {
					emit(ai.LLMEvent{ResponseID: v.ID, ToolCallID: fmt.Sprintf("gemini-%d", index), ToolIndex: index, ToolName: p.Call.Name, ToolArguments: p.Call.Args})
					index++
				}
			}
			if candidate.Finish != "" {
				if candidate.Finish != "STOP" && candidate.Finish != "MAX_TOKENS" {
					return false, fmt.Errorf("gemini generation stopped: %s", candidate.Finish)
				}
				done = true
			}
		}
		if v.Usage != nil {
			emit(ai.LLMEvent{ResponseID: v.ID, InputTokens: v.Usage.Input, OutputTokens: v.Usage.Output, TotalTokens: v.Usage.Total})
		}
		if done {
			emit(ai.LLMEvent{ResponseID: v.ID, Done: true})
		}
		return done, nil
	}), nil
}

var _ ai.LLM = Provider{}
var _ ai.ConfigValidator = Provider{}
var _ ai.CredentialVerifier = Provider{}
