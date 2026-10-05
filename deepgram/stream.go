package deepgram

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

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
	if err := frame.Validate(); err != nil {
		return err
	}
	if frame.Format != s.format {
		return fmt.Errorf("deepgram audio format changed during stream")
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.connection.Write(ctx, websocket.MessageBinary, frame.Data)
}

func (s *stream) Finalize(ctx context.Context) error {
	return s.writeJSON(ctx, map[string]string{"type": "ForceEndTurn"})
}

func (s *stream) Events() <-chan ai.STTEvent { return s.events }

func (s *stream) Close(ctx context.Context) error {
	var closeErr error
	s.closeOnce.Do(func() {
		if err := s.writeJSON(ctx, map[string]string{"type": "CloseStream"}); err != nil {
			s.cancel()
			_ = s.connection.CloseNow()
			closeErr = fmt.Errorf("close Deepgram stream: %w", err)
			return
		}
		select {
		case <-s.done:
		case <-ctx.Done():
			s.cancel()
			_ = s.connection.CloseNow()
			closeErr = fmt.Errorf("close Deepgram stream: %w", ctx.Err())
			return
		}
		s.cancel()
		_ = s.connection.CloseNow()
	})
	return closeErr
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
	defer close(s.done)
	defer close(s.events)
	for {
		messageType, payload, err := s.connection.Read(s.ctx)
		if err != nil {
			if s.ctx.Err() == nil && websocket.CloseStatus(err) != websocket.StatusNormalClosure {
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
	case "EagerEndOfTurn":
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
