package integration_test

import (
	"context"
	"io"
	"net"
	"testing"

	speechpb "cloud.google.com/go/speech/apiv1/speechpb"
	"github.com/leamout/ai-providers/google"
	"github.com/leamout/sdk/ai"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"
)

type speechServer struct {
	speechpb.UnimplementedSpeechServer
	t *testing.T
}

func (s *speechServer) StreamingRecognize(stream speechpb.Speech_StreamingRecognizeServer) error {
	md, _ := metadata.FromIncomingContext(stream.Context())
	if v := md.Get("authorization"); len(v) != 1 || v[0] != "Bearer access-token" {
		s.t.Errorf("authorization metadata %v", v)
	}
	v, e := stream.Recv()
	if e != nil {
		return e
	}
	cfg := v.GetStreamingConfig()
	if cfg == nil || cfg.Config.Encoding != speechpb.RecognitionConfig_LINEAR16 || cfg.Config.LanguageCode != "fr-FR" || cfg.Config.SampleRateHertz != 16000 || !cfg.InterimResults {
		s.t.Errorf("configuration: %v", cfg)
	}
	v, e = stream.Recv()
	if e != nil {
		return e
	}
	if len(v.GetAudioContent()) != 640 {
		s.t.Errorf("audio: %v", v)
	}
	if _, e = stream.Recv(); e != io.EOF {
		s.t.Errorf("expected half-close: %v", e)
	}
	for _, final := range []bool{false, true} {
		if e := stream.Send(&speechpb.StreamingRecognizeResponse{Results: []*speechpb.StreamingRecognitionResult{{IsFinal: final, Alternatives: []*speechpb.SpeechRecognitionAlternative{{Transcript: "bonjour"}}}}}); e != nil {
			return e
		}
	}
	return nil
}
func TestGoogleStreamingGRPC(t *testing.T) {
	ctx := deadline(t)
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	speechpb.RegisterSpeechServer(server, &speechServer{t: t})
	go server.Serve(listener)
	defer server.Stop()
	conn, e := grpc.NewClient("passthrough:///bufnet", grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Close()
	p := google.STT{Client: speechpb.NewSpeechClient(conn)}
	f := ai.AudioFormat{Encoding: ai.AudioEncodingPCM16LE, SampleRateHz: 16000, Channels: 1}
	s, e := p.StartSTT(ctx, ai.STTRequest{Runtime: ai.Runtime{Credential: "access-token"}, Format: f, Language: "fr-FR"})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close(ctx)
	if e := s.SendAudio(ctx, ai.AudioFrame{Format: f, Data: make([]byte, 640)}); e != nil {
		t.Fatal(e)
	}
	if e := s.Finalize(ctx); e != nil {
		t.Fatal(e)
	}
	if e := s.SendAudio(ctx, ai.AudioFrame{Format: f, Data: make([]byte, 640)}); e == nil {
		t.Fatal("accepted audio after half-close")
	}
	events := []ai.STTEvent{}
	for v := range s.Events() {
		if v.Err != nil {
			t.Fatal(v.Err)
		}
		events = append(events, v)
	}
	if len(events) != 2 || events[0].Type != ai.STTEventTranscriptDelta || events[1].Type != ai.STTEventTranscriptFinal || events[1].Text != "bonjour" {
		t.Fatal(events)
	}
}
