package catalog

import (
	"github.com/leamout/ai-providers/assemblyai"
	"github.com/leamout/ai-providers/cartesia"
	"github.com/leamout/ai-providers/deepgram"
	"github.com/leamout/ai-providers/elevenlabs"
	"github.com/leamout/ai-providers/gemini"
	"github.com/leamout/ai-providers/groq"
	"github.com/leamout/ai-providers/openai"
	"github.com/leamout/contracts/ai"
)

// Providers returns the official provider adapters shipped by Leamout.
//
// The returned slice is newly allocated so callers may append or reorder it
// without mutating package state.
func Providers() []ai.Provider {
	return []ai.Provider{
		deepgram.Provider{},
		assemblyai.Provider{},
		groq.Provider{},
		openai.Provider{},
		openai.RealtimeProvider{},
		cartesia.Provider{},
		elevenlabs.Provider{},
		gemini.RealtimeProvider{},
	}
}

// Descriptors returns the portable descriptors for every official adapter.
func Descriptors() []ai.Descriptor {
	providers := Providers()
	descriptors := make([]ai.Descriptor, 0, len(providers))
	for _, provider := range providers {
		descriptor := provider.Descriptor()
		descriptor.Capabilities = append([]ai.Capability(nil), descriptor.Capabilities...)
		descriptors = append(descriptors, descriptor)
	}
	return descriptors
}
