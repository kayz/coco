package agent

import "testing"

func TestSubSessionStoreSpawnAndSend(t *testing.T) {
	store := NewSubSessionStore()
	session := store.Spawn("planner")
	if session.ID == "" {
		t.Fatalf("expected non-empty session id")
	}
	if session.Name != "planner" {
		t.Fatalf("unexpected session name: %s", session.Name)
	}

	updated, err := store.Send(session.ID, "run task")
	if err != nil {
		t.Fatalf("send failed: %v", err)
	}
	if updated.MessageCount != 1 {
		t.Fatalf("expected message count 1, got %d", updated.MessageCount)
	}
	if updated.LastMessage != "run task" {
		t.Fatalf("unexpected last message: %s", updated.LastMessage)
	}
}

func TestSubSessionStoreSendUnknownSession(t *testing.T) {
	store := NewSubSessionStore()
	if _, err := store.Send("missing", "hello"); err == nil {
		t.Fatalf("expected error for missing session")
	}
}
