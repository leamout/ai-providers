# Leamout AI Providers

Official STT, LLM, TTS, and realtime AI provider integrations for Leamout.

## Architecture

This repository contains vendor adapters only. Provider-neutral contracts live in `github.com/leamout/sdk/ai`, while runtime orchestration and tenant configuration live in the main Leamout repository.

```text
leamout/sdk
    ↑
leamout/ai-providers
    ↑
leamout/leamout
```

The Agent Runtime composes providers independently:

```text
                    Agent Runtime
                         │
          ┌──────────────┼──────────────┐
          ▼              ▼              ▼
         STT            LLM            TTS

      Deepgram         Groq          Cartesia
      AssemblyAI       OpenAI        ElevenLabs
      Google           Anthropic     OpenAI
      Azure            Gemini        Azure
```

Integrated speech-to-speech providers are exposed separately through the realtime contract.

## Provider adapters

The adapters below implement `github.com/leamout/sdk/ai`. Tests exercise their
wire protocols against local HTTP, WebSocket, and gRPC servers. Live vendor
accounts have not been tested; validate credentials, model availability, and
account permissions before deploying.

| Role | Adapter | Transport | Configuration type |
| --- | --- | --- | --- |
| STT | `deepgram.Provider` | Deepgram Flux WebSocket | `deepgram.Config` |
| STT | `assemblyai.Provider` | Universal Streaming v3 WebSocket | `assemblyai.Config` |
| STT | `google.STT` | Cloud Speech v1 bidirectional gRPC | `google.Config` |
| STT | `azure.STT` | Azure Speech conversation WebSocket | `azure.STTConfig` |
| LLM | `groq.Provider` | Chat Completions SSE | `groq.Config` |
| LLM | `openai.LLM` | Chat Completions SSE | `openai.LLMConfig` |
| LLM | `anthropic.Provider` | Messages SSE | `anthropic.Config` |
| LLM | `gemini.Provider` | generateContent SSE | `gemini.Config` |
| TTS | `cartesia.Provider` | Cartesia WebSocket | `cartesia.Config` |
| TTS | `elevenlabs.Provider` | stream-input WebSocket | `elevenlabs.Config` |
| TTS | `openai.TTS` | Audio Speech HTTP response streaming | `openai.TTSConfig` |
| TTS | `azure.TTS` | SSML HTTP response streaming | `azure.TTSConfig` |

Packages are organized by vendor. Multi-role vendors expose separate types
(`openai.LLM`, `openai.TTS`, `azure.STT`, and `azure.TTS`). Descriptor IDs identify
the vendor; use `(ID, Kind)` as the catalog key. Realtime speech-to-speech is a
separate SDK contract and is not implemented in this repository yet.

### Configuration and authentication

Pass tenant credentials in `ai.Runtime.Credential` and JSON-encoded options in
`ai.Runtime.Config`. Adapters do not persist credentials. Models can be selected
in configuration; availability depends on the vendor account.

| Adapter | Defaults and required options |
| --- | --- |
| Deepgram | Flux `flux-general-en`; multilingual hints require `flux-general-multi`. |
| AssemblyAI | `universal-streaming-english`; use `speech_model` to select another streaming model. Language is selected by the model, rather than `STTRequest.Language`. |
| Google STT | `en-US`; optional `model`, `project_id` for billing/quota, and gRPC `endpoint` (default `speech.googleapis.com:443`). Credential is an OAuth access token with Cloud Speech permissions. |
| Azure STT | Required `region` (for example `eastus`) or full `wss` `endpoint`; default language `en-US`. Credential is a Speech subscription key. |
| Groq | `llama-3.3-70b-versatile`; API key. |
| OpenAI LLM | `gpt-4.1-mini`; API key. |
| Anthropic | `claude-sonnet-4-5`, `max_tokens: 4096`; API key. |
| Gemini | `gemini-2.5-flash`; API key. Thinking is disabled because the SDK cannot round-trip thought signatures through tool history. Use a model that supports `thinkingBudget: 0`. |
| Cartesia | `sonic-3.6`, API version `2026-08-14`; required `voice_id` or request `Voice`; API key. |
| ElevenLabs | `eleven_flash_v2_5`; required `voice_id` or request `Voice`; API key. |
| OpenAI TTS | `gpt-4o-mini-tts`, voice `alloy`, speed `1`; API key. |
| Azure TTS | Required `region` or full HTTPS `endpoint`; default voice `en-US-JennyNeural`, language `en-US`; Speech subscription key. |

All HTTP adapters accept an optional `HTTPClient` for connection reuse and
custom transports. Endpoints must use HTTPS or WSS. Google accepts an optional
`speechpb.SpeechClient` to reuse an authenticated gRPC client with application
default credentials or automatic token refresh; caller-supplied clients remain
owned by the caller. Otherwise it opens its own TLS connection using the supplied
OAuth token. API keys and service-account JSON are not OAuth access tokens.
Refresh tokens before starting new sessions; adapters do not manage token refresh.

