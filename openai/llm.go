// Package openai implements OpenAI language-model and speech adapters.
package openai

import "github.com/leamout/ai-providers/internal/chat"

// LLM implements streaming Chat Completions, including tools and usage.
type LLM = chat.Provider

// LLMConfig configures Chat Completions.
type LLMConfig = chat.Config
