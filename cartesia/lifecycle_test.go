package cartesia

import (
	"context"
	"github.com/coder/websocket"
	"github.com/leamout/contracts/ai"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCloseUnblocksStalledSend(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		<-release
	}))
	defer server.Close()
	defer close(release)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	s := newStream(ctx, cancel, conn, ai.AudioFormat{}, "ctx")
	sent := make(chan error, 1)
	go func() { sent <- s.SendText(ctx, ai.TextChunk{Text: strings.Repeat("a", 32<<20)}) }()
	deadline := time.Now().Add(time.Second)
	for s.writeMu.TryLock() {
		s.writeMu.Unlock()
		if time.Now().After(deadline) {
			t.Fatal("send did not acquire write lock")
		}
		time.Sleep(time.Millisecond)
	}
	closed := make(chan error, 1)
	go func() { closed <- s.Close() }()
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		cancel()
		<-closed
		t.Fatal("Close did not unblock the stalled writer")
	}
	select {
	case err := <-sent:
		if err == nil {
			t.Fatal("expected interrupted write to fail")
		}
	case <-time.After(time.Second):
		t.Fatal("writer did not exit after Close")
	}
	select {
	case <-ctx.Done():
	default:
		t.Fatal("Close did not cancel the stream")
	}
}
