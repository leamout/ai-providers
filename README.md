# Leamout AI Providers

Official AI provider adapters for the Leamout Agent Runtime.

This repository implements vendor-specific adapters behind the provider-neutral contracts in `github.com/leamout/contracts/ai`.

```text
leamout/contracts
    ↑
leamout/ai-providers
    ↑
leamout/leamout
```

`ai-providers` owns provider clients, configuration validation, credential verification, protocol translation, streaming, and normalized provider events. Agent orchestration, tenant configuration, telephony, media sessions, and provider selection remain in `github.com/leamout/leamout`.

## Provider catalog

The Agent Runtime supports two alternative AI execution models.

```text
                         Agent Runtime
                              │
                ┌─────────────┴─────────────┐
                │                           │
                ▼                           ▼
        Composable Engine            Realtime Engine
                │                           │
       ┌────────┼────────┐                  │
       ▼        ▼        ▼                  ▼
      STT      LLM      TTS              Realtime

   Deepgram    Groq    Cartesia            OpenAI
   AssemblyAI  OpenAI  ElevenLabs          Gemini
```

### Composable Engine

The Composable Engine independently selects one STT, one LLM, and one TTS provider.

```text
Audio
  ↓
STT
  ↓
Text
  ↓
LLM
  ↓
Text
  ↓
TTS
  ↓
Audio
```

For example:

```text
Deepgram → Groq → Cartesia
```

or:

```text
AssemblyAI → OpenAI → ElevenLabs
```

### Realtime Engine

The Realtime Engine uses one provider for the live bidirectional speech session.

```text
Audio
  ↓
Realtime Provider
  ↕
Live speech-to-speech session
  ↓
Audio
```

For example:

```text
OpenAI Realtime
```

or:

```text
Gemini Live
```

Realtime is an alternative to the composable STT → LLM → TTS pipeline. A Voice Agent does not need all four provider roles at the same time.

## Supported providers

| Role | Adapter | Transport | Configuration |
| --- | --- | --- | --- |
| STT | `deepgram.Provider` | WebSocket | `deepgram.Config` |
| STT | `assemblyai.Provider` | WebSocket | `assemblyai.Config` |
| LLM | `groq.Provider` | HTTP + SSE | `groq.Config` |
| LLM | `openai.Provider` | HTTP + SSE | `openai.Config` |
| TTS | `cartesia.Provider` | WebSocket | `cartesia.Config` |
| TTS | `elevenlabs.Provider` | WebSocket | `elevenlabs.Config` |
| Realtime | `openai.RealtimeProvider` | WebSocket | `openai.RealtimeConfig` |
| Realtime | `gemini.RealtimeProvider` | WebSocket | `gemini.RealtimeConfig` |

A vendor can implement more than one provider contract. OpenAI currently implements both the LLM and Realtime contracts.

## Repository layout

Packages are organized by vendor rather than provider role. Each vendor package follows the same responsibility-based layout:

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

Responsibilities are intentionally consistent across vendors:

- `config.go` — provider defaults and runtime configuration validation.
- `provider.go` — implementation of the public contracts boundary.
- `client.go` — upstream authentication, request construction, and connection setup.
- `stream.go` — stream lifecycle and normalized contract events.
- `model.go` — provider wire types.

Provider-specific behavior stays inside the vendor package. Shared code under `internal/` is limited to low-level transport mechanics that are genuinely reusable across providers.

## Runtime configuration

Tenant credentials are supplied through `ai.Runtime.Credential`. Provider-specific configuration is supplied through `ai.Runtime.Config`.

Adapters do not persist credentials or tenant configuration.

Current defaults include:

| Provider | Default / requirement |
| --- | --- |
| Deepgram | Flux `flux-general-en` |
| AssemblyAI | `universal-streaming-english` |
| Groq | `llama-3.3-70b-versatile` |
| OpenAI LLM | `gpt-4.1-mini` |
| OpenAI Realtime | `gpt-realtime-2.1` |
| Gemini Realtime | `gemini-3.8-live` |
| Cartesia | `sonic-3.6`; voice required through config or request |
| ElevenLabs | `eleven_flash_v2_5`; voice required through config or request |

## Example

Provider adapters expose the contracts directly. The Agent Runtime owns composition and lifecycle orchestration.

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

    // Forward normalized provider events into the Agent Runtime.
}
```

## Boundaries

This repository does not own:

- Agent definitions or orchestration.
- Tenant credential storage.
- Provider selection.
- Telephony or carrier integrations.
- Media session policy.
- Conversation persistence.

Those concerns remain in the main Leamout runtime.

Provider packages must not depend on `github.com/leamout/leamout`.

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
