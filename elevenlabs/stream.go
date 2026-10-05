package elevenlabs

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"sync"

	"github.com/leamout/ai-providers/internal/transport"
	"github.com/leamout/sdk/ai"
)

type stream struct {
	ws     *transport.WS
	format ai.AudioFormat
	events chan ai.TTSEvent
	mu     sync.Mutex
	final  bool
}

func newStream(ws *transport.WS, format ai.AudioFormat) *stream {
	return &stream{
		ws:     ws,
		format: format,
		events: make(chan ai.TTSEvent, 32),
	}
}

func (s *stream) SendText(ctx context.Context, chunk ai.TextChunk) error {
	if ctx == nil {
		return fmt.Errorf("elevenlabs context is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.final {
		return fmt.Errorf("elevenlabs text already finalized")
	}
	if chunk.Text == "" && !chunk.Final {
		return fmt.Errorf("elevenlabs text or final marker is required")
	}

	if chunk.Text != "" {
		text := chunk.Text
		if !strings.HasSuffix(text, " ") {
			text += " "
		}
		if err := s.ws.JSON(ctx, inputMessage{Text: text, TryTriggerGeneration: true}); err != nil {
			return err
		}
	}
	if chunk.Final {
		if err := s.ws.JSON(ctx, inputMessage{Text: ""}); err != nil {
			return err
		}
		s.final = true
	}
	return nil
}

func (s *stream) Events() <-chan ai.TTSEvent {
	return s.events
}

func (s *stream) Close() error {
	return s.ws.Close()
}

func (s *stream) emit(event ai.TTSEvent) bool {
	select {
	case s.events <- event:
		return true
	case <-s.ws.Ctx.Done():
		return false
	}
}

func (s *stream) readLoop() {
	defer close(s.events)
	defer func() { _ = s.Close() }()

	for {
		var message outputMessage
		if err := s.ws.Read(&message); err != nil {
			if s.ws.Ctx.Err() == nil {
				s.emit(ai.TTSEvent{Err: fmt.Errorf("read ElevenLabs stream: %w", err)})
			}
			return
		}
		if message.Error != "" {
			s.emit(ai.TTSEvent{Err: fmt.Errorf("elevenlabs %s: %s", message.Error, message.Message)})
			return
		}
		if message.Audio != "" {
			data, err := base64.StdEncoding.DecodeString(message.Audio)
			if err != nil {
				s.emit(ai.TTSEvent{Err: fmt.Errorf("decode ElevenLabs audio: %w", err)})
				return
			}
			frame := ai.AudioFrame{Data: data, Format: s.format}
			if err := frame.Validate(); err != nil {
				s.emit(ai.TTSEvent{Err: err})
				return
			}
			if !s.emit(ai.TTSEvent{Audio: frame}) {
				return
			}
		}
		if message.Final || message.LegacyFinal {
			s.emit(ai.TTSEvent{Done: true})
			return
		}
	}
}

var _ ai.TTSStream = (*stream)(nil)
