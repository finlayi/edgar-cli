package edgar

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSecClientRetries429(t *testing.T) {
	resetTickerMapCache()
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("retry-after", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"message":"rate limited"}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}))
	defer server.Close()

	client := NewSecClient(SecClientOptions{UserAgent: "Name user@example.com", HTTPClient: server.Client()})
	var payload map[string]bool
	if err := client.FetchSECJSON(context.Background(), server.URL, &payload); err != nil {
		t.Fatal(err)
	}
	if !payload["ok"] {
		t.Fatalf("payload = %#v", payload)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
}

func TestSecClientMapsUndeclaredToolPage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("<html><h1>Undeclared Automated Tool</h1></html>"))
	}))
	defer server.Close()

	client := NewSecClient(SecClientOptions{UserAgent: "Name user@example.com", HTTPClient: server.Client()})
	var payload map[string]any
	err := client.FetchSECJSON(context.Background(), server.URL, &payload)
	cliErr, ok := err.(*CLIError)
	if !ok {
		t.Fatalf("err = %#v, want CLIError", err)
	}
	if cliErr.Code != ErrorIdentityRequired {
		t.Fatalf("code = %s, want %s", cliErr.Code, ErrorIdentityRequired)
	}
}
