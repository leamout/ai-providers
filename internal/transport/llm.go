package transport

import (
	"context"
	"io"
	"sync"

	"github.com/leamout/sdk/ai"
)

type LLMStream struct {
	ctx    context.Context
	cancel context.CancelFunc
	body   io.ReadCloser
	events chan ai.LLMEvent
	once   sync.Once
}

// NewLLM parses a provider's SSE stream. mapEvent returns true only on a terminal event.
func NewLLM(parent context.Context, body io.ReadCloser, mapEvent func([]byte, func(ai.LLMEvent) bool) (bool, error)) ai.LLMStream {
	ctx, cancel := context.WithCancel(parent)
	s := &LLMStream{ctx: ctx, cancel: cancel, body: body, events: make(chan ai.LLMEvent, 32)}
	go func() {
		defer close(s.events)
		defer s.Close()
		stop := context.AfterFunc(ctx, func() { body.Close() })
		defer stop()
		done := false
		err := ReadSSE(body, func(b []byte) error {
			var e error
			done, e = mapEvent(b, s.emit)
			if e != nil {
				return e
			}
			if done {
				return io.EOF
			}
			return nil
		})
		if ctx.Err() == nil {
			if err != nil {
				s.emit(ai.LLMEvent{Err: err})
			} else if !done {
				s.emit(ai.LLMEvent{Err: io.ErrUnexpectedEOF})
			}
		}
	}()
	return s
}
func (s *LLMStream) emit(e ai.LLMEvent) bool {
	select {
	case s.events <- e:
		return true
	case <-s.ctx.Done():
		return false
	}
}
func (s *LLMStream) Events() <-chan ai.LLMEvent { return s.events }
func (s *LLMStream) Close() error {
	var e error
	s.once.Do(func() { s.cancel(); e = s.body.Close() })
	return e
}
