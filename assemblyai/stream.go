package assemblyai

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/leamout/ai-providers/internal/transport"
	"github.com/leamout/sdk/ai"
)

type stream struct {
	ws          *transport.WS
	format      ai.AudioFormat
	formatTurns bool
	events      chan ai.STTEvent
	done        chan struct{}
	mu          sync.Mutex
	closing     bool
}

func newStream(ws *transport.WS, format ai.AudioFormat, formatTurns bool) *stream {
	return &stream{
		ws:          ws,
		format:      format,
		formatTurns: formatTurns,
		events:      make(chan ai.STTEvent, 32),
		done:        make(chan struct{}),
	}
}

func (s *stream) SendAudio(ctx context.Context, frame ai.AudioFrame) error {
	if ctx == nil {
		return fmt.Errorf("assemblyai context is required")
	}
	if err := frame.Validate(); err != nil {
		return err
	}
	if frame.Format != s.format {
		return fmt.Errorf("assemblyai audio format changed")
	}
	duration := frame.Duration()
	if duration < 50*time.Millisecond || duration > time.Second {
		return fmt.Errorf("assemblyai audio chunks must contain 50–1000 ms")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		return fmt.Errorf("assemblyai stream is closing")
	}
	return s.ws.Write(ctx, websocket.MessageBinary, frame.Data)
}

func (s *stream) Finalize(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("assemblyai context is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		return fmt.Errorf("assemblyai stream is closing")
	}
	return s.ws.JSON(ctx, controlMessage{Type: "ForceEndpoint"})
}

func (s *stream) Events() <-chan ai.STTEvent {
	return s.events
}

func (s *stream) Close(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("assemblyai context is required")
	}

	s.mu.Lock()
	if !s.closing {
		s.closing = true
		if err := s.ws.JSON(ctx, controlMessage{Type: "Terminate"}); err != nil {
			s.mu.Unlock()
			_ = s.ws.Close()
			return err
		}
	}
	s.mu.Unlock()

	select {
	case <-s.done:
		return s.ws.Close()
	case <-ctx.Done():
		_ = s.ws.Close()
		return ctx.Err()
	}
}

func (s *stream) readLoop() {
	defer close(s.done)
	defer close(s.events)
	defer func() { _ = s.ws.Close() }()

	providerID := ""
	for {
		var upstream event
		if err := s.ws.Read(&upstream); err != nil {
			if s.ws.Ctx.Err() == nil {
				s.emit(ai.STTEvent{Type: ai.STTEventError, Err: err})
			}
			return
		}
		if upstream.Error != "" {
			s.emit(ai.STTEvent{Type: ai.STTEventError, Err: fmt.Errorf("assemblyai: %s", upstream.Error)})
			return
		}

		switch upstream.Type {
		case "Begin":
			providerID = upstream.ID
		case "Turn":
			if upstream.Transcript != "" {
				typeOfEvent := ai.STTEventTranscriptDelta
				if upstream.End && (!s.formatTurns || upstream.Formatted) {
					typeOfEvent = ai.STTEventTranscriptFinal
				}
				if !s.emit(ai.STTEvent{Type: typeOfEvent, Text: upstream.Transcript, ProviderID: providerID}) {
					return
				}
			}
			if upstream.End && !upstream.Formatted {
				if !s.emit(ai.STTEvent{Type: ai.STTEventSpeechStopped, ProviderID: providerID}) {
					return
				}
			}
		case "Termination":
			return
		}
	}
}

func (s *stream) emit(event ai.STTEvent) bool {
	select {
	case s.events <- event:
		return true
	case <-s.ws.Ctx.Done():
		return false
	}
}

var _ ai.STTStream = (*stream)(nil)
