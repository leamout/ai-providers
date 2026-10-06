package openai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/leamout/contracts/ai"
)

type realtimeClient struct {
	httpClient *http.Client
}

func newRealtimeClient(httpClient *http.Client) *realtimeClient {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &realtimeClient{httpClient: httpClient}
}

func (c *realtimeClient) start(ctx context.Context, cfg RealtimeConfig, request ai.RealtimeRequest) (ai.RealtimeStream, error) {
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

	header := http.Header{"Authorization": []string{"Bearer " + credential}}
	if safety := strings.TrimSpace(cfg.SafetyIdentifier); safety != "" {
		header.Set("OpenAI-Safety-Identifier", safety)
	}
	connection, response, err := websocket.Dial(ctx, parsed.String(), &websocket.DialOptions{
		HTTPClient:      c.httpClient,
		HTTPHeader:      header,
		CompressionMode: websocket.CompressionDisabled,
	})
	if response != nil && response.Body != nil {
		defer func() { _ = response.Body.Close() }()
	}
	if err != nil {
		if response != nil {
			return nil, fmt.Errorf("connect OpenAI Realtime: HTTP %d: %w", response.StatusCode, err)
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
	for _, tool := range request.Tools {
		tools = append(tools, realtimeTool{
			Type:        "function",
			Name:        tool.Name,
			Description: tool.Description,
			Parameters:  tool.Parameters,
		})
	}
	update := realtimeClientEvent{
		Type: "session.update",
		Session: &realtimeSessionUpdate{
			Type:             "realtime",
			Instructions:     request.Instructions,
			OutputModalities: []string{"audio"},
			Audio: realtimeAudioConfig{
				Input: realtimeAudioInput{Format: realtimeAudioFormat{Type: "audio/pcm", Rate: 24000}},
				Output: realtimeAudioOutput{
					Format: realtimeAudioFormat{Type: "audio/pcm", Rate: 24000},
					Voice:  strings.TrimSpace(request.Voice),
				},
			},
			Tools: tools,
		},
	}
	if err := result.writeJSON(ctx, update); err != nil {
		_ = result.Close(context.Background())
		return nil, fmt.Errorf("configure OpenAI Realtime session: %w", err)
	}
	go result.readLoop()
	return result, nil
}

func validateRealtimeFormat(format ai.AudioFormat) error {
	if err := format.Validate(); err != nil {
		return err
	}
	if format.Encoding != ai.AudioEncodingPCM16LE || format.SampleRateHz != 24000 || format.Channels != 1 {
		return fmt.Errorf("requires mono 24 kHz PCM16LE audio")
	}
	return nil
}

type realtimeStream struct {
	ctx        context.Context
	cancel     context.CancelFunc
	connection *websocket.Conn
	format     ai.AudioFormat
	events     chan ai.RealtimeEvent
	audio      chan ai.AudioFrame
	writeMu    sync.Mutex
	closeOnce  sync.Once
}

func (s *realtimeStream) SendAudio(ctx context.Context, frame ai.AudioFrame) error {
	if err := frame.Validate(); err != nil {
		return err
	}
	if frame.Format != s.format {
		return fmt.Errorf("openai realtime audio format changed during stream")
	}
	return s.writeJSON(ctx, realtimeClientEvent{
		Type:  "input_audio_buffer.append",
		Audio: base64.StdEncoding.EncodeToString(frame.Data),
	})
}

func (s *realtimeStream) Interrupt(ctx context.Context) error {
	return s.writeJSON(ctx, realtimeClientEvent{Type: "response.cancel"})
}

func (s *realtimeStream) SubmitToolResult(ctx context.Context, result ai.ToolResult) error {
	if strings.TrimSpace(result.ToolCallID) == "" {
		return fmt.Errorf("openai realtime tool_call_id is required")
	}
	if err := s.writeJSON(ctx, realtimeClientEvent{
		Type: "conversation.item.create",
		Item: &realtimeConversationItem{
			Type:   "function_call_output",
			CallID: result.ToolCallID,
			Output: result.Content,
		},
	}); err != nil {
		return err
	}
	return s.writeJSON(ctx, realtimeClientEvent{Type: "response.create"})
}

func (s *realtimeStream) Audio() <-chan ai.AudioFrame     { return s.audio }
func (s *realtimeStream) Events() <-chan ai.RealtimeEvent { return s.events }

func (s *realtimeStream) Close(context.Context) error {
	var err error
	s.closeOnce.Do(func() {
		s.cancel()
		err = s.connection.CloseNow()
	})
	return err
}

func (s *realtimeStream) writeJSON(ctx context.Context, value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.connection.Write(ctx, websocket.MessageText, payload)
}

func (s *realtimeStream) readLoop() {
	defer close(s.events)
	defer close(s.audio)
	defer func() { _ = s.Close(context.Background()) }()
	for {
		kind, payload, err := s.connection.Read(s.ctx)
		if err != nil {
			if s.ctx.Err() == nil && websocket.CloseStatus(err) != websocket.StatusNormalClosure {
				s.emit(ai.RealtimeEvent{
					Type:       ai.RealtimeEventError,
					Failure:    &ai.Failure{Source: "openai", Message: fmt.Sprintf("read OpenAI Realtime: %v", err), Terminal: true},
					OccurredAt: time.Now().UTC(),
				})
			}
			return
		}
		if kind != websocket.MessageText {
			s.emit(ai.RealtimeEvent{
				Type:       ai.RealtimeEventError,
				Failure:    &ai.Failure{Source: "openai", Message: "OpenAI Realtime returned a non-text event", Terminal: true},
				OccurredAt: time.Now().UTC(),
			})
			return
		}
		var event realtimeServerEvent
		if err := json.Unmarshal(payload, &event); err != nil {
			s.emit(ai.RealtimeEvent{
				Type:       ai.RealtimeEventError,
				Failure:    &ai.Failure{Source: "openai", Message: fmt.Sprintf("decode OpenAI Realtime event: %v", err), Terminal: true},
				OccurredAt: time.Now().UTC(),
			})
			return
		}
		s.handle(event)
	}
}

func (s *realtimeStream) handle(event realtimeServerEvent) {
	now := time.Now().UTC()
	providerID := event.EventID
	switch event.Type {
	case "input_audio_buffer.speech_started":
		s.emit(ai.RealtimeEvent{Type: ai.RealtimeEventSpeechStarted, ProviderID: providerID, OccurredAt: now})
	case "input_audio_buffer.speech_stopped":
		s.emit(ai.RealtimeEvent{Type: ai.RealtimeEventSpeechStopped, ProviderID: providerID, OccurredAt: now})
	case "conversation.item.input_audio_transcription.delta":
		s.emit(ai.RealtimeEvent{Type: ai.RealtimeEventTranscriptDelta, Transcript: &ai.TranscriptEvent{Text: event.Delta}, ProviderID: providerID, OccurredAt: now})
	case "conversation.item.input_audio_transcription.completed":
		s.emit(ai.RealtimeEvent{Type: ai.RealtimeEventTranscriptFinal, Transcript: &ai.TranscriptEvent{Text: event.Transcript}, ProviderID: providerID, OccurredAt: now})
	case "response.output_audio_transcript.delta", "response.audio_transcript.delta":
		if event.Delta != "" {
			s.emit(ai.RealtimeEvent{Type: ai.RealtimeEventResponseDelta, Response: &ai.ResponseEvent{Text: event.Delta}, ProviderID: providerID, OccurredAt: now})
		}
	case "response.created":
		s.emit(ai.RealtimeEvent{Type: ai.RealtimeEventResponseStarted, ProviderID: providerID, OccurredAt: now})
	case "response.done":
		if event.Response != nil && len(event.Response.Usage) != 0 {
			var usage struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
				TotalTokens  int `json:"total_tokens"`
			}
			if json.Unmarshal(event.Response.Usage, &usage) == nil {
				if usage.TotalTokens == 0 {
					usage.TotalTokens = usage.InputTokens + usage.OutputTokens
				}
				s.emit(ai.RealtimeEvent{Type: ai.RealtimeEventUsage, Usage: &ai.Usage{InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens, TotalTokens: usage.TotalTokens}, ProviderID: providerID, OccurredAt: now})
			}
		}
		s.emit(ai.RealtimeEvent{Type: ai.RealtimeEventResponseStopped, ProviderID: providerID, OccurredAt: now})
	case "response.audio.delta", "response.output_audio.delta":
		audio, err := base64.StdEncoding.DecodeString(event.Delta)
		if err != nil {
			s.emit(ai.RealtimeEvent{Type: ai.RealtimeEventError, Failure: &ai.Failure{Source: "openai", Message: err.Error(), Terminal: true}, OccurredAt: now})
			return
		}
		frame := ai.AudioFrame{Data: audio, Format: s.format, CapturedAt: now}
		select {
		case s.audio <- frame:
		case <-s.ctx.Done():
		}
	case "response.function_call_arguments.delta":
	case "response.function_call_arguments.done":
		arguments := event.Arguments
		if arguments == "" {
			arguments = event.Delta
		}
		if !json.Valid([]byte(arguments)) {
			s.emit(ai.RealtimeEvent{
				Type:       ai.RealtimeEventError,
				Failure:    &ai.Failure{Source: "openai", Code: "invalid_tool_arguments", Message: "OpenAI Realtime returned invalid tool arguments"},
				ProviderID: providerID,
				OccurredAt: now,
			})
			return
		}
		s.emit(ai.RealtimeEvent{Type: ai.RealtimeEventToolCall, ToolCall: &ai.ToolCall{ID: event.CallID, Name: event.Name, Arguments: json.RawMessage(arguments)}, ProviderID: providerID, OccurredAt: now})
	case "error":
		if event.Error != nil {
			s.emit(ai.RealtimeEvent{Type: ai.RealtimeEventError, Failure: &ai.Failure{Source: "openai", Code: event.Error.Code, Message: event.Error.Message}, ProviderID: providerID, OccurredAt: now})
		}
	}
}

func (s *realtimeStream) emit(event ai.RealtimeEvent) {
	select {
	case s.events <- event:
	case <-s.ctx.Done():
	}
}

var _ ai.RealtimeStream = (*realtimeStream)(nil)
