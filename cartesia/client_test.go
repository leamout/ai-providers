package cartesia

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/coder/websocket"
	"github.com/leamout/contracts/ai"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestBufferDelayOnWireAndErrorCorrelation(t *testing.T) {
	for _, delay := range []int{0, 3000, 5000} {
		t.Run(strconv.Itoa(delay), func(t *testing.T) {
			requests := make(chan generationRequest, 2)
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := websocket.Accept(w, r, nil)
				if err != nil {
					return
				}
				defer conn.CloseNow()
				for range 2 {
					_, payload, err := conn.Read(r.Context())
					if err != nil {
						return
					}
					var request generationRequest
					if json.Unmarshal(payload, &request) != nil {
						return
					}
					requests <- request
				}
				_ = conn.Write(r.Context(), websocket.MessageText, []byte(`{"type":"error","context_id":"ctx-error","request_id":"req-123","error_code":"concurrency_limited","status_code":429,"message":"too many requests"}`))
				_, _, _ = conn.Read(r.Context())
			}))
			defer server.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			raw, _ := json.Marshal(map[string]any{"endpoint": "wss" + strings.TrimPrefix(server.URL, "https"), "voice_id": "voice", "max_buffer_delay_ms": delay})
			cfg, err := decodeConfig(raw)
			if err != nil {
				t.Fatal(err)
			}
			s, err := newClient(server.Client()).start(ctx, "secret", cfg, ai.AudioFormat{Encoding: ai.AudioEncodingPCM16LE, SampleRateHz: 24000, Channels: 1})
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			if err := s.SendText(ctx, ai.TextChunk{Text: "Hello "}); err != nil {
				t.Fatal(err)
			}
			if err := s.SendText(ctx, ai.TextChunk{Final: true}); err != nil {
				t.Fatal(err)
			}
			for i := range 2 {
				select {
				case request := <-requests:
					if request.MaxBufferDelayMS == nil || *request.MaxBufferDelayMS != delay {
						t.Fatalf("delay not preserved: %+v", request)
					}
					if request.Continue != (i == 0) {
						t.Fatalf("wrong continuation flag: %+v", request)
					}
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
			}
			var received bool
			for event := range s.Events() {
				if event.Done {
					t.Fatal("error reported success")
				}
				var upstream *Error
				if !errors.As(event.Err, &upstream) {
					t.Fatalf("expected structured error: %v", event.Err)
				}
				if event.ProviderID != "ctx-error" || upstream.RequestID != "req-123" || upstream.ContextID != "ctx-error" || upstream.Code != "concurrency_limited" || upstream.StatusCode != 429 {
					t.Fatalf("lost diagnostics: %+v, %+v", event, upstream)
				}
				received = true
			}
			if !received {
				t.Fatal("expected upstream error event")
			}
		})
	}
}
