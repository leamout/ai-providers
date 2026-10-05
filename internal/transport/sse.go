package transport

import (
	"bufio"
	"errors"
	"io"
	"strings"
)

// ReadSSE dispatches complete Server-Sent Events data payloads.
//
// It preserves multiline data fields, ignores comments and non-data fields, and
// treats io.EOF returned by visit as a successful early stop. A network EOF is
// returned to the caller through the normal reader lifecycle.
func ReadSSE(reader io.Reader, visit func([]byte) error) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), 4<<20)

	var data []string
	dispatch := func() error {
		if len(data) == 0 {
			return nil
		}

		payload := []byte(strings.Join(data, "\n"))
		data = nil
		return visit(payload)
	}

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if err := dispatch(); err != nil {
				if errors.Is(err, io.EOF) {
					return nil
				}
				return err
			}
			continue
		}

		if !strings.HasPrefix(line, "data:") {
			continue
		}

		value := strings.TrimPrefix(line, "data:")
		value = strings.TrimPrefix(value, " ")
		data = append(data, value)
	}

	if err := scanner.Err(); err != nil {
		return err
	}

	if err := dispatch(); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return err
	}
	return nil
}
