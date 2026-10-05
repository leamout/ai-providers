package transport

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"

	"github.com/coder/websocket"
)

type WS struct {
	Conn   *websocket.Conn
	Ctx    context.Context
	cancel context.CancelFunc
	Mu     sync.Mutex
	once   sync.Once
	stop   func() bool
}

func Dial(parent context.Context, c *http.Client, endpoint string, h http.Header) (*WS, error) {
	if parent == nil {
		return nil, errors.New("context is required")
	}
	ctx, cancel := context.WithCancel(parent)
	conn, resp, e := websocket.Dial(ctx, endpoint, &websocket.DialOptions{HTTPClient: Client(c), HTTPHeader: h, CompressionMode: websocket.CompressionDisabled})
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}
	if e != nil {
		cancel()
		return nil, e
	}
	conn.SetReadLimit(4 << 20)
	s := &WS{Conn: conn, Ctx: ctx, cancel: cancel}
	s.stop = context.AfterFunc(ctx, func() { conn.CloseNow() })
	return s, nil
}
func (s *WS) JSON(ctx context.Context, v any) error {
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	return s.Write(ctx, websocket.MessageText, b)
}
func (s *WS) Write(ctx context.Context, kind websocket.MessageType, b []byte) error {
	if ctx == nil {
		return errors.New("context is required")
	}
	s.Mu.Lock()
	defer s.Mu.Unlock()
	if s.Ctx.Err() != nil {
		return s.Ctx.Err()
	}
	return s.Conn.Write(ctx, kind, b)
}
func (s *WS) Read(v any) error {
	k, b, e := s.Conn.Read(s.Ctx)
	if e != nil {
		return e
	}
	if k != websocket.MessageText {
		return errors.New("expected a text websocket event")
	}
	return json.Unmarshal(b, v)
}
func (s *WS) Close() error {
	var e error
	s.once.Do(func() { s.cancel(); s.stop(); e = s.Conn.CloseNow() })
	return e
}
