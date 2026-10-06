package gemini

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
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

func (c *client) start(
	ctx context.Context,
	cfg RealtimeConfig,
	request ai.RealtimeRequest,
) (ai.RealtimeStream, error) {
	if ctx == nil {
		return nil, fmt.Errorf("gemini realtime context is required")
	}

	credential := strings.TrimSpace(request.Runtime.Credential)
	if credential == "" {
		return nil, fmt.Errorf("gemini realtime credential is required")
	}
	if err := validateInputFormat(request.InputFormat); err != nil {
		return nil, fmt.Errorf("gemini realtime input format: %w", err)
	}
	if err := validateOutputFormat(request.OutputFormat); err != nil {
		return nil, fmt.Errorf("gemini realtime output format: %w", err)
	}

	parsed, err := url.Parse(cfg.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("parse Gemini Live endpoint: %w", err)
	}
	query := parsed.Query()
	query.Set("key", credential)
	parsed.RawQuery = query.Encode()

	connection, response, err := websocket.Dial(
		ctx,
		parsed.String(),
		&websocket.DialOptions{
			HTTPClient:      c.httpClient,
			CompressionMode: websocket.CompressionDisabled,
		},
	)
	if response != nil && response.Body != nil {
		defer func() { _ = response.Body.Close() }()
	}
	if err != nil {
		if response != nil {
			return nil, fmt.Errorf(
				"connect Gemini Live: HTTP %d: %w",
				response.StatusCode,
				err,
			)
		}
		return nil, fmt.Errorf("connect Gemini Live: %w", err)
	}

	streamCtx, cancel := context.WithCancel(ctx)
	result := &stream{
		ctx:          streamCtx,
		cancel:       cancel,
		connection:   connection,
		inputFormat:  request.InputFormat,
		outputFormat: request.OutputFormat,
		events:       make(chan ai.RealtimeEvent, 64),
		audio:        make(chan ai.AudioFrame, 32),
	}

	if err := result.writeJSON(ctx, setupMessage(cfg, request)); err != nil {
		_ = result.Close(context.Background())
		return nil, fmt.Errorf("configure Gemini Live session: %w", err)
	}
	if err := result.waitForSetup(ctx); err != nil {
		_ = result.Close(context.Background())
		return nil, err
	}

	go result.readLoop()
	return result, nil
}

func setupMessage(
	cfg RealtimeConfig,
	request ai.RealtimeRequest,
) realtimeClientMessage {
	setup := realtimeSetup{
		Model: "models/" + strings.TrimPrefix(cfg.Model, "models/"),
		GenerationConfig: realtimeGenerationConfig{
			ResponseModalities: []string{"AUDIO"},
		},
		InputAudioTranscription:  map[string]any{},
		OutputAudioTranscription: map[string]any{},
	}

	if instructions := strings.TrimSpace(request.Instructions); instructions != "" {
		setup.SystemInstruction = &realtimeContent{
			Parts: []realtimePart{{Text: instructions}},
		}
	}
	if voice := strings.TrimSpace(request.Voice); voice != "" {
		setup.GenerationConfig.SpeechConfig = &realtimeSpeechConfig{
			VoiceConfig: realtimeVoiceConfig{
				PrebuiltVoiceConfig: realtimePrebuiltVoiceConfig{
					VoiceName: voice,
				},
			},
		}
	}
	if len(request.Tools) != 0 {
		declarations := make([]realtimeFunctionDeclaration, 0, len(request.Tools))
		for _, tool := range request.Tools {
			declarations = append(declarations, realtimeFunctionDeclaration{
				Name:                 tool.Name,
				Description:          tool.Description,
				ParametersJSONSchema: tool.Parameters,
			})
		}
		setup.Tools = []realtimeTool{{FunctionDeclarations: declarations}}
	}

	return realtimeClientMessage{Setup: &setup}
}

func validateInputFormat(format ai.AudioFormat) error {
	if err := format.Validate(); err != nil {
		return err
	}
	if format.Encoding != ai.AudioEncodingPCM16LE || format.Channels != 1 {
		return fmt.Errorf("requires mono PCM16LE audio")
	}
	return nil
}

func validateOutputFormat(format ai.AudioFormat) error {
	if err := format.Validate(); err != nil {
		return err
	}
	if format.Encoding != ai.AudioEncodingPCM16LE ||
		format.SampleRateHz != 24000 ||
		format.Channels != 1 {
		return fmt.Errorf("requires mono 24 kHz PCM16LE audio")
	}
	return nil
}
