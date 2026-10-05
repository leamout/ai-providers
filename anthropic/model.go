package anthropic

import "encoding/json"

type message struct {
	Role    string         `json:"role"`
	Content []contentBlock `json:"content"`
}

type contentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   string          `json:"content,omitempty"`
}

type tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type request struct {
	Model       string   `json:"model"`
	MaxTokens   int      `json:"max_tokens"`
	Messages    []message `json:"messages"`
	Stream      bool     `json:"stream"`
	System      string   `json:"system,omitempty"`
	Tools       []tool   `json:"tools,omitempty"`
	Temperature *float64 `json:"temperature,omitempty"`
}

type streamEvent struct {
	Type  string `json:"type"`
	Index int    `json:"index"`

	Message struct {
		ID    string `json:"id"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	} `json:"message"`

	ContentBlock struct {
		Type string `json:"type"`
		ID   string `json:"id"`
		Name string `json:"name"`
		Text string `json:"text"`
	} `json:"content_block"`

	Delta struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		PartialJSON string `json:"partial_json"`
	} `json:"delta"`

	Usage struct {
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`

	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}
