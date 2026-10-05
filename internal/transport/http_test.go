package transport

import (
	"io"
	"strings"
	"testing"
)

func TestSSEFraming(t *testing.T) {
	var events []string
	err := ReadSSE(strings.NewReader(": heartbeat\r\nevent: delta\r\ndata: {\r\ndata: \"text\":\"hello\"}\r\n\r\ndata: last"), func(b []byte) error { events = append(events, string(b)); return nil })
	if err != nil || len(events) != 2 || events[0] != "{\n\"text\":\"hello\"}" || events[1] != "last" {
		t.Fatalf("%v %v", events, err)
	}
}
func TestSSETerminalStopsReading(t *testing.T) {
	n := 0
	e := ReadSSE(strings.NewReader("data: done\n\ndata: ignored\n\n"), func(b []byte) error { n++; return io.EOF })
	if e != nil || n != 1 {
		t.Fatalf("n %d e %v", n, e)
	}
}
