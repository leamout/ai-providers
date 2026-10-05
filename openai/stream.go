package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"

	"github.com/leamout/ai-providers/internal/transport"
	"github.com/leamout/sdk/ai"
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
				s.emit(ai.LLMEvent{ResponseID: chunk.ID, TextDelta: choice.Delta.Content})
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
