package gemini

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
			t.Errorf("read Gemini setup: %v", err)
			return
		}

		// Setup responses exceeding the library's 32 KiB default must work.
		setup := `{"setupComplete":{},"padding":"` +
			strings.Repeat("a", 64<<10) + `"}`
		if err := connection.Write(r.Context(), websocket.MessageText, []byte(setup)); err != nil {
			t.Errorf("write Gemini setup response: %v", err)
			return
		}

		oversized := `{"usageMetadata":{"promptTokenCount":1},"padding":"` +
			strings.Repeat("x", int(transport.MaxWebSocketMessageBytes)) + `"}`
		_ = connection.Write(r.Context(), websocket.MessageText, []byte(oversized))
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := newClient(nil).start(ctx, RealtimeConfig{
		Endpoint: "ws" + strings.TrimPrefix(server.URL, "http"),
		Model:    "test-live",
	}, realtimeReadLimitRequest())
	if err != nil {
		t.Fatalf("start realtime stream: %v", err)
	}
	defer func() { _ = result.Close(context.Background()) }()

	select {
	case event, ok := <-result.Events():
		if !ok || event.Type != ai.RealtimeEventError ||
			event.Failure == nil || !event.Failure.Terminal ||
			!strings.Contains(event.Failure.Message, "read limited at") {
			t.Fatalf("expected terminal message-size failure, got %#v (open=%t)", event, ok)
		}
	case <-ctx.Done():
		t.Fatalf("waiting for oversized realtime event rejection: %v", ctx.Err())
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

func TestRealtimeWebSocketReadLimitDuringSetup(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connection, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("accept realtime connection: %v", err)
			return
		}
		defer func() { _ = connection.CloseNow() }()

		if _, _, err := connection.Read(r.Context()); err != nil {
			t.Errorf("read Gemini setup: %v", err)
			return
		}

		oversized := `{"setupComplete":{},"padding":"` +
			strings.Repeat("x", int(transport.MaxWebSocketMessageBytes)) + `"}`
		_ = connection.Write(r.Context(), websocket.MessageText, []byte(oversized))
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := newClient(nil).start(ctx, RealtimeConfig{
		Endpoint: "ws" + strings.TrimPrefix(server.URL, "http"),
		Model:    "test-live",
	}, realtimeReadLimitRequest())
	if err == nil {
		_ = result.Close(context.Background())
		t.Fatal("expected oversized Gemini setup to fail")
	}
	if !strings.Contains(err.Error(), "read limited at") {
		t.Fatalf("expected message-size error during setup, got %v", err)
	}
}

func realtimeReadLimitRequest() ai.RealtimeRequest {
	input := ai.AudioFormat{
		Encoding:     ai.AudioEncodingPCM16LE,
		SampleRateHz: 16000,
		Channels:     1,
	}
	output := ai.AudioFormat{
		Encoding:     ai.AudioEncodingPCM16LE,
		SampleRateHz: 24000,
		Channels:     1,
	}
	request := ai.RealtimeRequest{
		InputFormat:  input,
		OutputFormat: output,
	}
	request.Runtime.Credential = "test-key"
	return request
}
