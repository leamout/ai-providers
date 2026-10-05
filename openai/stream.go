package openai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"

	"github.com/leamout/ai-providers/internal/transport"
	"github.com/leamout/sdk/ai"
)

type llmStream struct {
	ctx       context.Context
	cancel    context.CancelFunc
	body      io.ReadCloser
	events    chan ai.LLMEvent
	closeOnce sync.Once
}

func newLLMStream(parent context.Context, body io.ReadCloser) *llmStream {
	ctx, cancel := context.WithCancel(parent)
	return &llmStream{
		ctx:    ctx,
		cancel: cancel,
		body:   body,
		events: make(chan ai.LLMEvent, 32),
	}
}

func (s *llmStream) Events() <-chan ai.LLMEvent {
	return s.events
}

func (s *llmStream) Close() error {
	var err error
	s.closeOnce.Do(func() {
		s.cancel()
		err = s.body.Close()
	})
	return err
}

func (s *llmStream) readLoop() {
	defer close(s.events)
	stop := context.AfterFunc(s.ctx, func() { _ = s.body.Close() })
	defer stop()
	defer func() { _ = s.Close() }()

	completed := false
	err := transport.ReadSSE(s.body, func(data []byte) error {
		if string(data) == "[DONE]" {
			s.emitLLM(ai.LLMEvent{Done: true})
			completed = true
			return io.EOF
		}

		var chunk completionChunk
		if err := json.Unmarshal(data, &chunk); err != nil {
			return fmt.Errorf("decode OpenAI stream chunk: %w", err)
		}
		if chunk.Error != nil {
			return fmt.Errorf("OpenAI stream: %s", chunk.Error.Message)
		}
		if chunk.Usage != nil {
			s.emitLLM(ai.LLMEvent{
				ResponseID:   chunk.ID,
				InputTokens:  chunk.Usage.PromptTokens,
				OutputTokens: chunk.Usage.CompletionTokens,
				TotalTokens:  chunk.Usage.TotalTokens,
			})
		}
		for _, choice := range chunk.Choices {
			if choice.Delta.Content != "" {
				s.emitLLM(ai.LLMEvent{ResponseID: chunk.ID, TextDelta: choice.Delta.Content})
			}
			for _, call := range choice.Delta.ToolCalls {
				s.emitLLM(ai.LLMEvent{
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
		s.emitLLM(ai.LLMEvent{Err: fmt.Errorf("read OpenAI stream: %w", err)})
	}
	if err == nil && !completed && s.ctx.Err() == nil {
		s.emitLLM(ai.LLMEvent{Err: io.ErrUnexpectedEOF})
	}
}

func (s *llmStream) emitLLM(event ai.LLMEvent) bool {
	select {
	case s.events <- event:
		return true
	case <-s.ctx.Done():
		return false
	}
}

type ttsStream struct {
	ctx        context.Context
	cancel     context.CancelFunc
	format     ai.AudioFormat
	synthesize func(context.Context, string) (*http.Response, error)
	input      chan ai.TextChunk
	events     chan ai.TTSEvent
	mu         sync.Mutex
	final      bool
	closeOnce  sync.Once
}

func newTTSStream(
	parent context.Context,
	format ai.AudioFormat,
	synthesize func(context.Context, string) (*http.Response, error),
) *ttsStream {
	ctx, cancel := context.WithCancel(parent)
	return &ttsStream{
		ctx:        ctx,
		cancel:     cancel,
		format:     format,
		synthesize: synthesize,
		input:      make(chan ai.TextChunk, 16),
		events:     make(chan ai.TTSEvent, 32),
	}
}

func (s *ttsStream) SendText(ctx context.Context, chunk ai.TextChunk) error {
	if ctx == nil {
		return fmt.Errorf("context is required")
	}
	if chunk.Text == "" && !chunk.Final {
		return fmt.Errorf("text or final marker is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.final || s.ctx.Err() != nil {
		return fmt.Errorf("openai TTS stream is closed or finalized")
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-s.ctx.Done():
		return s.ctx.Err()
	case s.input <- chunk:
		s.final = chunk.Final
		return nil
	}
}

func (s *ttsStream) Events() <-chan ai.TTSEvent {
	return s.events
}

func (s *ttsStream) Close() error {
	s.closeOnce.Do(s.cancel)
	return nil
}

func (s *ttsStream) run() {
	defer close(s.events)
	defer func() { _ = s.Close() }()

	for {
		select {
		case <-s.ctx.Done():
			return
		case chunk := <-s.input:
			if chunk.Text != "" {
				resp, err := s.synthesize(s.ctx, chunk.Text)
				if err != nil {
					s.emitTTS(ai.TTSEvent{Err: err})
					return
				}
				if err := s.readAudio(resp.Body); err != nil {
					_ = resp.Body.Close()
					if s.ctx.Err() == nil {
						s.emitTTS(ai.TTSEvent{Err: err})
					}
					return
				}
				_ = resp.Body.Close()
			}
			if chunk.Final {
				s.emitTTS(ai.TTSEvent{Done: true})
				return
			}
		}
	}
}

func (s *ttsStream) readAudio(reader io.Reader) error {
	pending := make([]byte, 0, 8192)
	buf := make([]byte, 8192)
	for {
		n, err := reader.Read(buf)
		pending = append(pending, buf[:n]...)
		size := len(pending) / 2 * 2
		if size > 0 {
			data := append([]byte(nil), pending[:size]...)
			pending = pending[size:]
			if !s.emitTTS(ai.TTSEvent{Audio: ai.AudioFrame{Data: data, Format: s.format}}) {
				return s.ctx.Err()
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				if len(pending) != 0 {
					return fmt.Errorf("openai returned incomplete PCM16 sample")
				}
				return nil
			}
			return err
		}
	}
}

func (s *ttsStream) emitTTS(event ai.TTSEvent) bool {
	select {
	case s.events <- event:
		return true
	case <-s.ctx.Done():
		return false
	}
}

var _ ai.LLMStream = (*llmStream)(nil)
var _ ai.TTSStream = (*ttsStream)(nil)
