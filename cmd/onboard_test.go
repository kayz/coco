package cmd

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInferKeeperBaseURLFromRelay(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: "wss://keeper.example.com/ws", want: "https://keeper.example.com"},
		{input: "ws://127.0.0.1:8080/ws", want: "http://127.0.0.1:8080"},
		{input: "https://keeper.example.com/ws", want: ""},
	}

	for _, tt := range tests {
		got := inferKeeperBaseURLFromRelay(tt.input)
		if got != tt.want {
			t.Fatalf("inferKeeperBaseURLFromRelay(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestRenderHeartbeatMarkdownFallbackNotify(t *testing.T) {
	out := renderHeartbeatMarkdown("6h", "invalid", "check")
	if !strings.Contains(out, "notify: never") {
		t.Fatalf("expected invalid notify to fallback to never, got:\n%s", out)
	}
}

func TestUploadHeartbeatToKeeper(t *testing.T) {
	token := "secret-token"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/heartbeat/upload" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true,"path":"/tmp/HEARTBEAT.md"}`))
	}))
	defer srv.Close()

	tmp := t.TempDir()
	hbPath := filepath.Join(tmp, "HEARTBEAT.md")
	if err := os.WriteFile(hbPath, []byte("# HB\n"), 0644); err != nil {
		t.Fatalf("write temp heartbeat: %v", err)
	}

	path, err := uploadHeartbeatToKeeper(srv.URL, token, hbPath)
	if err != nil {
		t.Fatalf("uploadHeartbeatToKeeper returned error: %v", err)
	}
	if path != "/tmp/HEARTBEAT.md" {
		t.Fatalf("unexpected uploaded path: %s", path)
	}
}
