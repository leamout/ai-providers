package integration_test

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/leamout/ai-providers/assemblyai"
	"github.com/leamout/ai-providers/azure"
	"github.com/leamout/ai-providers/cartesia"
	"github.com/leamout/ai-providers/elevenlabs"
	"github.com/leamout/sdk/ai"
)

func writeJSON(ctx context.Context, c *websocket.Conn, v any) error {
	return c.Write(ctx, websocket.MessageText, raw(v))
}
func readJSON(ctx context.Context, c *websocket.Conn) (map[string]any, error) {
	_, b, e := c.Read(ctx)
	if e != nil {
		return nil, e
	}
	var v map[string]any
	e = json.Unmarshal(b, &v)
	return v, e
}
func TestWebSocketTTS(t *testing.T) {
	for _, vendor := range []string{"cartesia", "elevenlabs"} {
		t.Run(vendor, func(t *testing.T) {
			ctx := deadline(t)
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				header := "X-Api-Key"
				if vendor == "elevenlabs" {
					header = "Xi-Api-Key"
					if !strings.HasSuffix(r.URL.Path, "/voice/stream-input") || r.URL.Query().Get("output_format") != "pcm_24000" {
						t.Error(r.URL)
					}
				} else if r.URL.Query().Get("cartesia_version") == "" {
					t.Error("missing version")
				}
				if r.Header.Get(header) != "key" {
					t.Error("auth")
				}
				c, e := websocket.Accept(w, r, nil)
				if e != nil {
					t.Error(e)
					return
				}
				defer c.CloseNow()
				if vendor == "elevenlabs" {
					v, e := readJSON(ctx, c)
					if e != nil || v["text"] != " " {
						t.Errorf("initialization: %v %v", v, e)
						return
					}
				}
				for i := 0; i < 2; i++ {
					v, e := readJSON(ctx, c)
					if e != nil {
						t.Error(e)
						return
					}
					if vendor == "cartesia" {
						if v["transcript"] != "hello" || v["continue"] != (i == 0) {
							t.Error(v)
						}
						if v["output_format"].(map[string]any)["encoding"] != "pcm_s16le" {
							t.Error(v)
						}
					} else if v["text"] != "hello " {
						t.Error(v)
					}
				}
				if vendor == "elevenlabs" {
					v, e := readJSON(ctx, c)
					if e != nil || v["text"] != "" {
						t.Errorf("final: %v %v", v, e)
						return
					}
				}
				audio := base64.StdEncoding.EncodeToString([]byte{1, 2, 3, 4})
				if vendor == "cartesia" {
					writeJSON(ctx, c, map[string]any{"type": "chunk", "data": audio, "request_id": "id"})
					writeJSON(ctx, c, map[string]any{"type": "done", "request_id": "id"})
				} else {
					writeJSON(ctx, c, map[string]any{"audio": audio, "is_final": true})
				}
				_, _, _ = c.Read(ctx)
			}))
			defer server.Close()
			var p ai.TTS = cartesia.Provider{HTTPClient: server.Client()}
			if vendor == "elevenlabs" {
				p = elevenlabs.Provider{HTTPClient: server.Client()}
			}
			s, e := p.StartTTS(ctx, ai.TTSRequest{Runtime: ai.Runtime{Credential: "key", Config: raw(map[string]any{"endpoint": strings.Replace(server.URL, "https://", "wss://", 1), "voice_id": "voice"})}, Format: ai.AudioFormat{Encoding: ai.AudioEncodingPCM16LE, SampleRateHz: 24000, Channels: 1}})
			if e != nil {
				t.Fatal(e)
			}
			defer s.Close()
			if e := s.SendText(ctx, ai.TextChunk{Text: "hello"}); e != nil {
				t.Fatal(e)
			}
			if e := s.SendText(ctx, ai.TextChunk{Text: "hello", Final: true}); e != nil {
				t.Fatal(e)
			}
			done := false
			n := 0
			for e := range s.Events() {
				if e.Err != nil {
					t.Fatal(e.Err)
				}
				n += len(e.Audio.Data)
				done = done || e.Done
			}
			if !done || n != 4 {
				t.Fatalf("done %v audio %d", done, n)
			}
		})
	}
}
func TestAssemblyAIProtocol(t *testing.T) {
	ctx := deadline(t)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "key" || r.URL.Query().Get("encoding") != "pcm_s16le" || r.URL.Query().Get("sample_rate") != "16000" {
			t.Error(r.URL)
		}
		c, e := websocket.Accept(w, r, nil)
		if e != nil {
			t.Error(e)
			return
		}
		defer c.CloseNow()
		writeJSON(ctx, c, map[string]any{"type": "Begin", "id": "session"})
		kind, b, e := c.Read(ctx)
		if e != nil || kind != websocket.MessageBinary || len(b) != 1600 {
			t.Errorf("audio %d %d %v", kind, len(b), e)
			return
		}
		v, e := readJSON(ctx, c)
		if e != nil || v["type"] != "ForceEndpoint" {
			t.Errorf("finalize %v %v", v, e)
			return
		}
		writeJSON(ctx, c, map[string]any{"type": "Turn", "transcript": "hello", "end_of_turn": false})
		writeJSON(ctx, c, map[string]any{"type": "Turn", "transcript": "hello world", "end_of_turn": true})
		v, e = readJSON(ctx, c)
		if e != nil || v["type"] != "Terminate" {
			t.Errorf("close %v %v", v, e)
			return
		}
		writeJSON(ctx, c, map[string]any{"type": "Termination"})
	}))
	defer server.Close()
	f := ai.AudioFormat{Encoding: ai.AudioEncodingPCM16LE, SampleRateHz: 16000, Channels: 1}
	s, e := (assemblyai.Provider{HTTPClient: server.Client()}).StartSTT(ctx, ai.STTRequest{Runtime: ai.Runtime{Credential: "key", Config: raw(map[string]any{"endpoint": strings.Replace(server.URL, "https://", "wss://", 1)})}, Format: f})
	if e != nil {
		t.Fatal(e)
	}
	if e := s.SendAudio(ctx, ai.AudioFrame{Format: f, Data: make([]byte, 1600)}); e != nil {
		t.Fatal(e)
	}
	if e := s.Finalize(ctx); e != nil {
		t.Fatal(e)
	}
	types := []ai.STTEventType{ai.STTEventTranscriptDelta, ai.STTEventTranscriptFinal, ai.STTEventSpeechStopped}
	for _, want := range types {
		select {
		case v := <-s.Events():
			if v.Err != nil || v.Type != want || v.ProviderID != "session" {
				t.Fatalf("event %+v", v)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	if e := s.Close(ctx); e != nil {
		t.Fatal(e)
	}
}
func TestAzureSpeechFraming(t *testing.T) {
	ctx := deadline(t)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Ocp-Apim-Subscription-Key") != "key" || r.URL.Query().Get("language") != "en-US" {
			t.Error("auth/language")
		}
		c, e := websocket.Accept(w, r, nil)
		if e != nil {
			t.Error(e)
			return
		}
		defer c.CloseNow()
		for _, path := range []string{"speech.config", "speech.context"} {
			k, b, e := c.Read(ctx)
			if e != nil || k != websocket.MessageText || !strings.Contains(string(b), "Path: "+path) {
				t.Errorf("%s: %s %v", path, b, e)
				return
			}
		}
		for i := 0; i < 3; i++ {
			k, b, e := c.Read(ctx)
			if e != nil || k != websocket.MessageBinary || len(b) < 2 {
				t.Errorf("audio %v", e)
				return
			}
			size := int(binary.BigEndian.Uint16(b))
			if size > len(b)-2 {
				t.Error("header length")
				return
			}
			h, data := string(b[2:2+size]), b[2+size:]
			if !strings.Contains(h, "Path: audio") || !strings.Contains(h, "Content-Type: audio/x-wav") {
				t.Error(h)
			}
			switch i {
			case 0:
				if len(data) != 44 || string(data[:4]) != "RIFF" || binary.LittleEndian.Uint32(data[24:]) != 16000 {
					t.Error("wave header")
				}
			case 1:
				if len(data) != 640 {
					t.Error("audio data")
				}
			case 2:
				if len(data) != 0 {
					t.Error("audio EOF")
				}
			}
		}
		for _, v := range []struct{ path, body string }{{"speech.startDetected", "{}"}, {"speech.hypothesis", `{"Text":"hello"}`}, {"speech.phrase", `{"RecognitionStatus":"Success","DisplayText":"hello world"}`}, {"speech.endDetected", "{}"}, {"turn.end", "{}"}} {
			e = c.Write(ctx, websocket.MessageText, []byte("Path: "+v.path+"\r\nX-RequestId: id\r\n\r\n"+v.body))
			if e != nil {
				t.Error(e)
				return
			}
		}
	}))
	defer server.Close()
	f := ai.AudioFormat{Encoding: ai.AudioEncodingPCM16LE, SampleRateHz: 16000, Channels: 1}
	s, e := (azure.STT{HTTPClient: server.Client()}).StartSTT(ctx, ai.STTRequest{Runtime: ai.Runtime{Credential: "key", Config: raw(map[string]any{"endpoint": strings.Replace(server.URL, "https://", "wss://", 1)})}, Format: f})
	if e != nil {
		t.Fatal(e)
	}
	if e := s.SendAudio(ctx, ai.AudioFrame{Format: f, Data: make([]byte, 640)}); e != nil {
		t.Fatal(e)
	}
	if e := s.Finalize(ctx); e != nil {
		t.Fatal(e)
	}
	events := []ai.STTEvent{}
	for v := range s.Events() {
		if v.Err != nil {
			t.Fatal(v.Err)
		}
		events = append(events, v)
	}
	if len(events) != 4 || events[1].Text != "hello" || events[2].Type != ai.STTEventTranscriptFinal {
		t.Fatal(events)
	}
	s.Close(ctx)
}
func TestTTSUpstreamError(t *testing.T) {
	for _, vendor := range []string{"cartesia", "elevenlabs"} {
		t.Run(vendor, func(t *testing.T) {
			ctx := deadline(t)
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				c, e := websocket.Accept(w, r, nil)
				if e != nil {
					t.Error(e)
					return
				}
				defer c.CloseNow()
				if vendor == "elevenlabs" {
					if _, e := readJSON(ctx, c); e != nil {
						t.Error(e)
						return
					}
				}
				if vendor == "cartesia" {
					writeJSON(ctx, c, map[string]any{"type": "error", "message": "quota"})
				} else {
					writeJSON(ctx, c, map[string]any{"error": "quota", "message": "exhausted"})
				}
				_, _, _ = c.Read(ctx)
			}))
			defer server.Close()
			var p ai.TTS = cartesia.Provider{HTTPClient: server.Client()}
			if vendor == "elevenlabs" {
				p = elevenlabs.Provider{HTTPClient: server.Client()}
			}
			s, e := p.StartTTS(ctx, ai.TTSRequest{Runtime: ai.Runtime{Credential: "key", Config: raw(map[string]any{"endpoint": strings.Replace(server.URL, "https://", "wss://", 1), "voice_id": "v"})}, Format: ai.AudioFormat{Encoding: ai.AudioEncodingPCM16LE, SampleRateHz: 24000, Channels: 1}})
			if e != nil {
				t.Fatal(e)
			}
			defer s.Close()
			select {
			case v := <-s.Events():
				if v.Err == nil {
					t.Fatal("upstream error ignored")
				}
			case <-time.After(time.Second):
				t.Fatal("no error")
			}
		})
	}
}
func TestWebSocketCancellation(t *testing.T) {
	for _, vendor := range []string{"cartesia", "elevenlabs", "assemblyai", "azure"} {
		t.Run(vendor, func(t *testing.T) {
			ctx, cancel := context.WithCancel(deadline(t))
			defer cancel()
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				c, e := websocket.Accept(w, r, nil)
				if e != nil {
					t.Error(e)
					return
				}
				defer c.CloseNow()
				for {
					if _, _, e := c.Read(ctx); e != nil {
						return
					}
				}
			}))
			defer server.Close()
			runtime := ai.Runtime{Credential: "key", Config: raw(map[string]any{"endpoint": strings.Replace(server.URL, "https://", "wss://", 1), "voice_id": "voice"})}
			f := ai.AudioFormat{Encoding: ai.AudioEncodingPCM16LE, SampleRateHz: 16000, Channels: 1}
			var done chan struct{} = make(chan struct{})
			if vendor == "cartesia" || vendor == "elevenlabs" {
				var p ai.TTS = cartesia.Provider{HTTPClient: server.Client()}
				if vendor == "elevenlabs" {
					p = elevenlabs.Provider{HTTPClient: server.Client()}
				}
				s, e := p.StartTTS(ctx, ai.TTSRequest{Runtime: runtime, Format: f})
				if e != nil {
					t.Fatal(e)
				}
				defer s.Close()
				go func() {
					for range s.Events() {
					}
					close(done)
				}()
			} else {
				var p ai.STT = assemblyai.Provider{HTTPClient: server.Client()}
				if vendor == "azure" {
					p = azure.STT{HTTPClient: server.Client()}
				}
				s, e := p.StartSTT(ctx, ai.STTRequest{Runtime: runtime, Format: f})
				if e != nil {
					t.Fatal(e)
				}
				go func() {
					for range s.Events() {
					}
					close(done)
				}()
			}
			cancel()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("cancellation left read blocked")
			}
		})
	}
}
