package openai

import "encoding/json"

type message struct {
	Role       string        `json:"role"`
	Content    string        `json:"content,omitempty"`
	ToolCalls  []messageCall `json:"tool_calls,omitempty"`
	ToolCallID string        `json:"tool_call_id,omitempty"`
}

type messageCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function functionCall `json:"function"`
}

type functionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type tool struct {
	Type     string             `json:"type"`
	Function functionDefinition `json:"function"`
}

type functionDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"`
}

type completionRequest struct {
	Model               string        `json:"model"`
	Messages            []message     `json:"messages"`
	Stream              bool          `json:"stream"`
	StreamOptions       streamOptions `json:"stream_options"`
	Temperature         *float64      `json:"temperature,omitempty"`
	MaxCompletionTokens int           `json:"max_completion_tokens,omitempty"`
	Tools               []tool        `json:"tools,omitempty"`
}

type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type completionChunk struct {
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
	ID      string   `json:"id"`
	Choices []choice `json:"choices"`
	Usage   *usage   `json:"usage,omitempty"`
}

type choice struct {
	Delta delta `json:"delta"`
}

type delta struct {
	Content   string     `json:"content"`
	ToolCalls []toolCall `json:"tool_calls"`
}

type toolCall struct {
	Index    int          `json:"index"`
	ID       string       `json:"id"`
	Function functionCall `json:"function"`
}

type usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type realtimeTool struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"`
}

type realtimeClientEvent struct {
	Type    string                    `json:"type"`
	Session *realtimeSessionUpdate    `json:"session,omitempty"`
	Audio   string                    `json:"audio,omitempty"`
	Item    *realtimeConversationItem `json:"item,omitempty"`
}

type realtimeConversationItem struct {
	Type   string `json:"type"`
	CallID string `json:"call_id,omitempty"`
	Output string `json:"output,omitempty"`
}

type realtimeSessionUpdate struct {
	Type             string              `json:"type,omitempty"`
	Instructions     string              `json:"instructions,omitempty"`
	OutputModalities []string            `json:"output_modalities,omitempty"`
	Audio            realtimeAudioConfig `json:"audio"`
	Tools            []realtimeTool      `json:"tools,omitempty"`
}

type realtimeAudioConfig struct {
	Input  realtimeAudioInput  `json:"input"`
	Output realtimeAudioOutput `json:"output"`
}

type realtimeAudioInput struct {
	Format realtimeAudioFormat `json:"format"`
}

type realtimeAudioOutput struct {
	Format realtimeAudioFormat `json:"format"`
	Voice  string              `json:"voice,omitempty"`
}

type realtimeAudioFormat struct {
	Type string `json:"type"`
	Rate int    `json:"rate"`
}

type realtimeServerEvent struct {
	Type       string            `json:"type"`
	EventID    string            `json:"event_id"`
	CallID     string            `json:"call_id"`
	Name       string            `json:"name"`
	Delta      string            `json:"delta"`
	Transcript string            `json:"transcript"`
	Arguments  string            `json:"arguments"`
	Error      *realtimeAPIError `json:"error"`
	Response   *realtimeResponse `json:"response"`
}

type realtimeAPIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type realtimeResponse struct {
	Usage json.RawMessage `json:"usage"`
}
