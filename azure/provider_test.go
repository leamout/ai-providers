package azure

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/leamout/sdk/ai"
)

func TestDescriptors(t *testing.T) {
	t.Parallel()

	stt := (STT{}).Descriptor()
	if stt.ID != "azure" || stt.Kind != ai.KindSTT {
		t.Fatalf("unexpected STT descriptor: %#v", stt)
	}
	if !stt.Supports(ai.CapabilityStreaming) || !stt.Supports(ai.CapabilityTurnDetection) {
		t.Fatalf("unexpected STT capabilities: %#v", stt.Capabilities)
	}

	tts := (TTS{}).Descriptor()
	if tts.ID != "azure" || tts.Kind != ai.KindTTS {
		t.Fatalf("unexpected TTS descriptor: %#v", tts)
	}
	if !tts.Supports(ai.CapabilityStreaming) {
		t.Fatalf("unexpected TTS capabilities: %#v", tts.Capabilities)
	}
}

func TestConfigDerivesRegionalEndpoints(t *testing.T) {
	t.Parallel()

	stt, err := decodeSTTConfig(json.RawMessage(`{"region":"eastus"}`))
	if err != nil {
		t.Fatal(err)
	}
	if stt.Endpoint != "wss://eastus.stt.speech.microsoft.com/speech/recognition/conversation/cognitiveservices/v1" {
		t.Fatalf("unexpected STT endpoint %q", stt.Endpoint)
	}
	if stt.Language != DefaultSTTLanguage {
		t.Fatalf("unexpected STT language %q", stt.Language)
	}

	tts, err := decodeTTSConfig(json.RawMessage(`{"region":"eastus"}`))
	if err != nil {
		t.Fatal(err)
	}
	if tts.Endpoint != "https://eastus.tts.speech.microsoft.com/cognitiveservices/v1" {
		t.Fatalf("unexpected TTS endpoint %q", tts.Endpoint)
	}
	if tts.Voice != DefaultTTSVoice || tts.Language != DefaultTTSLanguage {
		t.Fatalf("unexpected TTS defaults: %#v", tts)
	}
}

func TestConfigRequiresRegionOrEndpoint(t *testing.T) {
	t.Parallel()

	if _, err := decodeSTTConfig(nil); err == nil {
		t.Fatal("expected STT region validation error")
	}
	if _, err := decodeTTSConfig(nil); err == nil {
		t.Fatal("expected TTS region validation error")
	}
}

func TestAzureAudioFormats(t *testing.T) {
	t.Parallel()

	if err := validateSTTAudioFormat(ai.AudioFormat{
		Encoding:     ai.AudioEncodingPCM16LE,
		SampleRateHz: 16000,
		Channels:     1,
	}); err != nil {
		t.Fatal(err)
	}
	if err := validateSTTAudioFormat(ai.AudioFormat{
		Encoding:     ai.AudioEncodingMuLaw,
		SampleRateHz: 8000,
		Channels:     1,
	}); err == nil {
		t.Fatal("expected unsupported STT encoding")
	}

	format, err := azureOutputFormat(ai.AudioFormat{
		Encoding:     ai.AudioEncodingPCM16LE,
		SampleRateHz: 24000,
		Channels:     1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if format != "raw-24khz-16bit-mono-pcm" {
		t.Fatalf("unexpected TTS format %q", format)
	}
}

func TestBuildSSMLEscapesValues(t *testing.T) {
	t.Parallel()

	ssml := buildSSML(`en-US`, `voice&name`, `<hello & goodbye>`)
	if !strings.Contains(ssml, `voice&amp;name`) {
		t.Fatalf("voice was not escaped: %s", ssml)
	}
	if !strings.Contains(ssml, `&lt;hello &amp; goodbye&gt;`) {
		t.Fatalf("text was not escaped: %s", ssml)
	}
}
