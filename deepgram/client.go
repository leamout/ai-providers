package deepgram

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/coder/websocket"
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

func (c *client) start(ctx context.Context, credential string, cfg Config, format ai.AudioFormat) (*stream, error) {
	if ctx == nil {
		return nil, fmt.Errorf("deepgram context is required")
	}
	if strings.TrimSpace(credential) == "" {
		return nil, fmt.Errorf("deepgram credential is required")
	}
	if err := validateAudioFormat(format); err != nil {
		return nil, err
	}

	endpoint, err := buildEndpoint(cfg, format)
	if err != nil {
		return nil, err
	}

	connection, response, err := websocket.Dial(ctx, endpoint, &websocket.DialOptions{
		HTTPClient: c.httpClient,
		HTTPHeader: http.Header{
			"Authorization": []string{"Token " + strings.TrimSpace(credential)},
		},
		CompressionMode: websocket.CompressionDisabled,
	})
	if response != nil && response.Body != nil {
		defer func() { _ = response.Body.Close() }()
	}
	if err != nil {
		if response != nil {
			return nil, fmt.Errorf("connect Deepgram: HTTP %d: %w", response.StatusCode, err)
		}
		return nil, fmt.Errorf("connect Deepgram: %w", err)
	}

	streamCtx, cancel := context.WithCancel(ctx)
	result := newStream(streamCtx, cancel, connection, format)
	go result.readLoop()
	return result, nil
}

func buildEndpoint(cfg Config, format ai.AudioFormat) (string, error) {
	if err := validateConfig(cfg); err != nil {
		return "", err
	}
	if err := validateAudioFormat(format); err != nil {
		return "", err
	}

	endpoint := strings.TrimSpace(cfg.Endpoint)
	if endpoint == "" {
		endpoint = DefaultEndpoint
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("parse Deepgram endpoint: %w", err)
	}

	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		model = DefaultModel
	}
	encoding, err := deepgramEncoding(format.Encoding)
	if err != nil {
		return "", err
	}

	query := parsed.Query()
	query.Set("model", model)
	query.Set("encoding", encoding)
	query.Set("sample_rate", strconv.Itoa(format.SampleRateHz))
	for _, language := range cfg.LanguageHints {
		if language = strings.TrimSpace(language); language != "" {
			query.Add("language_hint", language)
		}
	}
	if cfg.EOTThreshold != nil {
		query.Set("eot_threshold", strconv.FormatFloat(*cfg.EOTThreshold, 'f', -1, 64))
	}
	if cfg.EagerEOTThreshold != nil {
		query.Set("eager_eot_threshold", strconv.FormatFloat(*cfg.EagerEOTThreshold, 'f', -1, 64))
	}
	if cfg.EOTTimeoutMS > 0 {
		query.Set("eot_timeout_ms", strconv.Itoa(cfg.EOTTimeoutMS))
	}
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func validateAudioFormat(format ai.AudioFormat) error {
	if err := format.Validate(); err != nil {
		return fmt.Errorf("deepgram audio format: %w", err)
	}
	if format.Channels != 1 {
		return fmt.Errorf("deepgram Flux requires mono audio")
	}
	switch format.SampleRateHz {
	case 8000, 16000, 24000, 44100, 48000:
	default:
		return fmt.Errorf("deepgram Flux unsupported sample rate %d", format.SampleRateHz)
	}
	_, err := deepgramEncoding(format.Encoding)
	return err
}

func deepgramEncoding(encoding ai.AudioEncoding) (string, error) {
	switch encoding {
	case ai.AudioEncodingPCM16LE:
		return "linear16", nil
	case ai.AudioEncodingMuLaw:
		return "mulaw", nil
	case ai.AudioEncodingALaw:
		return "alaw", nil
	default:
		return "", fmt.Errorf("deepgram Flux unsupported audio encoding %q", encoding)
	}
}
