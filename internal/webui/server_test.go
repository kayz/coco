package webui

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kayz/coco/internal/router"
)

type fakeProcessor struct{}

func (fakeProcessor) HandleMessage(_ context.Context, msg router.Message) (router.Response, error) {
	return router.Response{Text: "echo: " + msg.Text}, nil
}

func TestStatusEndpoint(t *testing.T) {
	server := NewServer(fakeProcessor{})
	handler := server.Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "\"ok\":true") {
		t.Fatalf("unexpected status payload: %s", rr.Body.String())
	}
}

func TestChatEndpoint(t *testing.T) {
	server := NewServer(fakeProcessor{})
	handler := server.Handler()

	payload := map[string]string{
		"session_id": "s1",
		"user_id":    "u1",
		"text":       "hello",
	}
	data, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/api/chat", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "echo: hello") {
		t.Fatalf("unexpected chat response: %s", rr.Body.String())
	}
}
