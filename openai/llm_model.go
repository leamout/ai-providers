package openai

import "encoding/json"

type llmMessage struct {
	Role       string           `json:"role"`
	Content    string           `json:"content,omitempty"`
	ToolCalls  []llmMessageCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
}

type llmMessageCall struct {
	ID       string          `json:"id"`
	Type     string          `json:"type"`
	Function llmFunctionCall `json:"function"`
}

type llmFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type llmTool struct {
	Type     string                `json:"type"`
	Function llmFunctionDefinition `json:"function"`
}

type llmFunctionDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"`
}

type llmCompletionRequest struct {
	Model               string           `json:"model"`
	Messages            []llmMessage     `json:"messages"`
	Stream              bool             `json:"stream"`
	StreamOptions       llmStreamOptions `json:"stream_options"`
	Temperature         *float64         `json:"temperature,omitempty"`
	MaxCompletionTokens int              `json:"max_completion_tokens,omitempty"`
	Tools               []llmTool        `json:"tools,omitempty"`
}

type llmStreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type llmCompletionChunk struct {
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
	ID      string      `json:"id"`
	Choices []llmChoice `json:"choices"`
	Usage   *llmUsage   `json:"usage,omitempty"`
}

type llmChoice struct {
	Delta llmDelta `json:"delta"`
}

type llmDelta struct {
	Content   string        `json:"content"`
	ToolCalls []llmToolCall `json:"tool_calls"`
}

type llmToolCall struct {
	Index    int             `json:"index"`
	ID       string          `json:"id"`
	Function llmFunctionCall `json:"function"`
}

type llmUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}
