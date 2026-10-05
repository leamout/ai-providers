package elevenlabs

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/leamout/ai-providers/internal/transport"
	"github.com/leamout/sdk/ai"
)

type client struct {
	httpClient *http.Client
}

func newClient(httpClient *http.Client) *client {
	return &client{httpClient: httpClient}
}

func (c *client) verifyCredential(ctx context.Context, credential string) error {
	credential = strings.TrimSpace(credential)
	if credential == "" {
		return fmt.Errorf("elevenlabs credential is required")
	}
	return transport.Verify(
		ctx,
		c.httpClient,
		DefaultVerifyEndpoint,
		http.Header{"Xi-Api-Key": {credential}},
	)
}

func (c *client) start(ctx context.Context, credential string, cfg Config, request ai.TTSRequest) (*stream, error) {
	if ctx == nil {
		return nil, fmt.Errorf("elevenlabs context is required")
	}
	credential = strings.TrimSpace(credential)
	if credential == "" {
		return nil, fmt.Errorf("elevenlabs credential is required")
	}

	if voice := strings.TrimSpace(request.Voice); voice != "" {
		cfg.VoiceID = voice
	}
	if strings.TrimSpace(cfg.VoiceID) == "" {
		return nil, fmt.Errorf("elevenlabs voice_id or request voice is required")
	}
	if language := strings.TrimSpace(request.Language); language != "" {
		cfg.Language = language
	}

	outputFormat, err := outputFormat(request.Format)
	if err != nil {
		return nil, err
	}
	endpoint, err := buildEndpoint(cfg, outputFormat)
	if err != nil {
		return nil, err
	}

	ws, err := transport.Dial(
		ctx,
		c.httpClient,
		endpoint,
		http.Header{"Xi-Api-Key": {credential}},
	)
	if err != nil {
		return nil, fmt.Errorf("start ElevenLabs stream: %w", err)
	}
	if err := ws.JSON(ctx, inputMessage{Text: " "}); err != nil {
		_ = ws.Close()
		return nil, fmt.Errorf("initialize ElevenLabs stream: %w", err)
	}

	result := newStream(ws, request.Format)
	go result.readLoop()
	return result, nil
}

func buildEndpoint(cfg Config, output string) (string, error) {
	endpoint := strings.TrimRight(cfg.Endpoint, "/") + "/" + url.PathEscape(cfg.VoiceID) + "/stream-input"
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("build ElevenLabs endpoint: %w", err)
	}

	query := parsed.Query()
	query.Set("model_id", cfg.Model)
	query.Set("output_format", output)
	if language := strings.TrimSpace(cfg.Language); language != "" {
		query.Set("language_code", language)
	}
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func outputFormat(format ai.AudioFormat) (string, error) {
	if err := format.Validate(); err != nil {
		return "", err
	}
	if format.Channels != 1 {
		return "", fmt.Errorf("elevenlabs requires mono audio")
	}

	switch format.Encoding {
	case ai.AudioEncodingPCM16LE:
		switch format.SampleRateHz {
		case 8000, 16000, 22050, 24000, 44100, 48000:
			return "pcm_" + strconv.Itoa(format.SampleRateHz), nil
		}
	case ai.AudioEncodingMuLaw:
		if format.SampleRateHz == 8000 {
			return "ulaw_8000", nil
		}
	case ai.AudioEncodingALaw:
		if format.SampleRateHz == 8000 {
			return "alaw_8000", nil
		}
	}
	return "", fmt.Errorf("unsupported elevenlabs audio format")
}
