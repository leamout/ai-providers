// Package transport contains shared, provider-neutral network mechanics.
package transport

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

func Endpoint(value, scheme string) error {
	u, err := url.Parse(value)
	if err != nil || u.Scheme != scheme || u.Host == "" || u.User != nil || u.Fragment != "" {
		return fmt.Errorf("endpoint must be an absolute %s URL without user info or fragment", scheme)
	}
	return nil
}
func Client(c *http.Client) *http.Client {
	if c == nil {
		return http.DefaultClient
	}
	return c
}
func JSON(ctx context.Context, c *http.Client, method, endpoint string, headers http.Header, payload any) (*http.Response, error) {
	if ctx == nil {
		return nil, errors.New("context is required")
	}
	var body io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, err
	}
	req.Header = headers.Clone()
	if req.Header == nil {
		req.Header = make(http.Header)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := Client(c).Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		resp.Body.Close()
		return nil, fmt.Errorf("upstream HTTP %d", resp.StatusCode)
	}
	return resp, nil
}
func Verify(ctx context.Context, c *http.Client, endpoint string, headers http.Header) error {
	r, e := JSON(ctx, c, http.MethodGet, endpoint, headers, nil)
	if e != nil {
		return e
	}
	r.Body.Close()
	return nil
}

// ReadSSE dispatches complete events, preserving multiline data and ignoring comments.
// Returning io.EOF from visit ends successfully; network EOF is handled by the caller.
func ReadSSE(r io.Reader, visit func([]byte) error) error {
	scan := bufio.NewScanner(r)
	scan.Buffer(make([]byte, 4096), 4<<20)
	var data []string
	dispatch := func() error {
		if len(data) == 0 {
			return nil
		}
		b := []byte(strings.Join(data, "\n"))
		data = nil
		return visit(b)
	}
	for scan.Scan() {
		line := scan.Text()
		if line == "" {
			if err := dispatch(); err != nil {
				if errors.Is(err, io.EOF) {
					return nil
				}
				return err
			}
			continue
		}
		if strings.HasPrefix(line, "data:") {
			v := strings.TrimPrefix(line, "data:")
			v = strings.TrimPrefix(v, " ")
			data = append(data, v)
		}
	}
	if err := scan.Err(); err != nil {
		return err
	}
	err := dispatch()
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}
