package transport

import (
	"io"
	"strings"
	"testing"
)

func TestSSEFraming(t *testing.T) {
	var events []string
	err := ReadSSE(
		strings.NewReader(": heartbeat\r\nevent: delta\r\ndata: {\r\ndata: \"text\":\"hello\"}\r\n\r\ndata: last"),
		func(data []byte) error {
			events = append(events, string(data))
			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("unexpected event count %d", len(events))
	}
	if events[0] != "{\n\"text\":\"hello\"}" {
		t.Fatalf("unexpected first event %q", events[0])
	}
	if events[1] != "last" {
		t.Fatalf("unexpected second event %q", events[1])
	}
}

func TestSSETerminalStopsReading(t *testing.T) {
	count := 0
	err := ReadSSE(
		strings.NewReader("data: done\n\ndata: ignored\n\n"),
		func([]byte) error {
			count++
			return io.EOF
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("unexpected visit count %d", count)
	}
}
