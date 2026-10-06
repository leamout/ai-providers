package deepgram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"

	"github.com/coder/websocket"
	"github.com/leamout/contracts/ai"
)

type turnMessage struct {
	Type       string `json:"type"`
	Event      string `json:"event"`
	RequestID  string `json:"request_id"`
	Transcript string `json:"transcript"`
}

type stream struct {
	ctx        context.Context
	cancel     context.CancelFunc
	connection *websocket.Conn
	format     ai.AudioFormat
	events     chan ai.STTEvent
	done       chan struct{}
	writeMu    sync.Mutex
	closeOnce  sync.Once
	closing    atomic.Bool
	closeErr   error
}

func newStream(ctx context.Context, cancel context.CancelFunc, connection *websocket.Conn, format ai.AudioFormat) *stream {
	return &stream{
		ctx:        ctx,
		cancel:     cancel,
		connection: connection,
		format:     format,
		events:     make(chan ai.STTEvent, 32),
		done:       make(chan struct{}),
	}
}

func (s *stream) SendAudio(ctx context.Context, frame ai.AudioFrame) error {
	if ctx == nil {
		return fmt.Errorf("deepgram context is required")
	}
	if err := frame.Validate(); err != nil {
		return err
	}
	if frame.Format != s.format {
		return fmt.Errorf("deepgram audio format changed during stream")
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if s.closing.Load() {
		return fmt.Errorf("deepgram stream is closing")
	}
	return s.connection.Write(ctx, websocket.MessageBinary, frame.Data)
}

func (s *stream) Finalize(ctx context.Context) error {
	return s.writeJSON(ctx, map[string]string{"type": "ForceEndTurn"})
}

func (s *stream) Events() <-chan ai.STTEvent { return s.events }

func (s *stream) Close(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("deepgram context is required")
	}
	s.closeOnce.Do(func() {
		select {
		case <-s.done:
			return
		default:
		}
		s.closing.Store(true)
		stop := context.AfterFunc(ctx, func() {
			s.cancel()
			_ = s.connection.CloseNow()
		})
		defer stop()
		if err := s.writeJSON(ctx, map[string]string{"type": "CloseStream"}); err != nil {
			s.cancel()
			_ = s.connection.CloseNow()
			s.closeErr = fmt.Errorf("close Deepgram stream: %w", err)
			return
		}
		select {
		case <-s.done:
		case <-ctx.Done():
			s.closeErr = fmt.Errorf("close Deepgram stream: %w", ctx.Err())
		}
		if ctx.Err() != nil {
			s.closeErr = fmt.Errorf("close Deepgram stream: %w", ctx.Err())
		}
		s.cancel()
		_ = s.connection.CloseNow()
	})
	return s.closeErr
}

func (s *stream) writeJSON(ctx context.Context, value any) error {
	if ctx == nil {
		return fmt.Errorf("deepgram context is required")
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.connection.Write(ctx, websocket.MessageText, payload)
}

func (s *stream) readLoop() {
	defer s.cancel()
	defer func() { _ = s.connection.CloseNow() }()
	defer close(s.done)
	defer close(s.events)
	var pending *turnMessage
	for {
		messageType, payload, err := s.connection.Read(s.ctx)
		if err != nil {
			status := websocket.CloseStatus(err)
			expected := s.closing.Load() && (errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || status == websocket.StatusNormalClosure || status == websocket.StatusNoStatusRcvd)
			if expected && s.ctx.Err() == nil {
				if pending != nil {
					pending.Event = "EndOfTurn"
					for _, event := range mapTurnMessage(*pending) {
						s.emit(event)
					}
				}
			} else if s.ctx.Err() == nil && status != websocket.StatusNormalClosure {
				s.emit(ai.STTEvent{Type: ai.STTEventError, Err: fmt.Errorf("read Deepgram stream: %w", err)})
			}
			return
		}
		if messageType != websocket.MessageText {
			s.emit(ai.STTEvent{Type: ai.STTEventError, Err: fmt.Errorf("deepgram returned a non-text event")})
			return
		}
		var message turnMessage
		if err := json.Unmarshal(payload, &message); err != nil {
			s.emit(ai.STTEvent{Type: ai.STTEventError, Err: fmt.Errorf("decode Deepgram event: %w", err)})
			return
		}
		if message.Type != "TurnInfo" {
			continue
		}
		if message.Event == "EndOfTurn" {
			pending = nil
		} else if message.Transcript != "" {
			copyOfMessage := message
			pending = &copyOfMessage
		}
		for _, event := range mapTurnMessage(message) {
			s.emit(event)
		}
	}
}

func mapTurnMessage(message turnMessage) []ai.STTEvent {
	base := ai.STTEvent{ProviderID: message.RequestID}
	switch message.Event {
	case "StartOfTurn":
		base.Type = ai.STTEventSpeechStarted
		return []ai.STTEvent{base}
	case "TurnResumed":
		base.Type = ai.STTEventSpeechStarted
		result := []ai.STTEvent{base}
		if message.Transcript != "" {
			delta := base
			delta.Type = ai.STTEventTranscriptDelta
			delta.Text = message.Transcript
			result = append(result, delta)
		}
		return result
	case "Update", "EagerEndOfTurn":
		if message.Transcript == "" {
			return nil
		}
		base.Type = ai.STTEventTranscriptDelta
		base.Text = message.Transcript
		return []ai.STTEvent{base}
	case "EndOfTurn":
		result := make([]ai.STTEvent, 0, 2)
		if message.Transcript != "" {
			final := base
			final.Type = ai.STTEventTranscriptFinal
			final.Text = message.Transcript
			result = append(result, final)
		}
		stopped := base
		stopped.Type = ai.STTEventSpeechStopped
		result = append(result, stopped)
		return result
	default:
		return nil
	}
}

func (s *stream) emit(event ai.STTEvent) {
	select {
	case s.events <- event:
	case <-s.ctx.Done():
	}
}

var _ ai.STTStream = (*stream)(nil)
