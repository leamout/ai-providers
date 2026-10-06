package cartesia

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/leamout/contracts/ai"
)

func TestStreamFinalMarkerAndResponseCorrelation(t *testing.T) {
	requests := make(chan generationRequest, 3)
	serverErrors := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			serverErrors <- err
			return
		}
		defer conn.CloseNow()
		for range 2 {
			_, payload, err := conn.Read(r.Context())
			if err != nil {
				serverErrors <- err
				return
			}
			var request generationRequest
			if err := json.Unmarshal(payload, &request); err != nil {
				serverErrors <- err
				return
			}
			requests <- request
		}
		for _, payload := range []string{
			`{"type":"chunk","context_id":"ctx-1","data":"AAA="}`,
			`{"type":"done","context_id":"ctx-1"}`,
		} {
			if err := conn.Write(r.Context(), websocket.MessageText, []byte(payload)); err != nil {
				serverErrors <- err
				return
			}
		}
		// Read the adapter's cancellation before closing the socket.
		_, _, _ = conn.Read(r.Context())
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	streamCtx, streamCancel := context.WithCancel(ctx)
	s := newStream(streamCtx, streamCancel, conn, ai.AudioFormat{
		Encoding: ai.AudioEncodingPCM16LE, SampleRateHz: 24000, Channels: 1,
	}, "ctx-1")
	s.request = generationRequest{ContextID: "ctx-1", ModelID: "test", Voice: "voice"}
	defer s.Close()
	if err := s.SendText(ctx, ai.TextChunk{}); err == nil {
		t.Fatal("accepted empty non-final text")
	}
	if err := s.SendText(ctx, ai.TextChunk{Text: "hello"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SendText(ctx, ai.TextChunk{Final: true}); err != nil {
		t.Fatal(err)
	}
	if err := s.SendText(ctx, ai.TextChunk{Text: "late"}); err == nil {
		t.Fatal("accepted text after finalization")
	}
	for i := range 2 {
		select {
		case request := <-requests:
			if request.ContextID != "ctx-1" || request.Continue != (i == 0) {
				t.Fatalf("unexpected request %+v", request)
			}
			if i == 1 && request.Transcript != "" {
				t.Fatal("final marker unexpectedly contained text")
			}
		case err := <-serverErrors:
			t.Fatal(err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	go s.readLoop()
	var audio, done bool
	for event := range s.Events() {
		if event.Err != nil {
			t.Fatal(event.Err)
		}
		if event.ProviderID != "ctx-1" {
			t.Fatalf("missing context correlation: %+v", event)
		}
		audio = audio || len(event.Audio.Data) > 0
		done = done || event.Done
	}
	if !audio || !done {
		t.Fatalf("audio %v, done %v", audio, done)
	}
}
