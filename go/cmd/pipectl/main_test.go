package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestClientLoginStoresToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/auth/login" || r.Method != http.MethodPost {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"accessToken":"local-token"}`))
	}))
	defer server.Close()
	tokenFile := filepath.Join(t.TempDir(), "token")
	c := client{baseURL: server.URL, tokenFile: tokenFile, http: server.Client()}
	if err := c.login([]string{"--email", "user@example.test", "--password", "local-password"}); err != nil {
		t.Fatal(err)
	}
	value, err := os.ReadFile(tokenFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(value) != "local-token\n" {
		t.Fatalf("unexpected token file: %q", value)
	}
}
