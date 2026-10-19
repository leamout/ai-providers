package conformance_test

import (
	"encoding/json"
	"testing"

	"github.com/leamout/ai-providers/assemblyai"
	"github.com/leamout/ai-providers/cartesia"
	"github.com/leamout/ai-providers/deepgram"
	"github.com/leamout/ai-providers/elevenlabs"
	"github.com/leamout/ai-providers/gemini"
	"github.com/leamout/ai-providers/groq"
	"github.com/leamout/ai-providers/openai"
	"github.com/leamout/contracts/ai"
)

func TestOfficialAdaptersExposeValidUniqueDescriptors(t *testing.T) {
	t.Parallel()

	providers := []ai.Provider{
		deepgram.Provider{},
		assemblyai.Provider{},
		groq.Provider{},
		openai.Provider{},
		openai.RealtimeProvider{},
		cartesia.Provider{},
		elevenlabs.Provider{},
		gemini.RealtimeProvider{},
	}

	seen := make(map[string]struct{}, len(providers))
	for _, provider := range providers {
		descriptor := provider.Descriptor()
		if err := descriptor.Validate(); err != nil {
			t.Fatalf("descriptor %q is invalid: %v", descriptor.ID, err)
		}
		key := string(descriptor.Kind) + ":" + descriptor.ID
		if _, exists := seen[key]; exists {
			t.Fatalf("duplicate provider descriptor %q", key)
		}
		seen[key] = struct{}{}
	}
}

func TestOfficialAdaptersRejectUnapprovedRuntimeEndpoints(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		provider   ai.ConfigValidator
		authorized ai.ConfigValidator
		endpoint   string
	}{
		{
			name:       "assemblyai",
			provider:   assemblyai.Provider{},
			authorized: assemblyai.Provider{AllowedEndpoints: []string{"wss://custom.example.test/ws"}},
			endpoint:   "wss://custom.example.test/ws",
		},
		{
			name:       "deepgram",
			provider:   deepgram.Provider{},
			authorized: deepgram.Provider{AllowedEndpoints: []string{"wss://custom.example.test/ws"}},
			endpoint:   "wss://custom.example.test/ws",
		},
		{
			name:       "groq",
			provider:   groq.Provider{},
			authorized: groq.Provider{AllowedEndpoints: []string{"https://custom.example.test/v1/chat"}},
			endpoint:   "https://custom.example.test/v1/chat",
		},
		{
			name:       "openai-llm",
			provider:   openai.Provider{},
			authorized: openai.Provider{AllowedEndpoints: []string{"https://custom.example.test/v1/chat"}},
			endpoint:   "https://custom.example.test/v1/chat",
		},
		{
			name:       "cartesia",
			provider:   cartesia.Provider{},
			authorized: cartesia.Provider{AllowedEndpoints: []string{"wss://custom.example.test/ws"}},
			endpoint:   "wss://custom.example.test/ws",
		},
		{
			name:       "elevenlabs",
			provider:   elevenlabs.Provider{},
			authorized: elevenlabs.Provider{AllowedEndpoints: []string{"wss://custom.example.test/ws"}},
			endpoint:   "wss://custom.example.test/ws",
		},
		{
			name:       "openai-realtime",
			provider:   openai.RealtimeProvider{},
			authorized: openai.RealtimeProvider{AllowedEndpoints: []string{"wss://custom.example.test/ws"}},
			endpoint:   "wss://custom.example.test/ws",
		},
		{
			name:       "gemini-realtime",
			provider:   gemini.RealtimeProvider{},
			authorized: gemini.RealtimeProvider{AllowedEndpoints: []string{"wss://custom.example.test/ws"}},
			endpoint:   "wss://custom.example.test/ws",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if err := test.provider.ValidateConfig(nil); err != nil {
				t.Fatalf("default config rejected: %v", err)
			}

			config, err := json.Marshal(map[string]any{
				"endpoint":          test.endpoint,
				"allowed_endpoints": []string{test.endpoint},
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := test.provider.ValidateConfig(config); err == nil {
				t.Fatal("tenant config authorized an untrusted endpoint")
			}
			if err := test.authorized.ValidateConfig(config); err != nil {
				t.Fatalf("explicitly authorized endpoint rejected: %v", err)
			}
		})
	}
}
