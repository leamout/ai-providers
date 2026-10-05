// Package transport contains shared, provider-neutral network mechanics.
package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// Endpoint validates an absolute endpoint URL for the expected scheme.
func Endpoint(value, scheme string) error {
	u, err := url.Parse(value)
	if err != nil || u.Scheme != scheme || u.Host == "" || u.User != nil || u.Fragment != "" {
		return fmt.Errorf("endpoint must be an absolute %s URL without user info or fragment", scheme)
	}
	return nil
}

// Client returns c when provided, otherwise http.DefaultClient.
func Client(c *http.Client) *http.Client {
	if c == nil {
		return http.DefaultClient
	}
	return c
}

// JSON sends an HTTP request, JSON-encoding payload when it is non-nil.
// The caller owns the response body on success.
func JSON(
	ctx context.Context,
	client *http.Client,
	method string,
	endpoint string,
	headers http.Header,
	payload any,
) (*http.Response, error) {
	if ctx == nil {
		return nil, errors.New("context is required")
	}

	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(encoded)
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

	resp, err := Client(client).Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("upstream HTTP %d", resp.StatusCode)
	}
	return resp, nil
}

// Verify performs a GET request and closes the successful response body.
func Verify(ctx context.Context, client *http.Client, endpoint string, headers http.Header) error {
	resp, err := JSON(ctx, client, http.MethodGet, endpoint, headers, nil)
	if err != nil {
		return err
	}
	return resp.Body.Close()
}
