package transport

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestClientRejectsRedirectsWithoutMutatingCaller(t *testing.T) {
	t.Parallel()

	var redirected atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirected.Store(true)
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer origin.Close()

	original := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return nil
		},
	}
	safe := Client(original)
	if safe == original {
		t.Fatal("secure client mutated the supplied client")
	}
	if original.CheckRedirect == nil {
		t.Fatal("original redirect policy was changed")
	}

	req, err := http.NewRequest(http.MethodGet, origin.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := safe.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusTemporaryRedirect {
		t.Fatalf("status = %d, want temporary redirect", resp.StatusCode)
	}
	if redirected.Load() {
		t.Fatal("provider HTTP client followed a redirect")
	}
}
