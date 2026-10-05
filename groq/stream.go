package groq

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"

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
	defer func() { _ = s.Close() }()

	scanner := bufio.NewScanner(s.body)
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") || !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			s.emit(ai.LLMEvent{Done: true})
			return
		}

		var chunk completionChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			s.emit(ai.LLMEvent{Err: fmt.Errorf("decode Groq stream chunk: %w", err)})
			return
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
	}

	if err := scanner.Err(); err != nil && s.ctx.Err() == nil {
		s.emit(ai.LLMEvent{Err: fmt.Errorf("read Groq stream: %w", err)})
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
