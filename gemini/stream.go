package gemini

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/leamout/contracts/ai"
)

type stream struct {
	ctx            context.Context
	cancel         context.CancelFunc
	connection     *websocket.Conn
	inputFormat    ai.AudioFormat
	outputFormat   ai.AudioFormat
	events         chan ai.RealtimeEvent
	audio          chan ai.AudioFrame
	writeMu        sync.Mutex
	closeOnce      sync.Once
	speechActive   bool
	responseActive bool
}

func (s *stream) waitForSetup(ctx context.Context) error {
	kind, payload, err := s.connection.Read(ctx)
	if err != nil {
		return fmt.Errorf("wait for Gemini Live setup: %w", err)
	}
	if kind != websocket.MessageText {
		return fmt.Errorf("Gemini Live returned a non-text setup response")
	}

	var message realtimeServerMessage
	if err := json.Unmarshal(payload, &message); err != nil {
		return fmt.Errorf("decode Gemini Live setup response: %w", err)
	}
	if message.SetupComplete == nil {
		return fmt.Errorf("Gemini Live setup was not acknowledged")
	}
	return nil
}

func (s *stream) SendAudio(ctx context.Context, frame ai.AudioFrame) error {
	if err := frame.Validate(); err != nil {
		return err
	}
	if frame.Format != s.inputFormat {
		return fmt.Errorf("gemini realtime audio format changed during stream")
	}

	return s.writeJSON(ctx, realtimeClientMessage{
		RealtimeInput: &realtimeInput{
			Audio: &realtimeBlob{
				Data: base64.StdEncoding.EncodeToString(frame.Data),
				MimeType: fmt.Sprintf(
					"audio/pcm;rate=%d",
					frame.Format.SampleRateHz,
				),
			},
		},
	})
}

func (s *stream) Interrupt(context.Context) error {
	// Gemini Live performs barge-in through server-side activity detection.
	// The next realtime audio input interrupts the active model turn.
	return nil
}

func (s *stream) SubmitToolResult(ctx context.Context, result ai.ToolResult) error {
	if strings.TrimSpace(result.ToolCallID) == "" {
		return fmt.Errorf("gemini realtime tool_call_id is required")
	}
	if strings.TrimSpace(result.Name) == "" {
		return fmt.Errorf("gemini realtime tool name is required")
	}

	response := make(map[string]any)
	if result.IsError {
		response["error"] = result.Content
	} else if err := json.Unmarshal([]byte(result.Content), &response); err != nil {
		response["result"] = result.Content
	}

	return s.writeJSON(ctx, realtimeClientMessage{
		ToolResponse: &realtimeToolResponse{
			FunctionResponses: []realtimeFunctionResponse{{
				ID:       result.ToolCallID,
				Name:     result.Name,
				Response: response,
			}},
		},
	})
}

func (s *stream) Audio() <-chan ai.AudioFrame     { return s.audio }
func (s *stream) Events() <-chan ai.RealtimeEvent { return s.events }

func (s *stream) Close(context.Context) error {
	var err error
	s.closeOnce.Do(func() {
		s.cancel()
		err = s.connection.CloseNow()
	})
	return err
}

func (s *stream) writeJSON(ctx context.Context, value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.connection.Write(ctx, websocket.MessageText, payload)
}

