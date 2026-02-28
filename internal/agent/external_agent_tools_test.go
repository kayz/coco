package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kayz/coco/internal/router"
)

func TestExecuteSpawnAgentSuccess(t *testing.T) {
	var gotSource string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSource = r.Header.Get("X-Coco-Source")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"hello from ext"}`))
	}))
	defer srv.Close()

	a := &Agent{
		currentMsg: router.Message{
			Platform:  "wecom",
			ChannelID: "ch",
			UserID:    "u",
			Username:  "name",
		},
	}

	out := a.executeSpawnAgent(context.Background(), map[string]any{
		"endpoint": srv.URL,
		"prompt":   "run task",
		"auth":     "Bearer abc",
	})
	if !strings.Contains(out, "[external-agent] hello from ext") {
		t.Fatalf("unexpected output: %q", out)
	}
	if gotSource != "external-agent" {
		t.Fatalf("unexpected source header: %q", gotSource)
	}
}

func TestExecuteSpawnAgentValidation(t *testing.T) {
	a := &Agent{}
	if out := a.executeSpawnAgent(context.Background(), map[string]any{}); !strings.Contains(out, "endpoint is required") {
		t.Fatalf("unexpected output: %q", out)
	}
}
