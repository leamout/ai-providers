package openai

import "encoding/json"

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
