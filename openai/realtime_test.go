package openai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/leamout/ai-providers/internal/transport"
	"github.com/leamout/contracts/ai"
)

func TestRealtimeWebSocketReadLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connection, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("accept realtime connection: %v", err)
			return
		}
		defer func() { _ = connection.CloseNow() }()

		if _, _, err := connection.Read(r.Context()); err != nil {
			t.Errorf("read session update: %v", err)
			return
		}

		// Larger than the WebSocket library's 32 KiB default, but allowed.
		allowed := `{"type":"response.created","padding":"` + strings.Repeat("a", 64<<10) + `"}`
		if err := connection.Write(r.Context(), websocket.MessageText, []byte(allowed)); err != nil {
			t.Errorf("write allowed realtime event: %v", err)
			return
		}

		// A single event above the adapter's 4 MiB limit must be rejected.
		oversized := `{"type":"response.created","padding":"` +
			strings.Repeat("x", int(transport.MaxWebSocketMessageBytes)) + `"}`
		_ = connection.Write(r.Context(), websocket.MessageText, []byte(oversized))
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	format := ai.AudioFormat{
		Encoding:     ai.AudioEncodingPCM16LE,
		SampleRateHz: 24000,
		Channels:     1,
	}
	request := ai.RealtimeRequest{
		InputFormat:  format,
		OutputFormat: format,
	}
	request.Runtime.Credential = "test-key"

	result, err := newClient(nil).start(ctx, RealtimeConfig{
		Endpoint: "ws" + strings.TrimPrefix(server.URL, "http"),
		Model:    "test-realtime",
	}, request)
	if err != nil {
		t.Fatalf("start realtime stream: %v", err)
	}
	defer func() { _ = result.Close(context.Background()) }()

	select {
	case event, ok := <-result.Events():
		if !ok || event.Type != ai.RealtimeEventResponseStarted {
			t.Fatalf("expected allowed response-start event, got %#v (open=%t)", event, ok)
		}
	case <-ctx.Done():
		t.Fatalf("waiting for allowed realtime event: %v", ctx.Err())
	}

	select {
	case event, ok := <-result.Events():
		if !ok || event.Type != ai.RealtimeEventError ||
			event.Failure == nil || !event.Failure.Terminal ||
			!strings.Contains(event.Failure.Message, "read limited at") {
			t.Fatalf("expected terminal message-size failure, got %#v (open=%t)", event, ok)
		}
	case <-ctx.Done():
		t.Fatalf("waiting for oversized message rejection: %v", ctx.Err())
	}

	select {
	case _, ok := <-result.Events():
		if ok {
			t.Fatal("expected events channel to close after message-size violation")
		}
	case <-ctx.Done():
		t.Fatalf("waiting for event channel to close: %v", ctx.Err())
	}
}