Credential verification is available through `ai.CredentialVerifier` for
Deepgram, AssemblyAI, Groq, OpenAI, Anthropic, Gemini, Cartesia, and ElevenLabs.
It checks authentication, not access to every model or voice. Azure and Google
check authentication when opening/using a stream, since it depends on region,
project, or the injected client.

### Streaming lifecycle and audio

Supply a cancellable context for every session. Consume the event channel while
sending input, inspect `Err`, and close the stream when finished. Backpressure is
bounded; unread events can block generation. HTTP TTS sends requests in a worker,
so upstream synthesis errors arrive on `Events`, after `SendText` accepts a chunk.

For TTS, send ordered `ai.TextChunk` values and mark the last chunk `Final: true`.
An empty final chunk is supported. Text after finalization is rejected. Cartesia
and ElevenLabs send fragments over one WebSocket synthesis session. OpenAI and
Azure issue one HTTP synthesis request per nonempty fragment and preserve the
order of returned audio; choose sentence-sized fragments to avoid excessive
requests and audible prosody changes. `Done` marks successful completion;
`Close` cancels pending work.

| Adapter | Supported audio |
| --- | --- |
| Deepgram STT | Mono PCM16LE, mulaw, or alaw; 8, 16, 24, 44.1, or 48 kHz. |
| AssemblyAI STT | Mono PCM16LE or mulaw, 8–48 kHz; each frame must contain 50–1000 ms. |
| Google STT | Mono PCM16LE or mulaw, 8–48 kHz; frames limited to 25 KiB. |
| Azure STT | Mono PCM16LE at 8 or 16 kHz. |
| Cartesia TTS | Mono PCM16LE at 8, 16, 22.05, 24, 44.1, or 48 kHz. |
| ElevenLabs TTS | Mono PCM16LE at 8, 16, 22.05, 24, 44.1, or 48 kHz; mulaw/alaw at 8 kHz. Some formats require paid plans. |
| OpenAI TTS | Mono PCM16LE at 24 kHz. |
| Azure TTS | Mono PCM16LE at 8, 16, 24, or 48 kHz; mulaw/alaw at 8 kHz. |

There is no implicit resampling, transcoding, or channel mixing. TTS audio is raw,
without a WAV header. Deepgram and AssemblyAI `Finalize` force a turn boundary
without ending the session. Google and Azure `Finalize` end audio input and allow
remaining recognition results to drain; start a new session for more audio.
Google streams must be rotated by the runtime before the vendor's session limit.
Azure renews its recognition request when the service ends a turn while audio
input is still open. STT interim transcript events contain the vendor's current
hypothesis and can replace earlier text; they are not necessarily append-only.

All LLM adapters normalize text, tool calls, and usage. Accumulate tool argument
fragments by `ToolIndex`, retaining the call ID and name from earlier events.
Gemini returns complete function arguments and supplies session-local call IDs;
its tool results must reference preceding assistant tool calls in message history.

### Example: independent LLM and TTS selection

```go
llm := openai.LLM{HTTPClient: httpClient}
stream, err := llm.Generate(ctx, ai.LLMRequest{
    Runtime: ai.Runtime{
        Credential: openAIKey,
        Config: json.RawMessage(`{"model":"gpt-4.1-mini"}`),
    },
    Messages: []ai.Message{{Role: ai.RoleUser, Content: "Say hello."}},
})
if err != nil {
    return err
}
defer stream.Close()
for event := range stream.Events() {
    if event.Err != nil {
        return event.Err
    }
    // Forward event.TextDelta to your runtime's sentence buffer.
}

// TTS can use a different vendor; the runtime supplies its credential and voice.
tts := cartesia.Provider{HTTPClient: httpClient}
speech, err := tts.StartTTS(ctx, ai.TTSRequest{
    Runtime: ai.Runtime{Credential: cartesiaKey},
    Voice: cartesiaVoiceID,
    Format: ai.AudioFormat{
        Encoding: ai.AudioEncodingPCM16LE,
        SampleRateHz: 24000,
        Channels: 1,
    },
})
if err != nil {
    return err
}
defer speech.Close()
if err := speech.SendText(ctx, ai.TextChunk{Text: "Hello!", Final: true}); err != nil {
    return err
}
for event := range speech.Events() {
    if event.Err != nil {
        return event.Err
    }
    // Send event.Audio to your runtime's media output.
}
```

## Boundaries

This repository owns provider-specific clients, configuration validation, credential verification, protocol translation, and normalized event mapping. It does not own tenant credential storage, voice-agent bindings, orchestration, telephony, media session lifecycle, or carrier integrations.

## Development

The module targets Go 1.26.6.

```bash
go test ./...
go test -race ./...
go vet ./...
```

Tests require no vendor credentials. The `integration` suite uses local TLS HTTP,
WebSocket, and in-memory gRPC servers to verify payloads, authentication, streaming
events, tool calls, audio framing, errors, finalization, and cancellation.

## License

Apache License 2.0.
