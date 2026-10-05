package azure

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/leamout/ai-providers/internal/transport"
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

func (c *client) startSTT(ctx context.Context, cfg STTConfig, request ai.STTRequest) (ai.STTStream, error) {
	if ctx == nil {
		return nil, fmt.Errorf("azure context is required")
	}
	if strings.TrimSpace(request.Runtime.Credential) == "" {
		return nil, fmt.Errorf("azure subscription key is required")
	}
	if err := validateSTTAudioFormat(request.Format); err != nil {
		return nil, err
	}
	if request.Language != "" {
		cfg.Language = request.Language
	}

	u, err := url.Parse(cfg.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("parse Azure STT endpoint: %w", err)
	}
	query := u.Query()
	query.Set("language", cfg.Language)
	query.Set("format", "simple")
	u.RawQuery = query.Encode()

	ws, err := transport.Dial(ctx, c.httpClient, u.String(), http.Header{
		"Ocp-Apim-Subscription-Key": {request.Runtime.Credential},
	})
	if err != nil {
		return nil, fmt.Errorf("connect Azure STT: %w", err)
	}

	stream := newSTTStream(ws, request.Format)
	if err := stream.begin(ctx); err != nil {
		_ = ws.Close()
		return nil, err
	}
	go stream.readLoop()
	return stream, nil
}

func validateSTTAudioFormat(format ai.AudioFormat) error {
	if err := format.Validate(); err != nil {
		return err
	}
	if format.Encoding != ai.AudioEncodingPCM16LE || format.Channels != 1 {
		return fmt.Errorf("azure STT requires PCM16 mono audio")
	}
	if format.SampleRateHz != 8000 && format.SampleRateHz != 16000 {
		return fmt.Errorf("azure STT requires 8000 or 16000 Hz audio")
	}
	return nil
}

func (c *client) startTTS(ctx context.Context, cfg TTSConfig, request ai.TTSRequest) (ai.TTSStream, error) {
	if ctx == nil {
		return nil, fmt.Errorf("azure context is required")
	}
	if strings.TrimSpace(request.Runtime.Credential) == "" {
		return nil, fmt.Errorf("azure subscription key is required")
	}
	if request.Voice != "" {
		cfg.Voice = request.Voice
	}
	if request.Language != "" {
		cfg.Language = request.Language
	}
	output, err := azureOutputFormat(request.Format)
	if err != nil {
		return nil, err
	}

	synthesize := func(callCtx context.Context, text string) (io.ReadCloser, error) {
		ssml := buildSSML(cfg.Language, cfg.Voice, text)
		req, err := http.NewRequestWithContext(callCtx, http.MethodPost, cfg.Endpoint, strings.NewReader(ssml))
		if err != nil {
			return nil, fmt.Errorf("create Azure TTS request: %w", err)
		}
		req.Header.Set("Content-Type", "application/ssml+xml")
		req.Header.Set("Ocp-Apim-Subscription-Key", request.Runtime.Credential)
		req.Header.Set("X-Microsoft-OutputFormat", output)
		req.Header.Set("User-Agent", "leamout-ai-providers")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("start Azure TTS synthesis: %w", err)
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			defer func() { _ = resp.Body.Close() }()
			return nil, fmt.Errorf("start Azure TTS synthesis: HTTP %d", resp.StatusCode)
		}
		return resp.Body, nil
	}

	return newTTSStream(ctx, request.Format, synthesize), nil
}

func azureOutputFormat(format ai.AudioFormat) (string, error) {
	if err := format.Validate(); err != nil {
		return "", err
	}
	if format.Channels != 1 {
		return "", fmt.Errorf("azure TTS requires mono audio")
	}

	switch format.Encoding {
	case ai.AudioEncodingPCM16LE:
		switch format.SampleRateHz {
		case 8000, 16000, 24000, 48000:
			return fmt.Sprintf("raw-%dkhz-16bit-mono-pcm", format.SampleRateHz/1000), nil
		}
	case ai.AudioEncodingMuLaw:
		if format.SampleRateHz == 8000 {
			return "raw-8khz-8bit-mono-mulaw", nil
		}
	case ai.AudioEncodingALaw:
		if format.SampleRateHz == 8000 {
			return "raw-8khz-8bit-mono-alaw", nil
		}
	}
	return "", fmt.Errorf("unsupported azure TTS audio format")
}

func buildSSML(language, voice, text string) string {
	escape := func(value string) string {
		var buffer bytes.Buffer
		_ = xml.EscapeText(&buffer, []byte(value))
		return buffer.String()
	}
	return `<speak version="1.0" xmlns="http://www.w3.org/2001/10/synthesis" xml:lang="` + escape(language) + `"><voice name="` + escape(voice) + `">` + escape(text) + `</voice></speak>`
}
