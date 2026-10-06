package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/coder/websocket"
	"github.com/leamout/contracts/ai"
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
	request ai.LLMRequest,
) (*stream, error) {
	if ctx == nil {
		return nil, fmt.Errorf("openai context is required")
	}
	credential = strings.TrimSpace(credential)
	if credential == "" {
		return nil, fmt.Errorf("openai credential is required")
	}
	if len(request.Messages) == 0 &&
		strings.TrimSpace(request.Instructions) == "" {
		return nil, fmt.Errorf("openai messages are required")
	}

	messages := make([]message, 0, len(request.Messages)+1)
	if instructions := strings.TrimSpace(request.Instructions); instructions != "" {
		messages = append(messages, message{
			Role:    string(ai.RoleSystem),
			Content: instructions,
		})
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

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		cfg.Endpoint,
		bytes.NewReader(payload),
	)
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
	if resp.StatusCode < http.StatusOK ||
		resp.StatusCode >= http.StatusMultipleChoices {
		defer func() { _ = resp.Body.Close() }()
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		return nil, fmt.Errorf(
			"start OpenAI stream: HTTP %d: %s",
			resp.StatusCode,
			strings.TrimSpace(string(body)),
		)
	}

	result := newStream(ctx, resp.Body)
	go result.readLoop()
	return result, nil
}

func (c *client) start(
	ctx context.Context,
	cfg RealtimeConfig,
	request ai.RealtimeRequest,
) (ai.RealtimeStream, error) {
	if ctx == nil {
		return nil, fmt.Errorf("openai realtime context is required")
	}

	credential := strings.TrimSpace(request.Runtime.Credential)
	if credential == "" {
		return nil, fmt.Errorf("openai realtime credential is required")
	}

	if err := validateRealtimeFormat(request.InputFormat); err != nil {
		return nil, fmt.Errorf("openai realtime input format: %w", err)
	}
	if err := validateRealtimeFormat(request.OutputFormat); err != nil {
		return nil, fmt.Errorf("openai realtime output format: %w", err)
	}

	parsed, err := url.Parse(cfg.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("parse OpenAI Realtime endpoint: %w", err)
	}

	query := parsed.Query()
	query.Set("model", cfg.Model)
	parsed.RawQuery = query.Encode()

	header := http.Header{
		"Authorization": []string{"Bearer " + credential},
	}
	if safety := strings.TrimSpace(cfg.SafetyIdentifier); safety != "" {
		header.Set("OpenAI-Safety-Identifier", safety)
	}

	connection, response, err := websocket.Dial(
		ctx,
		parsed.String(),
		&websocket.DialOptions{
			HTTPClient:      c.httpClient,
			HTTPHeader:      header,
			CompressionMode: websocket.CompressionDisabled,
		},
	)
	if response != nil && response.Body != nil {
		defer func() { _ = response.Body.Close() }()
	}
	if err != nil {
		if response != nil {
			return nil, fmt.Errorf(
				"connect OpenAI Realtime: HTTP %d: %w",
				response.StatusCode,
				err,
			)
		}
		return nil, fmt.Errorf("connect OpenAI Realtime: %w", err)
	}

	streamCtx, cancel := context.WithCancel(ctx)
	result := &realtimeStream{
		ctx:        streamCtx,
		cancel:     cancel,
		connection: connection,
		format:     request.OutputFormat,
		events:     make(chan ai.RealtimeEvent, 64),
		audio:      make(chan ai.AudioFrame, 32),
	}

	tools := make([]realtimeTool, 0, len(request.Tools))
	for _, source := range request.Tools {
		tools = append(tools, realtimeTool{
			Type:        "function",
			Name:        source.Name,
			Description: source.Description,
			Parameters:  source.Parameters,
		})
	}

	update := realtimeClientEvent{
		Type: "session.update",
		Session: &realtimeSessionUpdate{
			Type:             "realtime",
			Instructions:     request.Instructions,
			OutputModalities: []string{"audio"},
			Audio: realtimeAudioConfig{
				Input: realtimeAudioInput{
					Format: realtimeAudioFormat{
						Type: "audio/pcm",
						Rate: 24000,
					},
				},
				Output: realtimeAudioOutput{
					Format: realtimeAudioFormat{
						Type: "audio/pcm",
						Rate: 24000,
					},
					Voice: strings.TrimSpace(request.Voice),
				},
			},
			Tools: tools,
		},
	}

	if err := result.writeJSON(ctx, update); err != nil {
		_ = result.Close(context.Background())
		return nil, fmt.Errorf(
			"configure OpenAI Realtime session: %w",
			err,
		)
	}

	go result.readLoop()
	return result, nil
}

func validateRealtimeFormat(format ai.AudioFormat) error {
	if err := format.Validate(); err != nil {
		return err
	}
	if format.Encoding != ai.AudioEncodingPCM16LE ||
		format.SampleRateHz != 24000 ||
		format.Channels != 1 {
		return fmt.Errorf("requires mono 24 kHz PCM16LE audio")
	}
	return nil
}
