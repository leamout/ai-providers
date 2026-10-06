package deepgram

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/leamout/contracts/ai"
)

func TestReadLoopClosesConnectionOnInvalidEvent(t *testing.T) {
	for _, test := range []struct {
		name    string
		kind    websocket.MessageType
		payload string
	}{
		{name: "invalid JSON", kind: websocket.MessageText, payload: "{"},
		{name: "binary event", kind: websocket.MessageBinary, payload: "unexpected"},
	} {
		t.Run(test.name, func(t *testing.T) {
			closed := make(chan error, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := websocket.Accept(w, r, nil)
				if err != nil {
					return
				}
				defer conn.CloseNow()
				if err := conn.Write(r.Context(), test.kind, []byte(test.payload)); err != nil {
					return
				}
				_, _, err = conn.Read(r.Context())
				closed <- err
			}))
			defer server.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.CloseNow()
			streamCtx, streamCancel := context.WithCancel(ctx)
			defer streamCancel()
			s := newStream(streamCtx, streamCancel, conn, ai.AudioFormat{})
			go s.readLoop()
			var gotErr error
			for event := range s.Events() {
				gotErr = event.Err
			}
			if gotErr == nil {
				t.Fatal("expected invalid-event error")
			}
			select {
			case err := <-closed:
				if err == nil {
					t.Fatal("expected peer connection to close")
				}
			case <-ctx.Done():
				t.Fatal("reader exited without releasing its connection")
			}
			select {
			case <-streamCtx.Done():
			case <-ctx.Done():
				t.Fatal("reader exited without cancelling stream context")
			}
		})
	}
}