func (s *stream) readLoop() {
	defer close(s.events)
	defer close(s.audio)
	defer func() { _ = s.Close(context.Background()) }()

	for {
		kind, payload, err := s.connection.Read(s.ctx)
		if err != nil {
			if s.ctx.Err() == nil && websocket.CloseStatus(err) != websocket.StatusNormalClosure {
				s.emit(ai.RealtimeEvent{
					Type: ai.RealtimeEventError,
					Failure: &ai.Failure{
						Source:   "gemini",
						Message:  fmt.Sprintf("read Gemini Live: %v", err),
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
					Source:   "gemini",
					Message:  "Gemini Live returned a non-text event",
					Terminal: true,
				},
				OccurredAt: time.Now().UTC(),
			})
			return
		}

		var message realtimeServerMessage
		if err := json.Unmarshal(payload, &message); err != nil {
			s.emit(ai.RealtimeEvent{
				Type: ai.RealtimeEventError,
				Failure: &ai.Failure{
					Source:   "gemini",
					Message:  fmt.Sprintf("decode Gemini Live event: %v", err),
					Terminal: true,
				},
				OccurredAt: time.Now().UTC(),
			})
			return
		}
		s.handle(message)
	}
}

func (s *stream) handle(message realtimeServerMessage) {
	now := time.Now().UTC()

	if message.UsageMetadata != nil {
		usage := message.UsageMetadata
		s.emit(ai.RealtimeEvent{
			Type: ai.RealtimeEventUsage,
			Usage: &ai.Usage{
				InputTokens:  usage.PromptTokenCount,
				OutputTokens: usage.CandidatesTokenCount,
				TotalTokens:  usage.TotalTokenCount,
			},
			OccurredAt: now,
		})
	}
	if message.ToolCall != nil {
		for _, call := range message.ToolCall.FunctionCalls {
			arguments, err := json.Marshal(call.Args)
			if err != nil {
				continue
			}
			s.emit(ai.RealtimeEvent{
				Type: ai.RealtimeEventToolCall,
				ToolCall: &ai.ToolCall{
					ID:        call.ID,
					Name:      call.Name,
					Arguments: arguments,
				},
				ProviderID: call.ID,
				OccurredAt: now,
			})
		}
	}
	if message.GoAway != nil {
		s.emit(ai.RealtimeEvent{
			Type: ai.RealtimeEventError,
			Failure: &ai.Failure{
				Source:   "gemini",
				Code:     "go_away",
				Message:  "Gemini Live session is closing",
				Terminal: true,
			},
			OccurredAt: now,
		})
	}
	if message.ServerContent != nil {
		s.handleServerContent(*message.ServerContent, now)
	}
}

func (s *stream) handleServerContent(content realtimeServerContent, now time.Time) {
	if content.InterimInputTranscription != nil {
		if !s.speechActive {
			s.speechActive = true
			s.emit(ai.RealtimeEvent{Type: ai.RealtimeEventSpeechStarted, OccurredAt: now})
		}
		if text := content.InterimInputTranscription.Text; text != "" {
			s.emit(ai.RealtimeEvent{
				Type:       ai.RealtimeEventTranscriptDelta,
				Transcript: &ai.TranscriptEvent{Text: text},
				OccurredAt: now,
			})
		}
	}

	if content.InputTranscription != nil {
		if text := content.InputTranscription.Text; text != "" {
			s.emit(ai.RealtimeEvent{
				Type:       ai.RealtimeEventTranscriptFinal,
				Transcript: &ai.TranscriptEvent{Text: text},
				OccurredAt: now,
			})
		}
		if s.speechActive {
			s.speechActive = false
			s.emit(ai.RealtimeEvent{Type: ai.RealtimeEventSpeechStopped, OccurredAt: now})
		}
	}

	if content.OutputTranscription != nil {
		if text := content.OutputTranscription.Text; text != "" {
			s.ensureResponseStarted(now)
			s.emit(ai.RealtimeEvent{
				Type:       ai.RealtimeEventResponseDelta,
				Response:   &ai.ResponseEvent{Text: text},
				OccurredAt: now,
			})
		}
	}

	if content.ModelTurn != nil {
		for _, part := range content.ModelTurn.Parts {
			if part.InlineData == nil || part.InlineData.Data == "" {
				continue
			}
			audio, err := base64.StdEncoding.DecodeString(part.InlineData.Data)
			if err != nil {
				s.emit(ai.RealtimeEvent{
					Type:       ai.RealtimeEventError,
					Failure:    &ai.Failure{Source: "gemini", Message: err.Error()},
					OccurredAt: now,
				})
				continue
			}

			s.ensureResponseStarted(now)
			frame := ai.AudioFrame{Data: audio, Format: s.outputFormat, CapturedAt: now}
			select {
			case s.audio <- frame:
			case <-s.ctx.Done():
				return
			}
		}
	}

	if content.Interrupted || content.GenerationComplete || content.TurnComplete {
		s.finishResponse(now)
	}
}

func (s *stream) ensureResponseStarted(now time.Time) {
	if s.responseActive {
		return
	}
	s.responseActive = true
	s.emit(ai.RealtimeEvent{Type: ai.RealtimeEventResponseStarted, OccurredAt: now})
}

func (s *stream) finishResponse(now time.Time) {
	if !s.responseActive {
		return
	}
	s.responseActive = false
	s.emit(ai.RealtimeEvent{Type: ai.RealtimeEventResponseStopped, OccurredAt: now})
}

func (s *stream) emit(event ai.RealtimeEvent) {
	select {
	case s.events <- event:
	case <-s.ctx.Done():
	}
}

var _ ai.RealtimeStream = (*stream)(nil)
