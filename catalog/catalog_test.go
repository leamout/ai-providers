package catalog

import (
	"testing"

	"github.com/leamout/contracts/ai"
)

func TestProvidersExposeUniqueKindAndIDPairs(t *testing.T) {
	t.Parallel()

	seen := make(map[string]struct{})
	for _, provider := range Providers() {
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

	for _, key := range []string{
		string(ai.KindSTT) + ":deepgram",
		string(ai.KindSTT) + ":assemblyai",
		string(ai.KindLLM) + ":groq",
		string(ai.KindLLM) + ":openai",
		string(ai.KindTTS) + ":cartesia",
		string(ai.KindTTS) + ":elevenlabs",
		string(ai.KindRealtime) + ":openai",
		string(ai.KindRealtime) + ":gemini",
	} {
		if _, ok := seen[key]; !ok {
			t.Fatalf("official provider catalog missing %q", key)
		}
	}
}
