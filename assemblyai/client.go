package assemblyai

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/leamout/ai-providers/internal/transport"
	"github.com/leamout/contracts/ai"
)

type client struct {
	httpClient *http.Client
}

func newClient(httpClient *http.Client) *client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &client{httpClient: httpClient}
}

func (c *client) verifyCredential(ctx context.Context, credential string) error {
	credential = strings.TrimSpace(credential)
	if credential == "" {
		return fmt.Errorf("assemblyai credential is required")
	}
	return transport.Verify(ctx, c.httpClient, DefaultVerifyEndpoint, http.Header{"Authorization": {credential}})
}

func (c *client) start(ctx context.Context, credential string, cfg Config, request ai.STTRequest) (*stream, error) {
	if ctx == nil {
		return nil, fmt.Errorf("assemblyai context is required")
	}
	credential = strings.TrimSpace(credential)
	if credential == "" {
		return nil, fmt.Errorf("assemblyai credential is required")
	}
	if err := validateAudioFormat(request.Format); err != nil {
		return nil, err
	}
	if strings.TrimSpace(request.Language) != "" {
		return nil, fmt.Errorf("assemblyai language is selected by speech_model, not request language")
	}

	u, err := url.Parse(cfg.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("parse AssemblyAI endpoint: %w", err)
	}
	query := u.Query()
	query.Set("sample_rate", strconv.Itoa(request.Format.SampleRateHz))
	query.Set("encoding", encodingName(request.Format.Encoding))
	query.Set("speech_model", cfg.SpeechModel)
	query.Set("format_turns", strconv.FormatBool(cfg.FormatTurns))
	u.RawQuery = query.Encode()

	ws, err := transport.Dial(ctx, c.httpClient, u.String(), http.Header{"Authorization": {credential}})
	if err != nil {
		return nil, fmt.Errorf("start AssemblyAI stream: %w", err)
	}

	result := newStream(ws, request.Format, cfg.FormatTurns)
	go result.readLoop()
	return result, nil
}

func validateAudioFormat(format ai.AudioFormat) error {
	if err := format.Validate(); err != nil {
		return err
	}
	if format.Channels != 1 {
		return fmt.Errorf("assemblyai requires mono audio")
	}
	switch format.Encoding {
	case ai.AudioEncodingPCM16LE, ai.AudioEncodingMuLaw:
	default:
		return fmt.Errorf("assemblyai requires PCM16 or mulaw audio")
	}
	if format.SampleRateHz < 8000 || format.SampleRateHz > 48000 {
		return fmt.Errorf("assemblyai sample rate must be 8000–48000 Hz")
	}
	return nil
}

func encodingName(encoding ai.AudioEncoding) string {
	if encoding == ai.AudioEncodingMuLaw {
		return "pcm_mulaw"
	}
	return "pcm_s16le"
}
