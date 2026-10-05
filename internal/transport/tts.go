package transport

import (
	"context"
	"errors"
	"io"
	"sync"

	"github.com/leamout/sdk/ai"
)

// HTTP TTS preserves fragment order, issuing one synthesis request per nonempty chunk.
type TTSStream struct {
	ctx    context.Context
	cancel context.CancelFunc
	input  chan ai.TextChunk
	events chan ai.TTSEvent
	mu     sync.Mutex
	final  bool
	once   sync.Once
}

func NewTTS(parent context.Context, format ai.AudioFormat, synthesize func(context.Context, string) (io.ReadCloser, error)) ai.TTSStream {
	ctx, cancel := context.WithCancel(parent)
	s := &TTSStream{ctx: ctx, cancel: cancel, input: make(chan ai.TextChunk, 16), events: make(chan ai.TTSEvent, 32)}
	go func() {
		defer close(s.events)
		defer s.Close()
		for {
			select {
			case <-ctx.Done():
				return
			case chunk := <-s.input:
				if chunk.Text != "" {
					body, e := synthesize(ctx, chunk.Text)
					if e != nil {
						s.emit(ai.TTSEvent{Err: e})
						return
					}
					stop := context.AfterFunc(ctx, func() { body.Close() })
					e = s.readAudio(body, format)
					stop()
					body.Close()
					if e != nil {
						if ctx.Err() == nil {
							s.emit(ai.TTSEvent{Err: e})
						}
						return
					}
				}
				if chunk.Final {
					s.emit(ai.TTSEvent{Done: true})
					return
				}
			}
		}
	}()
	return s
}
func (s *TTSStream) readAudio(r io.Reader, f ai.AudioFormat) error {
	width := f.Channels
	if f.Encoding == ai.AudioEncodingPCM16LE {
		width *= 2
	}
	pending := []byte{}
	buf := make([]byte, 8192)
	for {
		n, e := r.Read(buf)
		pending = append(pending, buf[:n]...)
		size := len(pending) / width * width
		if size > 0 {
			data := append([]byte(nil), pending[:size]...)
			pending = pending[size:]
			if !s.emit(ai.TTSEvent{Audio: ai.AudioFrame{Data: data, Format: f}}) {
				return s.ctx.Err()
			}
		}
		if e != nil {
			if e == io.EOF {
				if len(pending) != 0 {
					return errors.New("upstream returned incomplete audio sample")
				}
				return nil
			}
			return e
		}
	}
}
func (s *TTSStream) SendText(ctx context.Context, c ai.TextChunk) error {
	if ctx == nil {
		return errors.New("context is required")
	}
	if c.Text == "" && !c.Final {
		return errors.New("text or final marker is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.final || s.ctx.Err() != nil {
		return errors.New("TTS stream is closed or finalized")
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-s.ctx.Done():
		return s.ctx.Err()
	case s.input <- c:
		s.final = c.Final
		return nil
	}
}
func (s *TTSStream) Events() <-chan ai.TTSEvent { return s.events }
func (s *TTSStream) Close() error               { s.once.Do(s.cancel); return nil }
func (s *TTSStream) emit(e ai.TTSEvent) bool {
	select {
	case <-s.ctx.Done():
		return false
	case s.events <- e:
		return true
	}
}
