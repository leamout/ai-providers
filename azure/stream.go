package azure

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/leamout/ai-providers/internal/transport"
	"github.com/leamout/sdk/ai"
)

type sttStream struct {
	ws        *transport.WS
	format    ai.AudioFormat
	events    chan ai.STTEvent
	done      chan struct{}
	mu        sync.Mutex
	requestID string
	final     bool
}

func newSTTStream(ws *transport.WS, format ai.AudioFormat) *sttStream {
	return &sttStream{
		ws:     ws,
		format: format,
		events: make(chan ai.STTEvent, 32),
		done:   make(chan struct{}),
	}
}

func (s *sttStream) SendAudio(ctx context.Context, frame ai.AudioFrame) error {
	if ctx == nil {
		return fmt.Errorf("azure context is required")
	}
	if err := frame.Validate(); err != nil {
		return err
	}
	if frame.Format != s.format {
		return fmt.Errorf("azure audio format changed")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.final {
		return fmt.Errorf("azure audio finalized")
	}
	return s.writeAudio(ctx, frame.Data)
}

// Finalize sends audio end-of-stream while keeping recognition results readable.
func (s *sttStream) Finalize(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("azure context is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.final {
		return nil
	}
	if err := s.writeAudio(ctx, nil); err != nil {
		return err
	}
	s.final = true
	return nil
}

func (s *sttStream) Events() <-chan ai.STTEvent {
	return s.events
}

func (s *sttStream) Close(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("azure context is required")
	}
	if err := s.Finalize(ctx); err != nil {
		_ = s.ws.Close()
		return err
	}
	select {
	case <-s.done:
		return s.ws.Close()
	case <-ctx.Done():
		_ = s.ws.Close()
		return ctx.Err()
	}
}

func (s *sttStream) begin(ctx context.Context) error {
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		return fmt.Errorf("create Azure request ID: %w", err)
	}
	s.requestID = hex.EncodeToString(id)

	if err := s.writeText(ctx, "speech.config", map[string]any{
		"context": map[string]any{
			"system": map[string]string{
				"name":    "leamout-ai-providers",
				"version": "1.0",
				"build":   "Go",
			},
		},
	}); err != nil {
		return err
	}
	if err := s.writeText(ctx, "speech.context", map[string]any{}); err != nil {
		return err
	}
	return s.writeAudio(ctx, waveHeader(s.format.SampleRateHz))
}

func (s *sttStream) header(path, contentType string) string {
	return "Path: " + path + "\r\n" +
		"X-RequestId: " + s.requestID + "\r\n" +
		"X-Timestamp: " + time.Now().UTC().Format(time.RFC3339Nano) + "\r\n" +
		"Content-Type: " + contentType + "\r\n\r\n"
}

func (s *sttStream) writeText(ctx context.Context, path string, value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode Azure STT message: %w", err)
	}
	wire := append([]byte(s.header(path, "application/json")), payload...)
	return s.ws.Write(ctx, websocket.MessageText, wire)
}

func (s *sttStream) writeAudio(ctx context.Context, audio []byte) error {
	header := []byte(s.header("audio", "audio/x-wav"))
	wire := make([]byte, 2, len(header)+len(audio)+2)
	binary.BigEndian.PutUint16(wire, uint16(len(header)))
	wire = append(wire, header...)
	wire = append(wire, audio...)
	return s.ws.Write(ctx, websocket.MessageBinary, wire)
}

func (s *sttStream) readLoop() {
	defer close(s.done)
	defer close(s.events)
	defer func() { _ = s.ws.Close() }()

	for {
		kind, payload, err := s.ws.Conn.Read(s.ws.Ctx)
		if err != nil {
			if s.ws.Ctx.Err() == nil {
				s.emit(ai.STTEvent{Type: ai.STTEventError, Err: err})
			}
			return
		}
		if kind != websocket.MessageText {
			s.emit(ai.STTEvent{Type: ai.STTEventError, Err: fmt.Errorf("azure returned non-text event")})
			return
		}

		header, body, ok := strings.Cut(string(payload), "\r\n\r\n")
		if !ok {
			s.emit(ai.STTEvent{Type: ai.STTEventError, Err: fmt.Errorf("invalid azure event framing")})
			return
		}
		path, providerID := parseAzureHeaders(header)

		switch path {
		case "speech.startdetected":
			if !s.emit(ai.STTEvent{Type: ai.STTEventSpeechStarted, ProviderID: providerID}) {
				return
			}
		case "speech.enddetected":
			if !s.emit(ai.STTEvent{Type: ai.STTEventSpeechStopped, ProviderID: providerID}) {
				return
			}
		case "speech.hypothesis", "speech.fragment", "speech.phrase":
			if !s.handleRecognition(path, providerID, body) {
				return
			}
		case "turn.end":
			s.mu.Lock()
			if s.final {
				s.mu.Unlock()
				return
			}
			err := s.begin(s.ws.Ctx)
			s.mu.Unlock()
			if err != nil {
				s.emit(ai.STTEvent{Type: ai.STTEventError, Err: err})
				return
			}
		}
	}
}

