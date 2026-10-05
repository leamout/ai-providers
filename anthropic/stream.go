package anthropic

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

	responseID string
	input      int
	output     int
	toolArgs   map[int]bool
}

func newStream(parent context.Context, body io.ReadCloser) *stream {
	ctx, cancel := context.WithCancel(parent)
	return &stream{
		ctx:      ctx,
		cancel:   cancel,
		body:     body,
		events:   make(chan ai.LLMEvent, 32),
		toolArgs: make(map[int]bool),
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
		var event streamEvent
		if err := json.Unmarshal(data, &event); err != nil {
			return fmt.Errorf("decode Anthropic stream event: %w", err)
		}

		done, err := s.handleEvent(event)
		if err != nil {
			return err
		}
		if done {
			completed = true
			return io.EOF
		}
		return nil
	})

	if err != nil && s.ctx.Err() == nil {
		s.emit(ai.LLMEvent{Err: fmt.Errorf("read Anthropic stream: %w", err)})
	}
	if err == nil && !completed && s.ctx.Err() == nil {
		s.emit(ai.LLMEvent{Err: io.ErrUnexpectedEOF})
	}
}

func (s *stream) handleEvent(event streamEvent) (bool, error) {
	switch event.Type {
	case "ping":
		return false, nil
	case "error":
		return false, fmt.Errorf("anthropic stream: %s", event.Error.Message)
	case "message_start":
		s.responseID = event.Message.ID
		s.input = event.Message.Usage.InputTokens
		s.output = event.Message.Usage.OutputTokens
	case "content_block_start":
		if event.ContentBlock.Type == "tool_use" {
			s.toolArgs[event.Index] = false
			s.emit(ai.LLMEvent{
				ResponseID: s.responseID,
				ToolIndex:  event.Index,
				ToolCallID: event.ContentBlock.ID,
				ToolName:   event.ContentBlock.Name,
			})
		} else if event.ContentBlock.Text != "" {
			s.emit(ai.LLMEvent{
				ResponseID: s.responseID,
				TextDelta:  event.ContentBlock.Text,
			})
		}
	case "content_block_delta":
		switch event.Delta.Type {
		case "text_delta":
			s.emit(ai.LLMEvent{
				ResponseID: s.responseID,
				TextDelta:  event.Delta.Text,
			})
		case "input_json_delta":
			s.toolArgs[event.Index] = true
			s.emit(ai.LLMEvent{
				ResponseID:    s.responseID,
				ToolIndex:     event.Index,
				ToolArguments: []byte(event.Delta.PartialJSON),
			})
		}
	case "content_block_stop":
		if hasArgs, ok := s.toolArgs[event.Index]; ok && !hasArgs {
			s.emit(ai.LLMEvent{
				ResponseID:    s.responseID,
				ToolIndex:     event.Index,
				ToolArguments: []byte("{}"),
			})
		}
		delete(s.toolArgs, event.Index)
	case "message_delta":
		s.output = event.Usage.OutputTokens
		s.emit(ai.LLMEvent{
			ResponseID:   s.responseID,
			InputTokens:  s.input,
			OutputTokens: s.output,
			TotalTokens:  s.input + s.output,
		})
	case "message_stop":
		s.emit(ai.LLMEvent{ResponseID: s.responseID, Done: true})
		return true, nil
	}
	return false, nil
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
