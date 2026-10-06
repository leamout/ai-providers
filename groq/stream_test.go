package groq

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

func TestStreamRejectsIncompleteResponses(t *testing.T) {
	for _, body := range []string{"", "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n"} {
		s := newStream(context.Background(), io.NopCloser(strings.NewReader(body)))
		go s.readLoop()
		var got error
		for event := range s.Events() {
			if event.Done {
				t.Fatal("incomplete response reported completion")
			}
			if event.Err != nil {
				got = event.Err
			}
		}
		if !errors.Is(got, io.ErrUnexpectedEOF) {
			t.Fatalf("expected unexpected EOF, got %v", got)
		}
	}
}

func TestStreamReportsUpstreamError(t *testing.T) {
	body := "data: {\"error\":{\"message\":\"rate limit exceeded\"}}\n\ndata: [DONE]\n\n"
	s := newStream(context.Background(), io.NopCloser(strings.NewReader(body)))
	go s.readLoop()
	var got error
	for event := range s.Events() {
		if event.Done {
			t.Fatal("upstream error reported completion")
		}
		got = event.Err
	}
	if got == nil || !strings.Contains(got.Error(), "rate limit exceeded") {
		t.Fatalf("expected upstream error, got %v", got)
	}
}

func TestStreamMapsMultilineSSEAndCompletion(t *testing.T) {
	body := "data: {\"id\":\"resp-1\",\n" +
		"data: \"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n" +
		"data: [DONE]\n\n"
	s := newStream(context.Background(), io.NopCloser(strings.NewReader(body)))
	go s.readLoop()
	var text string
	var done bool
	for event := range s.Events() {
		if event.Err != nil {
			t.Fatal(event.Err)
		}
		text += event.TextDelta
		done = done || event.Done
	}
	if text != "hello" || !done {
		t.Fatalf("unexpected text %q, done %v", text, done)
	}
}

func TestStreamCancellationUnblocksReader(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reader, writer := io.Pipe()
	defer writer.Close()
	s := newStream(ctx, reader)
	go s.readLoop()
	cancel()
	select {
	case _, ok := <-s.Events():
		if ok {
			t.Fatal("expected event channel to close")
		}
	case <-time.After(time.Second):
		t.Fatal("cancellation did not unblock reader")
	}
}
