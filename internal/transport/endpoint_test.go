package transport

import (
	"testing"
)

func TestAllowedEndpoint(t *testing.T) {
	t.Parallel()

	const official = "https://api.example.com/v1/chat/completions"
	const custom = "https://proxy.example.net/v1/chat/completions"

	tests := []struct {
		name       string
		endpoint   string
		additional []string
		wantError  bool
	}{
		{name: "official", endpoint: official},
		{name: "approved custom", endpoint: custom, additional: []string{custom}},
		{name: "unapproved custom", endpoint: custom, wantError: true},
		{name: "different official path", endpoint: "https://api.example.com/v1/keys", wantError: true},
		{name: "lookalike domain", endpoint: "https://api.example.com.evil.test/v1/chat/completions", wantError: true},
		{name: "explicit port", endpoint: "https://api.example.com:443/v1/chat/completions", wantError: true},
		{name: "HTTP downgrade", endpoint: "http://api.example.com/v1/chat/completions", wantError: true},
		{name: "userinfo", endpoint: "https://name@api.example.com/v1/chat/completions", wantError: true},
		{name: "fragment", endpoint: official + "#fragment", wantError: true},
		{name: "substring is not enough", endpoint: custom + "/evil", additional: []string{custom}, wantError: true},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := AllowedEndpoint(test.endpoint, official, test.additional)
			if (err != nil) != test.wantError {
				t.Fatalf("AllowedEndpoint() error = %v, want error = %v", err, test.wantError)
			}
		})
	}
}
