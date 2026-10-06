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

## Provider catalog

The Agent Runtime supports two alternative AI execution models:

- **Composable Engine** — independently selects STT, LLM, and TTS providers.
- **Realtime Engine** — uses one realtime speech-to-speech provider for the live conversation.

A Voice Agent uses one engine path for a session. Realtime is an alternative to the composable STT → LLM → TTS pipeline; it is not a fourth provider that must be configured alongside the other three.

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

The initial catalog keeps the composable surface deliberately small while making realtime a first-class provider kind. Additional vendors can be added behind the same contracts without changing Agent Runtime orchestration.

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

A vendor package can implement more than one contract. For example, `openai.Provider` implements the LLM contract while `openai.RealtimeProvider` implements the realtime speech-to-speech contract.

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
| OpenAI LLM | `gpt-4.1-mini` |
| OpenAI Realtime | `gpt-realtime-2.1`; `wss://api.openai.com/v1/realtime` |
| Gemini Realtime | Gemini Live native-audio model; `wss://generativelanguage.googleapis.com/ws/google.ai.generativelanguage.v1beta.GenerativeService.BidiGenerateContent` |
| Cartesia | `sonic-3.6`; voice required through config or request |
| ElevenLabs | `eleven_flash_v2_5`; voice required through config or request |

## Execution models

### Composable Engine

The runtime selects STT, LLM, and TTS independently. For example, a call can use AssemblyAI for transcription, OpenAI for reasoning and tool calls, and Cartesia for synthesis without any provider-specific orchestration logic in the Agent Runtime.

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

AssemblyAI maps native `SpeechStarted` events when the upstream model provides them. For models without that signal, it emits `speech.started` on the first non-empty transcript of each turn; this fallback depends on transcription latency. Speech stop is emitted once per completed turn, independently of transcript formatting.

### Realtime Engine

The realtime path uses one provider to maintain a continuous bidirectional speech session. The provider receives streaming audio, emits streaming audio, and can surface normalized speech, transcript, response, usage, error, and tool-call events through the shared realtime contract.

```text
Audio
  ↓
Realtime Provider
  ↕
Live speech-to-speech session
  ↓
Audio
```

OpenAI Realtime and Gemini Live implement the same vendor-neutral realtime contract, so the Agent Runtime can select either provider without changing its engine boundary.

## Cartesia buffering and interruption

Cartesia defaults to an explicit `max_buffer_delay_ms` of 3000. Set it to `0` when the runtime already aggregates text into phrases; use a positive value when streaming tokens directly. The adapter accepts 0–5000 ms and preserves an explicit zero on every context input, including the final marker.

```json
{"model":"sonic-3.6","voice_id":"your-voice-id","max_buffer_delay_ms":0}
```

Cartesia concatenates transcripts verbatim, so the runtime must preserve token whitespace. Audio, completion, and upstream error events use the context ID for `ProviderID`. Use `errors.As` with `*cartesia.Error` to inspect the upstream request ID, error code, and status code.

On caller interruption, the runtime must immediately stop playback and discard queued audio from the interrupted response. Cartesia cancellation stops pending generation, but active generation may continue. Closing the synthesis stream releases the connection; it does not clear audio already queued in the runtime.

## Deepgram Flux transcript and shutdown semantics

Deepgram `transcript.delta` events contain the complete current turn transcript, not append-only text fragments. Replace the displayed interim transcript on each update and use `transcript.final` as the committed turn text. `Update` and `EagerEndOfTurn` are interim snapshots; an eager event never commits a turn.

`TurnResumed` maps to `speech.started` followed by the latest interim transcript. The runtime must use this interruption signal to cancel any speculative response and stop playback. Consumers must tolerate another speech-start signal within the same unfinished turn.

Keep consuming events while calling `Close(ctx)`: Flux drains remaining audio into updates before closing. On an expected server disconnect after `CloseStream`, the adapter commits the last unfinished transcript and emits speech stop. A turn already finalized by `EndOfTurn` is not committed again. `Finalize(ctx)` sends `ForceEndTurn` and leaves the stream open; it is separate from closing the stream.

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