func (s *sttStream) handleRecognition(path, providerID, body string) bool {
	var result recognitionResult
	if err := json.Unmarshal([]byte(body), &result); err != nil {
		return s.emit(ai.STTEvent{Type: ai.STTEventError, Err: err})
	}

	if path == "speech.phrase" {
		if result.RecognitionStatus != "Success" {
			switch result.RecognitionStatus {
			case "NoMatch", "InitialSilenceTimeout", "EndOfDictation":
				return true
			default:
				return s.emit(ai.STTEvent{Type: ai.STTEventError, Err: fmt.Errorf("azure recognition: %s", result.RecognitionStatus)})
			}
		}
		return s.emit(ai.STTEvent{
			Type:       ai.STTEventTranscriptFinal,
			Text:       result.DisplayText,
			ProviderID: providerID,
		})
	}

	return s.emit(ai.STTEvent{
		Type:       ai.STTEventTranscriptDelta,
		Text:       result.Text,
		ProviderID: providerID,
	})
}

func parseAzureHeaders(header string) (path, providerID string) {
	for _, line := range strings.Split(header, "\r\n") {
		key, value, _ := strings.Cut(line, ":")
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "path":
			path = strings.ToLower(strings.TrimSpace(value))
		case "x-requestid":
			providerID = strings.TrimSpace(value)
		}
	}
	return path, providerID
}

func (s *sttStream) emit(event ai.STTEvent) bool {
	select {
	case s.events <- event:
		return true
	case <-s.ws.Ctx.Done():
		return false
	}
}

func waveHeader(sampleRate int) []byte {
	buffer := make([]byte, 44)
	copy(buffer, "RIFF")
	binary.LittleEndian.PutUint32(buffer[4:], 0)
	copy(buffer[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(buffer[16:], 16)
	binary.LittleEndian.PutUint16(buffer[20:], 1)
	binary.LittleEndian.PutUint16(buffer[22:], 1)
	binary.LittleEndian.PutUint32(buffer[24:], uint32(sampleRate))
	binary.LittleEndian.PutUint32(buffer[28:], uint32(sampleRate*2))
	binary.LittleEndian.PutUint16(buffer[32:], 2)
	binary.LittleEndian.PutUint16(buffer[34:], 16)
	copy(buffer[36:], "data")
	binary.LittleEndian.PutUint32(buffer[40:], 0)
	return buffer
}

type ttsStream struct {
	ctx        context.Context
	cancel     context.CancelFunc
	format     ai.AudioFormat
	synthesize func(context.Context, string) (io.ReadCloser, error)
	input      chan ai.TextChunk
	events     chan ai.TTSEvent
	mu         sync.Mutex
	final      bool
	closeOnce  sync.Once
}

func newTTSStream(parent context.Context, format ai.AudioFormat, synthesize func(context.Context, string) (io.ReadCloser, error)) *ttsStream {
	ctx, cancel := context.WithCancel(parent)
	stream := &ttsStream{
		ctx:        ctx,
		cancel:     cancel,
		format:     format,
		synthesize: synthesize,
		input:      make(chan ai.TextChunk, 16),
		events:     make(chan ai.TTSEvent, 32),
	}
	go stream.run()
	return stream
}

func (s *ttsStream) SendText(ctx context.Context, chunk ai.TextChunk) error {
	if ctx == nil {
		return fmt.Errorf("azure context is required")
	}
	if chunk.Text == "" && !chunk.Final {
		return fmt.Errorf("azure text or final marker is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.final || s.ctx.Err() != nil {
		return fmt.Errorf("azure TTS stream is closed or finalized")
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-s.ctx.Done():
		return s.ctx.Err()
	case s.input <- chunk:
		s.final = chunk.Final
		return nil
	}
}

func (s *ttsStream) Events() <-chan ai.TTSEvent {
	return s.events
}

func (s *ttsStream) Close() error {
	s.closeOnce.Do(s.cancel)
	return nil
}

func (s *ttsStream) run() {
	defer close(s.events)
	defer func() { _ = s.Close() }()

	for {
		select {
		case <-s.ctx.Done():
			return
		case chunk := <-s.input:
			if chunk.Text != "" {
				if err := s.synthesizeText(chunk.Text); err != nil {
					if s.ctx.Err() == nil {
						s.emitTTS(ai.TTSEvent{Err: err})
					}
					return
				}
			}
			if chunk.Final {
				s.emitTTS(ai.TTSEvent{Done: true})
				return
			}
		}
	}
}

func (s *ttsStream) synthesizeText(text string) error {
	body, err := s.synthesize(s.ctx, text)
	if err != nil {
		return err
	}
	stop := context.AfterFunc(s.ctx, func() { _ = body.Close() })
	defer stop()
	defer func() { _ = body.Close() }()
	return s.readAudio(body)
}

func (s *ttsStream) readAudio(reader io.Reader) error {
	width := s.format.Channels
	if s.format.Encoding == ai.AudioEncodingPCM16LE {
		width *= 2
	}

	pending := make([]byte, 0, 8192)
	buffer := make([]byte, 8192)
	for {
		n, err := reader.Read(buffer)
		pending = append(pending, buffer[:n]...)
		size := len(pending) / width * width
		if size > 0 {
			data := append([]byte(nil), pending[:size]...)
			pending = pending[size:]
			if !s.emitTTS(ai.TTSEvent{Audio: ai.AudioFrame{Data: data, Format: s.format}}) {
				return s.ctx.Err()
			}
		}
		if err != nil {
			if err == io.EOF {
				if len(pending) != 0 {
					return fmt.Errorf("azure upstream returned incomplete audio sample")
				}
				return nil
			}
			return err
		}
	}
}

func (s *ttsStream) emitTTS(event ai.TTSEvent) bool {
	select {
	case s.events <- event:
		return true
	case <-s.ctx.Done():
		return false
	}
}

var _ ai.STTStream = (*sttStream)(nil)
var _ ai.TTSStream = (*ttsStream)(nil)
