package cartesia

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/leamout/sdk/ai"
)

type outputFormat struct {
	Container  string `json:"container"`
	Encoding   string `json:"encoding"`
	SampleRate int    `json:"sample_rate"`
}

type generationRequest struct {
	ModelID      string       `json:"model_id"`
	Transcript   string       `json:"transcript"`
	Voice        string       `json:"voice"`
	Language     string       `json:"language,omitempty"`
	ContextID    string       `json:"context_id"`
	OutputFormat outputFormat `json:"output_format"`
	Continue     bool         `json:"continue"`
}

type response struct {
	Type       string `json:"type"`
	Data       string `json:"data"`
	StatusCode int    `json:"status_code"`
	RequestID  string `json:"request_id"`
	Message    string `json:"message"`
	ErrorCode  string `json:"error_code"`
}

type stream struct {
	ctx        context.Context
	cancel     context.CancelFunc
	connection *websocket.Conn
	format     ai.AudioFormat
	contextID  string
	request    generationRequest
	events     chan ai.TTSEvent
	writeMu    sync.Mutex
	sendMu     sync.Mutex
	final      bool
	closeOnce  sync.Once
}

func newStream(ctx context.Context, cancel context.CancelFunc, connection *websocket.Conn, format ai.AudioFormat, contextID string) *stream {
	return &stream{ctx: ctx, cancel: cancel, connection: connection, format: format, contextID: contextID, events: make(chan ai.TTSEvent, 32)}
}

func (s *stream) SendText(ctx context.Context, chunk ai.TextChunk) error {
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	if s.final {
		return fmt.Errorf("cartesia text already finalized")
	}
	if chunk.Text == "" && !chunk.Final {
		return fmt.Errorf("cartesia transcript is required")
	}
	request := s.request
	request.Transcript = chunk.Text
	request.Continue = !chunk.Final
	if err := s.writeJSON(ctx, request); err != nil {
		return err
	}
	s.final = chunk.Final
	return nil
}

func (s *stream) Events() <-chan ai.TTSEvent { return s.events }

func (s *stream) Close() error {
	var err error
	s.closeOnce.Do(func() {

		s.cancel()
		err = s.connection.CloseNow()
	})
	return err
}

func (s *stream) writeJSON(ctx context.Context, value any) error {
	if ctx == nil {
		return fmt.Errorf("cartesia context is required")
	}
	if err := s.ctx.Err(); err != nil {
		return err
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
	stop := context.AfterFunc(s.ctx, func() { _ = s.connection.CloseNow() })
	defer stop()
	defer close(s.events)
	defer func() { _ = s.Close() }()
	for {
		kind, payload, err := s.connection.Read(s.ctx)
		if err != nil {
			if s.ctx.Err() == nil && websocket.CloseStatus(err) != websocket.StatusNormalClosure {
				s.emit(ai.TTSEvent{Err: fmt.Errorf("read Cartesia stream: %w", err)})
			}
			return
		}
		if kind != websocket.MessageText {
			s.emit(ai.TTSEvent{Err: fmt.Errorf("cartesia returned a non-text event")})
			return
		}
		var msg response
		if err := json.Unmarshal(payload, &msg); err != nil {
			s.emit(ai.TTSEvent{Err: fmt.Errorf("decode Cartesia event: %w", err)})
			return
		}
		if msg.Type == "error" || msg.StatusCode >= 400 {
			s.emit(ai.TTSEvent{ProviderID: msg.RequestID, Err: fmt.Errorf("cartesia %s: %s", msg.ErrorCode, msg.Message)})
			return
		}
		switch msg.Type {
		case "chunk":
			audio, err := base64.StdEncoding.DecodeString(msg.Data)
			if err != nil {
				s.emit(ai.TTSEvent{Err: fmt.Errorf("decode Cartesia audio: %w", err)})
				return
			}
			frame := ai.AudioFrame{Data: audio, Format: s.format, CapturedAt: time.Now().UTC()}
			if err := frame.Validate(); err != nil {
				s.emit(ai.TTSEvent{Err: err})
				return
			}
			s.emit(ai.TTSEvent{Audio: frame, ProviderID: msg.RequestID})
		case "done":
			s.emit(ai.TTSEvent{ProviderID: msg.RequestID, Done: true})
			return
		}
	}
}

func (s *stream) emit(event ai.TTSEvent) {
	select {
	case s.events <- event:
	case <-s.ctx.Done():
	}
}

var _ ai.TTSStream = (*stream)(nil)
