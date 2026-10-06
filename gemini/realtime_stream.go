package gemini

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/leamout/contracts/ai"
)

type realtimeClient struct {
	httpClient *http.Client
}

func newRealtimeClient(httpClient *http.Client) *realtimeClient {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &realtimeClient{httpClient: httpClient}
}

func (c *realtimeClient) start(
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
	result := &realtimeStream{
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
		declarations := make(
			[]realtimeFunctionDeclaration,
			0,
			len(request.Tools),
		)
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

type realtimeStream struct {
	ctx          context.Context
	cancel       context.CancelFunc
	connection   *websocket.Conn
	inputFormat  ai.AudioFormat
	outputFormat ai.AudioFormat
	events       chan ai.RealtimeEvent
	audio        chan ai.AudioFrame
	writeMu      sync.Mutex
	closeOnce    sync.Once
	speechActive bool
	responseActive bool
}

func (s *realtimeStream) waitForSetup(ctx context.Context) error {
	kind, payload, err := s.connection.Read(ctx)
	if err != nil {
		return fmt.Errorf("wait for Gemini Live setup: %w", err)
	}
	if kind != websocket.MessageText {
		return fmt.Errorf("Gemini Live returned a non-text setup response")
	}

	var message realtimeServerMessage
	if err := json.Unmarshal(payload, &message); err != nil {
		return fmt.Errorf("decode Gemini Live setup response: %w", err)
	}
	if message.SetupComplete == nil {
		return fmt.Errorf("Gemini Live setup was not acknowledged")
	}

	return nil
}

func (s *realtimeStream) SendAudio(
	ctx context.Context,
	frame ai.AudioFrame,
) error {
	if err := frame.Validate(); err != nil {
		return err
	}
	if frame.Format != s.inputFormat {
		return fmt.Errorf("gemini realtime audio format changed during stream")
	}

	return s.writeJSON(ctx, realtimeClientMessage{
		RealtimeInput: &realtimeInput{
			Audio: &realtimeBlob{
				Data: base64.StdEncoding.EncodeToString(frame.Data),
				MimeType: fmt.Sprintf(
					"audio/pcm;rate=%d",
					frame.Format.SampleRateHz,
				),
			},
		},
	})
}

func (s *realtimeStream) Interrupt(context.Context) error {
	// Gemini Live performs barge-in through server-side activity detection.
	// The next realtime audio input interrupts the active model turn.
	return nil
}

func (s *realtimeStream) SubmitToolResult(
	ctx context.Context,
	result ai.ToolResult,
) error {
	if strings.TrimSpace(result.ToolCallID) == "" {
		return fmt.Errorf("gemini realtime tool_call_id is required")
	}
	if strings.TrimSpace(result.Name) == "" {
		return fmt.Errorf("gemini realtime tool name is required")
	}

	response := make(map[string]any)
	if result.IsError {
		response["error"] = result.Content
	} else if err := json.Unmarshal([]byte(result.Content), &response); err != nil {
		response["result"] = result.Content
	}

	return s.writeJSON(ctx, realtimeClientMessage{
		ToolResponse: &realtimeToolResponse{
			FunctionResponses: []realtimeFunctionResponse{{
				ID:       result.ToolCallID,
				Name:     result.Name,
				Response: response,
			}},
		},
	})
}

func (s *realtimeStream) Audio() <-chan ai.AudioFrame {
	return s.audio
}

func (s *realtimeStream) Events() <-chan ai.RealtimeEvent {
	return s.events
}

func (s *realtimeStream) Close(context.Context) error {
	var err error
	s.closeOnce.Do(func() {
		s.cancel()
		err = s.connection.CloseNow()
	})
	return err
}

func (s *realtimeStream) writeJSON(ctx context.Context, value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.connection.Write(ctx, websocket.MessageText, payload)
}

func (s *realtimeStream) readLoop() {
	defer close(s.events)
	defer close(s.audio)
	defer func() { _ = s.Close(context.Background()) }()

	for {
		kind, payload, err := s.connection.Read(s.ctx)
		if err != nil {
			if s.ctx.Err() == nil &&
				websocket.CloseStatus(err) != websocket.StatusNormalClosure {
				s.emit(ai.RealtimeEvent{
					Type: ai.RealtimeEventError,
					Failure: &ai.Failure{
						Source:   "gemini",
						Message:  fmt.Sprintf("read Gemini Live: %v", err),
						Terminal: true,
					},
					OccurredAt: time.Now().UTC(),
				})
			}
			return
		}
		if kind != websocket.MessageText {
			s.emit(ai.RealtimeEvent{
				Type: ai.RealtimeEventError,
				Failure: &ai.Failure{
					Source:   "gemini",
					Message:  "Gemini Live returned a non-text event",
					Terminal: true,
				},
				OccurredAt: time.Now().UTC(),
			})
			return
		}

		var message realtimeServerMessage
		if err := json.Unmarshal(payload, &message); err != nil {
			s.emit(ai.RealtimeEvent{
				Type: ai.RealtimeEventError,
				Failure: &ai.Failure{
					Source:   "gemini",
					Message:  fmt.Sprintf("decode Gemini Live event: %v", err),
					Terminal: true,
				},
				OccurredAt: time.Now().UTC(),
			})
			return
		}

		s.handle(message)
	}
}

func (s *realtimeStream) handle(message realtimeServerMessage) {
	now := time.Now().UTC()

	if message.UsageMetadata != nil {
		usage := message.UsageMetadata
		s.emit(ai.RealtimeEvent{
			Type: ai.RealtimeEventUsage,
			Usage: &ai.Usage{
				InputTokens:  usage.PromptTokenCount,
				OutputTokens: usage.CandidatesTokenCount,
				TotalTokens:  usage.TotalTokenCount,
			},
			OccurredAt: now,
		})
	}
	if message.ToolCall != nil {
		for _, call := range message.ToolCall.FunctionCalls {
			arguments, err := json.Marshal(call.Args)
			if err != nil {
				continue
			}
			s.emit(ai.RealtimeEvent{
				Type: ai.RealtimeEventToolCall,
				ToolCall: &ai.ToolCall{
					ID:        call.ID,
					Name:      call.Name,
					Arguments: arguments,
				},
				ProviderID: call.ID,
				OccurredAt: now,
			})
		}
	}
	if message.GoAway != nil {
		s.emit(ai.RealtimeEvent{
			Type: ai.RealtimeEventError,
			Failure: &ai.Failure{
				Source:   "gemini",
				Code:     "go_away",
				Message:  "Gemini Live session is closing",
				Terminal: true,
			},
			OccurredAt: now,
		})
	}
	if message.ServerContent != nil {
		s.handleServerContent(*message.ServerContent, now)
	}
}

func (s *realtimeStream) handleServerContent(
	content realtimeServerContent,
	now time.Time,
) {
	if content.InterimInputTranscription != nil {
		if !s.speechActive {
			s.speechActive = true
			s.emit(ai.RealtimeEvent{
				Type:       ai.RealtimeEventSpeechStarted,
				OccurredAt: now,
			})
		}
		if text := content.InterimInputTranscription.Text; text != "" {
			s.emit(ai.RealtimeEvent{
				Type: ai.RealtimeEventTranscriptDelta,
				Transcript: &ai.TranscriptEvent{Text: text},
				OccurredAt: now,
			})
		}
	}

	if content.InputTranscription != nil {
		if text := content.InputTranscription.Text; text != "" {
			s.emit(ai.RealtimeEvent{
				Type: ai.RealtimeEventTranscriptFinal,
				Transcript: &ai.TranscriptEvent{Text: text},
				OccurredAt: now,
			})
		}
		if s.speechActive {
			s.speechActive = false
			s.emit(ai.RealtimeEvent{
				Type:       ai.RealtimeEventSpeechStopped,
				OccurredAt: now,
			})
		}
	}

	if content.OutputTranscription != nil {
		text := content.OutputTranscription.Text
		if text != "" {
			s.ensureResponseStarted(now)
			s.emit(ai.RealtimeEvent{
				Type: ai.RealtimeEventResponseDelta,
				Response: &ai.ResponseEvent{Text: text},
				OccurredAt: now,
			})
		}
	}

	if content.ModelTurn != nil {
		for _, part := range content.ModelTurn.Parts {
			if part.InlineData == nil || part.InlineData.Data == "" {
				continue
			}
			audio, err := base64.StdEncoding.DecodeString(part.InlineData.Data)
			if err != nil {
				s.emit(ai.RealtimeEvent{
					Type: ai.RealtimeEventError,
					Failure: &ai.Failure{
						Source:  "gemini",
						Message: err.Error(),
					},
					OccurredAt: now,
				})
				continue
			}

			s.ensureResponseStarted(now)
			frame := ai.AudioFrame{
				Data:       audio,
				Format:     s.outputFormat,
				CapturedAt: now,
			}
			select {
			case s.audio <- frame:
			case <-s.ctx.Done():
				return
			}
		}
	}

	if content.Interrupted || content.GenerationComplete || content.TurnComplete {
		s.finishResponse(now)
	}
}

func (s *realtimeStream) ensureResponseStarted(now time.Time) {
	if s.responseActive {
		return
	}
	s.responseActive = true
	s.emit(ai.RealtimeEvent{
		Type:       ai.RealtimeEventResponseStarted,
		OccurredAt: now,
	})
}

func (s *realtimeStream) finishResponse(now time.Time) {
	if !s.responseActive {
		return
	}
	s.responseActive = false
	s.emit(ai.RealtimeEvent{
		Type:       ai.RealtimeEventResponseStopped,
		OccurredAt: now,
	})
}

func (s *realtimeStream) emit(event ai.RealtimeEvent) {
	select {
	case s.events <- event:
	case <-s.ctx.Done():
	}
}

var _ ai.RealtimeStream = (*realtimeStream)(nil)
