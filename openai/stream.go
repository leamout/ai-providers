package openai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/leamout/ai-providers/internal/transport"
	"github.com/leamout/contracts/ai"
)

type stream struct {
	ctx       context.Context
	cancel    context.CancelFunc
	body      io.ReadCloser
	events    chan ai.LLMEvent
	closeOnce sync.Once
}

func newStream(parent context.Context, body io.ReadCloser) *stream {
	ctx, cancel := context.WithCancel(parent)
	return &stream{
		ctx:    ctx,
		cancel: cancel,
		body:   body,
		events: make(chan ai.LLMEvent, 32),
	}
}

func (s *stream) Events() <-chan ai.LLMEvent {
	return s.events
}

func (s *stream) Close() error {
	var err error
	s.closeOnce.Do(func() {
		s.cancel()
		err = s.body.Close()
	})
	return err
}

func (s *stream) readLoop() {
	defer close(s.events)
	stop := context.AfterFunc(s.ctx, func() { _ = s.body.Close() })
	defer stop()
	defer func() { _ = s.Close() }()

	completed := false
	err := transport.ReadSSE(s.body, func(data []byte) error {
		if string(data) == "[DONE]" {
			s.emit(ai.LLMEvent{Done: true})
			completed = true
			return io.EOF
		}

		var chunk completionChunk
		if err := json.Unmarshal(data, &chunk); err != nil {
			return fmt.Errorf("decode OpenAI stream chunk: %w", err)
		}
		if chunk.Error != nil {
			return fmt.Errorf("openai stream: %s", chunk.Error.Message)
		}
		if chunk.Usage != nil {
			s.emit(ai.LLMEvent{
				ResponseID:   chunk.ID,
				InputTokens:  chunk.Usage.PromptTokens,
				OutputTokens: chunk.Usage.CompletionTokens,
				TotalTokens:  chunk.Usage.TotalTokens,
			})
		}
		for _, choice := range chunk.Choices {
			if choice.Delta.Content != "" {
				s.emit(ai.LLMEvent{
					ResponseID: chunk.ID,
					TextDelta:  choice.Delta.Content,
				})
			}
			for _, call := range choice.Delta.ToolCalls {
				s.emit(ai.LLMEvent{
					ResponseID:    chunk.ID,
					ToolCallID:    call.ID,
					ToolIndex:     call.Index,
					ToolName:      call.Function.Name,
					ToolArguments: []byte(call.Function.Arguments),
				})
			}
		}
		return nil
	})

	if err != nil && s.ctx.Err() == nil {
		s.emit(ai.LLMEvent{Err: fmt.Errorf("read OpenAI stream: %w", err)})
	}
	if err == nil && !completed && s.ctx.Err() == nil {
		s.emit(ai.LLMEvent{Err: io.ErrUnexpectedEOF})
	}
}

func (s *stream) emit(event ai.LLMEvent) bool {
	select {
	case s.events <- event:
		return true
	case <-s.ctx.Done():
		return false
	}
}

var _ ai.LLMStream = (*stream)(nil)

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

func (s *realtimeStream) SendAudio(
	ctx context.Context,
	frame ai.AudioFrame,
) error {
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
	return s.writeJSON(ctx, realtimeClientEvent{
		Type: "response.cancel",
	})
}

func (s *realtimeStream) SubmitToolResult(
	ctx context.Context,
	result ai.ToolResult,
) error {
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

	return s.writeJSON(ctx, realtimeClientEvent{
		Type: "response.create",
	})
}

func (s *realtimeStream) Audio() <-chan ai.AudioFrame {
	return s.audio
}

