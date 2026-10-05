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

## Repository layout

Provider packages are organized by vendor rather than role because one vendor may implement multiple roles.

```text
ai-providers/
├── deepgram/
├── assemblyai/
├── google/
├── azure/
├── groq/
├── openai/
├── anthropic/
├── gemini/
├── cartesia/
├── elevenlabs/
└── builtin/
```

A vendor may expose more than one adapter, for example `openai.LLM`, `openai.TTS`, and `openai.Realtime`, while sharing the same internal client and configuration code.

## Initial implementation order

The first providers will mirror the integrations already proven in the Leamout runtime: Deepgram STT, Groq LLM, Cartesia TTS, and OpenAI Realtime. After that, the repository will expand with AssemblyAI, Anthropic, ElevenLabs, OpenAI LLM/TTS, Google, Gemini, and Azure.

## Boundaries

This repository owns provider-specific clients, configuration validation, credential verification, protocol translation, and normalized event mapping. It does not own tenant credential storage, voice-agent bindings, orchestration, telephony, media session lifecycle, or carrier integrations.

## Development

The module targets Go 1.26.6.

```bash
go test ./...
go vet ./...
```

## License

Apache License 2.0.
