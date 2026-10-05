package cartesia

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/coder/websocket"
	"github.com/leamout/sdk/ai"
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

func (c *client) start(ctx context.Context, credential string, cfg Config, format ai.AudioFormat) (*stream, error) {
	if ctx == nil {
		return nil, fmt.Errorf("cartesia context is required")
	}
	credential = strings.TrimSpace(credential)
	if credential == "" {
		return nil, fmt.Errorf("cartesia credential is required")
	}
	if strings.TrimSpace(cfg.VoiceID) == "" {
		return nil, fmt.Errorf("cartesia voice ID is required")
	}
	if err := validateAudioFormat(format); err != nil {
		return nil, err
	}

	parsed, err := url.Parse(cfg.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("parse Cartesia endpoint: %w", err)
	}
	query := parsed.Query()
	query.Set("cartesia_version", cfg.APIVersion)
	parsed.RawQuery = query.Encode()

	connection, response, err := websocket.Dial(ctx, parsed.String(), &websocket.DialOptions{
		HTTPClient: c.httpClient,
		HTTPHeader: http.Header{"X-API-Key": []string{credential}},
		CompressionMode: websocket.CompressionDisabled,
	})
	if response != nil && response.Body != nil {
		defer func() { _ = response.Body.Close() }()
	}
	if err != nil {
		if response != nil {
			return nil, fmt.Errorf("connect Cartesia: HTTP %d: %w", response.StatusCode, err)
		}
		return nil, fmt.Errorf("connect Cartesia: %w", err)
	}

	contextID, err := newContextID()
	if err != nil {
		_ = connection.CloseNow()
		return nil, err
	}
	streamCtx, cancel := context.WithCancel(ctx)
	result := newStream(streamCtx, cancel, connection, format, contextID)
	result.request = generationRequest{
		ModelID:   cfg.Model,
		Voice:     cfg.VoiceID,
		Language:  cfg.Language,
		ContextID: contextID,
		OutputFormat: outputFormat{
			Container:  "raw",
			Encoding:   "pcm_s16le",
			SampleRate: format.SampleRateHz,
		},
	}
	go result.readLoop()
	return result, nil
}

func validateAudioFormat(format ai.AudioFormat) error {
	if err := format.Validate(); err != nil {
		return fmt.Errorf("cartesia audio format: %w", err)
	}
	if format.Encoding != ai.AudioEncodingPCM16LE {
		return fmt.Errorf("cartesia unsupported audio encoding %q", format.Encoding)
	}
	if format.Channels != 1 {
		return fmt.Errorf("cartesia requires mono audio")
	}
	return nil
}

func newContextID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("create Cartesia context ID: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}
