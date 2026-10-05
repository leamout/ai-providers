package integration_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/leamout/ai-providers/anthropic"
	"github.com/leamout/ai-providers/assemblyai"
	"github.com/leamout/ai-providers/azure"
	"github.com/leamout/ai-providers/cartesia"
	"github.com/leamout/ai-providers/elevenlabs"
	"github.com/leamout/ai-providers/gemini"
	"github.com/leamout/ai-providers/google"
	"github.com/leamout/ai-providers/openai"
	"github.com/leamout/sdk/ai"
)

func raw(v any) json.RawMessage {
	b, e := json.Marshal(v)
	if e != nil {
		panic(e)
	}
	return b
}
func deadline(t *testing.T) context.Context {
	t.Helper()
	ctx, c := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(c)
	return ctx
}
func TestCatalogAndInvalidConfiguration(t *testing.T) {
	providers := []ai.Provider{openai.LLM{}, openai.TTS{}, anthropic.Provider{}, gemini.Provider{}, cartesia.Provider{}, elevenlabs.Provider{}, assemblyai.Provider{}, google.STT{}, azure.STT{}, azure.TTS{}}
	seen := map[string]bool{}
	for _, p := range providers {
		d := p.Descriptor()
		if e := d.Validate(); e != nil {
			t.Fatal(e)
		}
		key := d.ID + ":" + string(d.Kind)
		if seen[key] {
			t.Fatalf("duplicate %s", key)
		}
		seen[key] = true
		validator := p.(ai.ConfigValidator)
		if e := validator.ValidateConfig(json.RawMessage(`{`)); e == nil {
			t.Fatalf("%s accepts invalid JSON", key)
		}
	}
	for _, p := range []ai.ConfigValidator{openai.LLM{}, openai.TTS{}, anthropic.Provider{}, gemini.Provider{}, cartesia.Provider{}, elevenlabs.Provider{}, assemblyai.Provider{}, azure.STT{}, azure.TTS{}} {
		if e := p.ValidateConfig(raw(map[string]any{"endpoint": "http://insecure.test"})); e == nil {
			t.Fatalf("%T accepts insecure endpoint", p)
		}
	}
}
func TestLLMNativeProtocols(t *testing.T) {
	cases := []struct {
		name               string
		make               func(*http.Client) ai.LLM
		path, auth, header string
		body               string
		config             map[string]any
		check              func(*testing.T, map[string]any)
	}{
		{"openai", func(c *http.Client) ai.LLM { return openai.LLM{HTTPClient: c} }, "/chat", "Bearer key", "Authorization", "data: {\"id\":\"r1\",\"choices\":[{\"delta\":{\"content\":\"hello\",\"tool_calls\":[{\"index\":0,\"id\":\"call\",\"function\":{\"name\":\"lookup\",\"arguments\":\"{}\"}}]}}]}\n\ndata: {\"id\":\"r1\",\"usage\":{\"prompt_tokens\":2,\"completion_tokens\":3,\"total_tokens\":5}}\n\ndata: [DONE]\n\n", map[string]any{"model": "test-model"}, func(t *testing.T, p map[string]any) {
			m := p["messages"].([]any)
			if len(m) != 2 || m[0].(map[string]any)["role"] != "system" {
				t.Fatal(p)
			}
			if p["stream_options"].(map[string]any)["include_usage"] != true {
				t.Fatal(p)
			}
		}},
		{"anthropic", func(c *http.Client) ai.LLM { return anthropic.Provider{HTTPClient: c} }, "/messages", "key", "X-Api-Key", "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"r1\",\"usage\":{\"input_tokens\":2}}}\n\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"hello\"}}\n\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"tool_use\",\"id\":\"call\",\"name\":\"lookup\"}}\n\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{}\"}}\n\ndata: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":3}}\n\ndata: {\"type\":\"message_stop\"}\n\n", nil, func(t *testing.T, p map[string]any) {
			if p["system"] != "instructions" || p["max_tokens"].(float64) <= 0 {
				t.Fatal(p)
			}
			if _, ok := p["tools"].([]any)[0].(map[string]any)["input_schema"]; !ok {
				t.Fatal(p)
			}
		}},
		{"gemini", func(c *http.Client) ai.LLM { return gemini.Provider{HTTPClient: c} }, "/models/test-model:streamGenerateContent", "key", "X-Goog-Api-Key", "data: {\"responseId\":\"r1\",\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"hello\"},{\"functionCall\":{\"name\":\"lookup\",\"args\":{}}}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":2,\"candidatesTokenCount\":3,\"totalTokenCount\":5}}\n\n", map[string]any{"model": "test-model"}, func(t *testing.T, p map[string]any) {
			if p["contents"].([]any)[0].(map[string]any)["role"] != "user" {
				t.Fatal(p)
			}
			if p["generationConfig"].(map[string]any)["thinkingConfig"].(map[string]any)["thinkingBudget"] != float64(0) {
				t.Fatal(p)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" || r.URL.Path != tc.path || r.Header.Get(tc.header) != tc.auth {
					t.Errorf("unexpected request %s %s, auth=%q", r.Method, r.URL.Path, r.Header.Get(tc.header))
				}
				var p map[string]any
				if e := json.NewDecoder(r.Body).Decode(&p); e != nil {
					t.Error(e)
				}
				tc.check(t, p)
				w.Header().Set("Content-Type", "text/event-stream")
				io.WriteString(w, tc.body)
			}))
			defer server.Close()
			config := map[string]any{"endpoint": server.URL + tc.path}
			if tc.name == "gemini" {
				config["endpoint"] = server.URL
			}
			for k, v := range tc.config {
				config[k] = v
			}
			s, e := tc.make(server.Client()).Generate(deadline(t), ai.LLMRequest{Runtime: ai.Runtime{Credential: "key", Config: raw(config)}, Instructions: "instructions", Messages: []ai.Message{{Role: ai.RoleUser, Content: "hi"}}, Tools: []ai.ToolDefinition{{Name: "lookup", Parameters: raw(map[string]any{"type": "object"})}}})
			if e != nil {
				t.Fatal(e)
			}
			defer s.Close()
			text, args, name := "", "", ""
			total := 0
			done := false
			for event := range s.Events() {
				if event.Err != nil {
					t.Fatal(event.Err)
				}
				text += event.TextDelta
				args += string(event.ToolArguments)
				if event.ToolName != "" {
					name = event.ToolName
				}
				if event.TotalTokens > 0 {
					total = event.TotalTokens
				}
				done = done || event.Done
			}
			if text != "hello" || args != "{}" || name != "lookup" || total != 5 || !done {
				t.Fatalf("text=%q args=%q name=%q total=%d done=%v", text, args, name, total, done)
			}
		})
	}
}
func TestLLMFailuresAndCancellation(t *testing.T) {
	for _, mode := range []string{"http", "malformed", "truncated", "upstream", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			started := make(chan struct{})
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if mode == "http" {
					w.WriteHeader(401)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(200)
				w.(http.Flusher).Flush()
				close(started)
				switch mode {
				case "malformed":
					io.WriteString(w, "data: {invalid}\n\n")
				case "truncated":
					io.WriteString(w, "data: {\"choices\":[]}\n\n")
				case "upstream":
					io.WriteString(w, "data: {\"error\":{\"message\":\"failed\"}}\n\n")
				case "cancel":
					<-r.Context().Done()
				}
			}))
			defer server.Close()
			ctx, cancel := context.WithCancel(deadline(t))
			defer cancel()
			s, e := (openai.LLM{HTTPClient: server.Client()}).Generate(ctx, ai.LLMRequest{Runtime: ai.Runtime{Credential: "key", Config: raw(map[string]any{"endpoint": server.URL})}, Messages: []ai.Message{{Role: ai.RoleUser, Content: "hi"}}})
			if mode == "http" {
				if e == nil {
					t.Fatal("401 accepted")
				}
				return
			}
			if e != nil {
				t.Fatal(e)
			}
			defer s.Close()
			if mode == "cancel" {
				<-started
				cancel()
				select {
				case <-s.Events():
				case <-time.After(time.Second):
					t.Fatal("cancellation did not unblock read")
				}
				return
			}
			failed := false
			for event := range s.Events() {
				failed = failed || event.Err != nil
			}
			if !failed {
				t.Fatal("failure was not emitted")
			}
		})
	}
}
func TestHTTPTextToSpeech(t *testing.T) {
	for _, vendor := range []string{"openai", "azure"} {
		t.Run(vendor, func(t *testing.T) {
			texts := []string{}
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if vendor == "openai" {
					if r.Header.Get("Authorization") != "Bearer key" {
						t.Error("missing auth")
					}
					var p map[string]any
					json.NewDecoder(r.Body).Decode(&p)
					if p["response_format"] != "pcm" {
						t.Error(p)
					}
					texts = append(texts, p["input"].(string))
				} else {
					if r.Header.Get("Ocp-Apim-Subscription-Key") != "key" || r.Header.Get("X-Microsoft-OutputFormat") != "raw-24khz-16bit-mono-pcm" {
						t.Error("headers")
					}
					b, _ := io.ReadAll(r.Body)
					if !strings.Contains(string(b), "&lt;") {
						t.Error("SSML not escaped")
					}
					texts = append(texts, string(b))
				}
				w.Write([]byte{1})
				w.(http.Flusher).Flush()
				w.Write([]byte{2, 3, 4})
			}))
			defer server.Close()
			var p ai.TTS = openai.TTS{HTTPClient: server.Client()}
			if vendor == "azure" {
				p = azure.TTS{HTTPClient: server.Client()}
			}
			s, e := p.StartTTS(deadline(t), ai.TTSRequest{Runtime: ai.Runtime{Credential: "key", Config: raw(map[string]any{"endpoint": server.URL})}, Format: ai.AudioFormat{Encoding: ai.AudioEncodingPCM16LE, Channels: 1, SampleRateHz: 24000}})
			if e != nil {
				t.Fatal(e)
			}
			defer s.Close()
			if e := s.SendText(deadline(t), ai.TextChunk{Text: "first <"}); e != nil {
				t.Fatal(e)
			}
			if e := s.SendText(deadline(t), ai.TextChunk{Text: "second <", Final: true}); e != nil {
				t.Fatal(e)
			}
			if e := s.SendText(deadline(t), ai.TextChunk{Text: "late"}); e == nil {
				t.Fatal("accepted text after final")
			}
			audio := []byte{}
			done := false
			for v := range s.Events() {
				if v.Err != nil {
					t.Fatal(v.Err)
				}
				if len(v.Audio.Data) > 0 {
					if e := v.Audio.Validate(); e != nil {
						t.Fatal(e)
					}
					audio = append(audio, v.Audio.Data...)
				}
				done = done || v.Done
			}
			if len(texts) != 2 || len(audio) != 8 || !done {
				t.Fatalf("texts %v audio %v done %v", texts, audio, done)
			}
		})
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestCredentialVerification(t *testing.T) {
	cases := []struct {
		name, host, header, value string
		make                      func(*http.Client) ai.CredentialVerifier
	}{
		{"openai", "api.openai.com", "Authorization", "Bearer key", func(c *http.Client) ai.CredentialVerifier { return openai.LLM{HTTPClient: c} }},
		{"anthropic", "api.anthropic.com", "X-Api-Key", "key", func(c *http.Client) ai.CredentialVerifier { return anthropic.Provider{HTTPClient: c} }},
		{"gemini", "generativelanguage.googleapis.com", "X-Goog-Api-Key", "key", func(c *http.Client) ai.CredentialVerifier { return gemini.Provider{HTTPClient: c} }},
		{"assemblyai", "api.assemblyai.com", "Authorization", "key", func(c *http.Client) ai.CredentialVerifier { return assemblyai.Provider{HTTPClient: c} }},
		{"cartesia", "api.cartesia.ai", "X-Api-Key", "key", func(c *http.Client) ai.CredentialVerifier { return cartesia.Provider{HTTPClient: c} }},
		{"elevenlabs", "api.elevenlabs.io", "Xi-Api-Key", "key", func(c *http.Client) ai.CredentialVerifier { return elevenlabs.Provider{HTTPClient: c} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status := 200
			called := false
			c := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				called = true
				if r.Method != http.MethodGet || r.URL.Scheme != "https" || r.URL.Host != tc.host || r.Header.Get(tc.header) != tc.value {
					t.Errorf("unexpected verification request: %s %s", r.Method, r.URL)
				}
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("{}")), Header: make(http.Header)}, nil
			})}
			p := tc.make(c)
			if e := p.VerifyCredential(deadline(t), ""); e == nil || called {
				t.Fatal("empty credential caused network request or passed")
			}
			if e := p.VerifyCredential(deadline(t), "key"); e != nil {
				t.Fatal(e)
			}
			status = 401
			if e := p.VerifyCredential(deadline(t), "key"); e == nil {
				t.Fatal("invalid credential accepted")
			}
		})
	}
}
func TestTTSAudioConstraints(t *testing.T) {
	bad := ai.AudioFormat{Encoding: ai.AudioEncodingPCM16LE, SampleRateHz: 12345, Channels: 2}
	for _, p := range []ai.TTS{openai.TTS{}, azure.TTS{}, cartesia.Provider{}, elevenlabs.Provider{}} {
		_, e := p.StartTTS(deadline(t), ai.TTSRequest{Runtime: ai.Runtime{Credential: "key", Config: raw(map[string]any{"region": "eastus", "voice_id": "voice"})}, Format: bad})
		if e == nil {
			t.Fatalf("%T accepted invalid format", p)
		}
	}
}
func TestNativeToolHistory(t *testing.T) {
	for _, vendor := range []string{"openai", "anthropic", "gemini"} {
		t.Run(vendor, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var p map[string]any
				if e := json.NewDecoder(r.Body).Decode(&p); e != nil {
					t.Error(e)
					return
				}
				switch vendor {
				case "openai":
					m := p["messages"].([]any)
					call := m[1].(map[string]any)["tool_calls"].([]any)[0].(map[string]any)
					if call["id"] != "call-id" || m[2].(map[string]any)["tool_call_id"] != "call-id" {
						t.Error(p)
					}
					io.WriteString(w, "data: [DONE]\n\n")
				case "anthropic":
					m := p["messages"].([]any)
					call := m[1].(map[string]any)["content"].([]any)[0].(map[string]any)
					result := m[2].(map[string]any)["content"].([]any)[0].(map[string]any)
					if call["type"] != "tool_use" || call["input"].(map[string]any)["id"] != float64(1) || result["tool_use_id"] != "call-id" || m[2].(map[string]any)["role"] != "user" {
						t.Error(p)
					}
					io.WriteString(w, "data: {\"type\":\"message_stop\"}\n\n")
				case "gemini":
					m := p["contents"].([]any)
					call := m[1].(map[string]any)["parts"].([]any)[0].(map[string]any)["functionCall"].(map[string]any)
					result := m[2].(map[string]any)["parts"].([]any)[0].(map[string]any)["functionResponse"].(map[string]any)
					if call["name"] != "lookup" || result["name"] != "lookup" || result["response"].(map[string]any)["value"] != "found" {
						t.Error(p)
					}
					io.WriteString(w, "data: {\"candidates\":[{\"finishReason\":\"STOP\"}]}\n\n")
				}
			}))
			defer server.Close()
			var p ai.LLM = openai.LLM{HTTPClient: server.Client()}
			if vendor == "anthropic" {
				p = anthropic.Provider{HTTPClient: server.Client()}
			} else if vendor == "gemini" {
				p = gemini.Provider{HTTPClient: server.Client()}
			}
			s, e := p.Generate(deadline(t), ai.LLMRequest{Runtime: ai.Runtime{Credential: "key", Config: raw(map[string]any{"endpoint": server.URL})}, Messages: []ai.Message{{Role: ai.RoleUser, Content: "lookup"}, {Role: ai.RoleAssistant, ToolCalls: []ai.ToolCall{{ID: "call-id", Name: "lookup", Arguments: raw(map[string]any{"id": 1})}}}, {Role: ai.RoleTool, ToolCallID: "call-id", Content: `{"value":"found"}`}}})
			if e != nil {
				t.Fatal(e)
			}
			defer s.Close()
			done := false
			for v := range s.Events() {
				if v.Err != nil {
					t.Fatal(v.Err)
				}
				done = done || v.Done
			}
			if !done {
				t.Fatal("missing done")
			}
		})
	}
}
func TestHTTPAudioFailure(t *testing.T) {
	for _, mode := range []string{"status", "odd-sample", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if mode == "status" {
					w.WriteHeader(429)
					return
				}
				w.Write([]byte{1})
				w.(http.Flusher).Flush()
				if mode == "cancel" {
					<-r.Context().Done()
				}
			}))
			defer server.Close()
			ctx, cancel := context.WithCancel(deadline(t))
			defer cancel()
			s, e := (openai.TTS{HTTPClient: server.Client()}).StartTTS(ctx, ai.TTSRequest{Runtime: ai.Runtime{Credential: "key", Config: raw(map[string]any{"endpoint": server.URL})}, Format: ai.AudioFormat{Encoding: ai.AudioEncodingPCM16LE, SampleRateHz: 24000, Channels: 1}})
			if e != nil {
				t.Fatal(e)
			}
			defer s.Close()
			if e := s.SendText(ctx, ai.TextChunk{Text: "hi", Final: true}); e != nil {
				t.Fatal(e)
			}
			if mode == "cancel" {
				cancel()
				select {
				case <-s.Events():
				case <-time.After(time.Second):
					t.Fatal("blocked HTTP TTS after cancel")
				}
				return
			}
			failed := false
			for v := range s.Events() {
				failed = failed || v.Err != nil
				if v.Done {
					t.Fatal("failed synthesis reported done")
				}
			}
			if !failed {
				t.Fatal("failure ignored")
			}
		})
	}
}
