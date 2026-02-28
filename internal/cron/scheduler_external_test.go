package cron

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type testNotifier struct {
	mu       sync.Mutex
	messages []string
}

func (n *testNotifier) NotifyChat(message string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.messages = append(n.messages, message)
	return nil
}

func (n *testNotifier) NotifyChatUser(platform, channelID, userID, message string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.messages = append(n.messages, message)
	return nil
}

func TestSchedulerExternalJobExecution(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		if payload["source"] != "external-agent" {
			t.Fatalf("unexpected source: %#v", payload["source"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"job ok"}`))
	}))
	defer srv.Close()

	store, err := NewStore(filepath.Join(t.TempDir(), "cron.db"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer store.Close()

	notifier := &testNotifier{}
	s := NewScheduler(store, nil, nil, notifier)

	job, err := s.AddExternalJob(
		"ext", "assistant-task", "* * * * *", srv.URL, "Bearer test-token", true,
		map[string]any{"a": 1}, "wecom", "channel", "user",
	)
	if err != nil {
		t.Fatalf("add external job: %v", err)
	}

	s.executeJob(job)

	if gotAuth != "Bearer test-token" {
		t.Fatalf("unexpected auth header: %q", gotAuth)
	}
	if job.LastRun == nil {
		t.Fatalf("expected last run set")
	}
	if job.LastError != "" {
		t.Fatalf("unexpected last error: %s", job.LastError)
	}
	if len(notifier.messages) == 0 {
		t.Fatalf("expected notification message")
	}
	if notifier.messages[0] != "[external-agent] job ok" {
		t.Fatalf("unexpected notifier message: %q", notifier.messages[0])
	}
}

func TestSchedulerExternalJobFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "fail", http.StatusBadGateway)
	}))
	defer srv.Close()

	store, err := NewStore(filepath.Join(t.TempDir(), "cron.db"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer store.Close()

	notifier := &testNotifier{}
	s := NewScheduler(store, nil, nil, notifier)

	job := &Job{
		ID:        "j1",
		Name:      "bad",
		Type:      "external",
		Schedule:  "* * * * *",
		Endpoint:  srv.URL,
		Platform:  "wecom",
		ChannelID: "c",
		UserID:    "u",
		Enabled:   true,
		CreatedAt: time.Now(),
	}
	s.executeJob(job)

	if job.LastRun == nil {
		t.Fatalf("expected last run set")
	}
	if job.LastError == "" {
		t.Fatalf("expected last error set")
	}

	// Ensure context use does not panic.
	_, _ = s.executeExternalJob(context.Background(), job)
}
