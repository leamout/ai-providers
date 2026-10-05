# Leamout AI Providers

Official AI provider adapters for the Leamout Agent Runtime.

Provider-neutral contracts live in `github.com/leamout/contracts/ai`. This repository owns vendor-specific clients, configuration validation, credential verification, protocol translation, and normalized event mapping. Runtime orchestration, tenant configuration, telephony, and media session lifecycle remain in `github.com/leamout/leamout`.

```text
leamout/contracts
    ↑
leamout/ai-providers
    ↑
leamout/leamout
```

## Initial provider catalog

The first composable Agent Runtime intentionally starts with two providers per layer:

```text
                    Agent Runtime
                         │
          ┌──────────────┼──────────────┐
          ▼              ▼              ▼
         STT            LLM            TTS

      Deepgram         Groq          Cartesia
      AssemblyAI       OpenAI        ElevenLabs
```

This is enough to validate provider-independent composition without expanding the integration surface faster than the runtime itself.

| Role | Adapter | Transport | Configuration |
| --- | --- | --- | --- |
| STT | `deepgram.Provider` | WebSocket | `deepgram.Config` |
| STT | `assemblyai.Provider` | WebSocket | `assemblyai.Config` |
| LLM | `groq.Provider` | HTTP + SSE | `groq.Config` |
| LLM | `openai.Provider` | HTTP + SSE | `openai.Config` |
| TTS | `cartesia.Provider` | WebSocket | `cartesia.Config` |
| TTS | `elevenlabs.Provider` | WebSocket | `elevenlabs.Config` |

Additional vendors and realtime speech-to-speech providers are deferred until this initial catalog and the Agent Runtime are stable.

## Repository layout

Packages are organized by vendor rather than role. Each package follows the same responsibility-oriented structure:

```text
<vendor>/
├── config.go
├── provider.go
├── client.go
├── stream.go        # when streaming exists
├── model.go         # when protocol types justify it
├── *_test.go
└── ...
```

`config.go` owns provider defaults and runtime configuration validation. `provider.go` is the contracts adapter boundary. `client.go` owns upstream authentication, request construction, and connection setup. `stream.go` owns live stream lifecycle and normalized contract events. `model.go` contains vendor wire types when they justify a separate file.

Shared code under `internal/` is intentionally limited to low-level transport mechanics. Provider-specific lifecycle and protocol behavior belongs in the vendor package.

## Runtime configuration

Tenant credentials are supplied through `ai.Runtime.Credential` and provider-specific options through `ai.Runtime.Config`. Adapters do not persist credentials.

Current defaults:

| Provider | Default / requirement |
| --- | --- |
| Deepgram | Flux `flux-general-en` |
| AssemblyAI | `universal-streaming-english` |
| Groq | `llama-3.3-70b-versatile` |
| OpenAI | `gpt-4.1-mini` |
| Cartesia | `sonic-3.6`; voice required through config or request |
| ElevenLabs | `eleven_flash_v2_5`; voice required through config or request |

## Composition

The runtime selects STT, LLM, and TTS independently. For example, a call can use AssemblyAI for transcription, OpenAI for reasoning and tool calls, and Cartesia for synthesis without any provider-specific orchestration logic in the Agent Runtime.

```go
llm := openai.Provider{HTTPClient: httpClient}
stream, err := llm.Generate(ctx, ai.LLMRequest{
    Runtime: ai.Runtime{
        Credential: openAIKey,
        Config:     json.RawMessage(`{"model":"gpt-4.1-mini"}`),
    },
    Messages: []ai.Message{
        {Role: ai.RoleUser, Content: "Say hello."},
    },
})
if err != nil {
    return err
}
defer stream.Close()

for event := range stream.Events() {
    if event.Err != nil {
        return event.Err
    }
    // Forward event.TextDelta to the runtime's response pipeline.
}
```

## Boundaries

This repository does not own provider selection, agent definitions, tenant credential storage, orchestration, telephony, carrier integrations, or media session policy. Those concerns stay in the main Leamout runtime.

The dependency direction is:

```text
leamout/leamout
    ↓
leamout/ai-providers
    ↓
leamout/contracts
```

Provider packages must not depend on the Leamout runtime.

## Development

The module targets Go 1.26.6.

```bash
go test ./...
go test -race ./...
go vet ./...
```

Provider tests use local HTTP/WebSocket fixtures and do not require live vendor credentials.

## License

Apache License 2.0.
