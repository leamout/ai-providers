package assemblyai

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

func TestStreamSpeechStartedOncePerTurn(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		for _, payload := range []string{
			`{"type":"Begin","id":"session-1"}`,
			`{"type":"SpeechStarted"}`,
			`{"type":"SpeechStarted"}`,
			`{"type":"Turn","turn_order":0,"transcript":""}`,
			`{"type":"Turn","turn_order":0,"transcript":"hello"}`,
			`{"type":"Turn","turn_order":0,"transcript":"hello there","end_of_turn":true}`,
			`{"type":"Turn","turn_order":0,"transcript":"Hello there.","end_of_turn":true,"turn_is_formatted":true}`,
			`{"type":"Turn","turn_order":1,"transcript":"next"}`,
			`{"type":"Turn","turn_order":0,"transcript":"Late formatted text.","end_of_turn":true,"turn_is_formatted":true}`,
			`{"type":"Turn","turn_order":1,"transcript":"Next.","end_of_turn":true,"turn_is_formatted":true}`,
			`{"type":"Termination"}`,
		} {
			if err := conn.Write(r.Context(), websocket.MessageText, []byte(payload)); err != nil {
				return
			}
		}
		_, _, _ = conn.Read(r.Context())
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ws, err := transport.Dial(ctx, nil, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := newStream(ws, ai.AudioFormat{}, true)
	go s.readLoop()
	var types []ai.STTEventType
	for event := range s.Events() {
		if event.Err != nil {
			t.Fatal(event.Err)
		}
		if event.ProviderID != "session-1" {
			t.Fatalf("unexpected provider id %q", event.ProviderID)
		}
		types = append(types, event.Type)
	}
	want := []ai.STTEventType{
		ai.STTEventSpeechStarted,
		ai.STTEventTranscriptDelta,
		ai.STTEventTranscriptDelta,
		ai.STTEventSpeechStopped,
		ai.STTEventTranscriptFinal,
		ai.STTEventSpeechStarted,
		ai.STTEventTranscriptDelta,
		ai.STTEventTranscriptFinal,
		ai.STTEventTranscriptFinal,
		ai.STTEventSpeechStopped,
	}
	if len(types) != len(want) {
		t.Fatalf("unexpected events %v", types)
	}
	for i := range want {
		if types[i] != want[i] {
			t.Fatalf("unexpected events %v", types)
		}
	}
}

func TestStreamNativeSpeechStartedBeforeTranscript(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		for _, payload := range []string{
			`{"type":"Begin","id":"session-1"}`,
			`{"type":"SpeechStarted"}`,
			`{"type":"Termination"}`,
		} {
			if err := conn.Write(r.Context(), websocket.MessageText, []byte(payload)); err != nil {
				return
			}
		}
		_, _, _ = conn.Read(r.Context())
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ws, err := transport.Dial(ctx, nil, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := newStream(ws, ai.AudioFormat{}, true)
	go s.readLoop()
	var types []ai.STTEventType
	for event := range s.Events() {
		if event.Err != nil {
			t.Fatal(event.Err)
		}
		if event.ProviderID != "session-1" {
			t.Fatalf("unexpected provider id %q", event.ProviderID)
		}
		types = append(types, event.Type)
	}
	want := []ai.STTEventType{ai.STTEventSpeechStarted}

	if len(types) != len(want) {
		t.Fatalf("unexpected events %v", types)
	}
	for i := range want {
		if types[i] != want[i] {
			t.Fatalf("unexpected events %v", types)
		}
	}
}