func (s *realtimeStream) Events() <-chan ai.RealtimeEvent {
	return s.events
}

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
			if s.ctx.Err() == nil &&
				websocket.CloseStatus(err) != websocket.StatusNormalClosure {
				s.emit(ai.RealtimeEvent{
					Type: ai.RealtimeEventError,
					Failure: &ai.Failure{
						Source:   "openai",
						Message:  fmt.Sprintf("read OpenAI Realtime: %v", err),
						Terminal: true,
					},
					OccurredAt: time.Now().UTC(),
				})
			}
			return
		}

		if kind != websocket.MessageText {
			s.emit(ai.RealtimeEvent{
				Type: ai.RealtimeEventError,
				Failure: &ai.Failure{
					Source:   "openai",
					Message:  "OpenAI Realtime returned a non-text event",
					Terminal: true,
				},
				OccurredAt: time.Now().UTC(),
			})
			return
		}

		var event realtimeServerEvent
		if err := json.Unmarshal(payload, &event); err != nil {
			s.emit(ai.RealtimeEvent{
				Type: ai.RealtimeEventError,
				Failure: &ai.Failure{
					Source:   "openai",
					Message:  fmt.Sprintf("decode OpenAI Realtime event: %v", err),
					Terminal: true,
				},
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
		s.emit(ai.RealtimeEvent{
			Type:       ai.RealtimeEventSpeechStarted,
			ProviderID: providerID,
			OccurredAt: now,
		})

	case "input_audio_buffer.speech_stopped":
		s.emit(ai.RealtimeEvent{
			Type:       ai.RealtimeEventSpeechStopped,
			ProviderID: providerID,
			OccurredAt: now,
		})

	case "conversation.item.input_audio_transcription.delta":
		s.emit(ai.RealtimeEvent{
			Type: ai.RealtimeEventTranscriptDelta,
			Transcript: &ai.TranscriptEvent{
				Text: event.Delta,
			},
			ProviderID: providerID,
			OccurredAt: now,
		})

	case "conversation.item.input_audio_transcription.completed":
		s.emit(ai.RealtimeEvent{
			Type: ai.RealtimeEventTranscriptFinal,
			Transcript: &ai.TranscriptEvent{
				Text: event.Transcript,
			},
			ProviderID: providerID,
			OccurredAt: now,
		})

	case "response.output_audio_transcript.delta", "response.audio_transcript.delta":
		if event.Delta != "" {
			s.emit(ai.RealtimeEvent{
				Type: ai.RealtimeEventResponseDelta,
				Response: &ai.ResponseEvent{
					Text: event.Delta,
				},
				ProviderID: providerID,
				OccurredAt: now,
			})
		}

	case "response.created":
		s.emit(ai.RealtimeEvent{
			Type:       ai.RealtimeEventResponseStarted,
			ProviderID: providerID,
			OccurredAt: now,
		})

	case "response.done":
		s.emitUsage(event, providerID, now)
		s.emit(ai.RealtimeEvent{
			Type:       ai.RealtimeEventResponseStopped,
			ProviderID: providerID,
			OccurredAt: now,
		})

	case "response.audio.delta", "response.output_audio.delta":
		s.handleAudioDelta(event, now)

	case "response.function_call_arguments.delta":

	case "response.function_call_arguments.done":
		s.handleToolCall(event, providerID, now)

	case "error":
		if event.Error != nil {
			s.emit(ai.RealtimeEvent{
				Type: ai.RealtimeEventError,
				Failure: &ai.Failure{
					Source:  "openai",
					Code:    event.Error.Code,
					Message: event.Error.Message,
				},
				ProviderID: providerID,
				OccurredAt: now,
			})
		}
	}
}

func (s *realtimeStream) emitUsage(
	event realtimeServerEvent,
	providerID string,
	occurredAt time.Time,
) {
	if event.Response == nil || len(event.Response.Usage) == 0 {
		return
	}

	var usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
		TotalTokens  int `json:"total_tokens"`
	}
	if err := json.Unmarshal(event.Response.Usage, &usage); err != nil {
		return
	}
	if usage.TotalTokens == 0 {
		usage.TotalTokens = usage.InputTokens + usage.OutputTokens
	}

	s.emit(ai.RealtimeEvent{
		Type: ai.RealtimeEventUsage,
		Usage: &ai.Usage{
			InputTokens:  usage.InputTokens,
			OutputTokens: usage.OutputTokens,
			TotalTokens:  usage.TotalTokens,
		},
		ProviderID: providerID,
		OccurredAt: occurredAt,
	})
}

func (s *realtimeStream) handleAudioDelta(
	event realtimeServerEvent,
	occurredAt time.Time,
) {
	audio, err := base64.StdEncoding.DecodeString(event.Delta)
	if err != nil {
		s.emit(ai.RealtimeEvent{
			Type: ai.RealtimeEventError,
			Failure: &ai.Failure{
				Source:   "openai",
				Message:  err.Error(),
				Terminal: true,
			},
			OccurredAt: occurredAt,
		})
		return
	}

	frame := ai.AudioFrame{
		Data:       audio,
		Format:     s.format,
		CapturedAt: occurredAt,
	}

	select {
	case s.audio <- frame:
	case <-s.ctx.Done():
	}
}

func (s *realtimeStream) handleToolCall(
	event realtimeServerEvent,
	providerID string,
	occurredAt time.Time,
) {
	arguments := event.Arguments
	if arguments == "" {
		arguments = event.Delta
	}

	if !json.Valid([]byte(arguments)) {
		s.emit(ai.RealtimeEvent{
			Type: ai.RealtimeEventError,
			Failure: &ai.Failure{
				Source:  "openai",
				Code:    "invalid_tool_arguments",
				Message: "OpenAI Realtime returned invalid tool arguments",
			},
			ProviderID: providerID,
			OccurredAt: occurredAt,
		})
		return
	}

	s.emit(ai.RealtimeEvent{
		Type: ai.RealtimeEventToolCall,
		ToolCall: &ai.ToolCall{
			ID:        event.CallID,
			Name:      event.Name,
			Arguments: json.RawMessage(arguments),
		},
		ProviderID: providerID,
		OccurredAt: occurredAt,
	})
}

func (s *realtimeStream) emit(event ai.RealtimeEvent) {
	select {
	case s.events <- event:
	case <-s.ctx.Done():
	}
}

var _ ai.RealtimeStream = (*realtimeStream)(nil)
