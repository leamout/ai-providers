package deepgram

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/coder/websocket"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/leamout/contracts/ai"
)

func TestBuildEndpoint(t *testing.T) {
	threshold := 0.8
	cfg := Config{
		Model:        "flux-general-en",
		EOTThreshold: &threshold,
		EOTTimeoutMS: 7000,
	}
	endpoint, err := buildEndpoint(cfg, ai.AudioFormat{
		Encoding:     ai.AudioEncodingMuLaw,
		SampleRateHz: 8000,
		Channels:     1,
	})
	if err != nil {
		t.Fatalf("buildEndpoint() error = %v", err)
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}
	query := parsed.Query()
	if got := query.Get("model"); got != "flux-general-en" {
		t.Fatalf("model = %q", got)
	}
	if got := query.Get("encoding"); got != "mulaw" {
		t.Fatalf("encoding = %q", got)
	}
	if got := query.Get("sample_rate"); got != "8000" {
		t.Fatalf("sample_rate = %q", got)
	}
	if got := query.Get("eot_threshold"); got != "0.8" {
		t.Fatalf("eot_threshold = %q", got)
	}
	if got := query.Get("eot_timeout_ms"); got != "7000" {
		t.Fatalf("eot_timeout_ms = %q", got)
	}
}

func TestValidateAudioFormatRejectsStereo(t *testing.T) {
	err := validateAudioFormat(ai.AudioFormat{
		Encoding:     ai.AudioEncodingPCM16LE,
		SampleRateHz: 16000,
		Channels:     2,
	})
	if err == nil {
		t.Fatal("validateAudioFormat() error = nil, want error")
	}
}

func TestFluxInterimAndResumeMapping(t *testing.T) {
	for _, name := range []string{"Update", "EagerEndOfTurn", "TurnResumed"} {
		t.Run(name, func(t *testing.T) {
			events := mapTurnMessage(turnMessage{Event: name, Transcript: "whole turn", RequestID: "req"})
			if name == "TurnResumed" {
				if len(events) != 2 || events[0].Type != ai.STTEventSpeechStarted {
					t.Fatalf("missing interruption signal: %+v", events)
				}
				events = events[1:]
			}
			if len(events) != 1 || events[0].Type != ai.STTEventTranscriptDelta || events[0].Text != "whole turn" {
				t.Fatalf("unexpected snapshot: %+v", events)
			}
		})
	}
}

func TestFluxCloseDrainsTranscript(t *testing.T) {
	for _, finalized := range []bool{false, true} {
		t.Run(fmt.Sprintf("finalized=%v", finalized), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := websocket.Accept(w, r, nil)
				if err != nil {
					return
				}
				defer conn.CloseNow()
				_, payload, err := conn.Read(r.Context())
				if err != nil {
					return
				}
				var command map[string]string
				if json.Unmarshal(payload, &command) != nil || command["type"] != "CloseStream" {
					return
				}
				event := "Update"
				if finalized {
					event = "EndOfTurn"
				}
				payload, _ = json.Marshal(turnMessage{Type: "TurnInfo", Event: event, Transcript: "last words", RequestID: "req"})
				_ = conn.Write(r.Context(), websocket.MessageText, payload)
				// Flux closes without a WebSocket close frame after draining.
			}))
			defer server.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
			if err != nil {
				t.Fatal(err)
			}
			streamCtx, streamCancel := context.WithCancel(ctx)
			defer streamCancel()
			s := newStream(streamCtx, streamCancel, conn, ai.AudioFormat{})
			go s.readLoop()
			closed := make(chan error, 1)
			go func() { closed <- s.Close(ctx) }()
			var finals, stops int
			for event := range s.Events() {
				if event.Err != nil {
					t.Fatal(event.Err)
				}
				if event.Type == ai.STTEventTranscriptFinal {
					finals++
					if event.Text != "last words" {
						t.Fatalf("lost final text: %+v", event)
					}
				}
				if event.Type == ai.STTEventSpeechStopped {
					stops++
				}
			}
			if err := <-closed; err != nil {
				t.Fatal(err)
			}
			if finals != 1 || stops != 1 {
				t.Fatalf("finals %d, stops %d", finals, stops)
			}
			if err := s.Close(ctx); err != nil {
				t.Fatalf("repeat close: %v", err)
			}
		})
	}
}
