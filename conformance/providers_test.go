package conformance_test

import (
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
