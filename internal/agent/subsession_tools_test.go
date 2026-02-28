package agent

import (
	"strings"
	"testing"
)

func TestExecuteSessionsSpawnAndSend(t *testing.T) {
	a := &Agent{subSessions: NewSubSessionStore()}

	spawned := a.executeSessionsSpawn(map[string]any{"name": "planner"})
	if !strings.Contains(spawned, "Sub-session created: id=sub-") {
		t.Fatalf("unexpected spawn response: %s", spawned)
	}

	parts := strings.Split(spawned, "id=")
	if len(parts) < 2 {
		t.Fatalf("failed to parse session id from %q", spawned)
	}
	idPart := strings.Split(parts[1], " ")[0]

	sent := a.executeSessionsSend(map[string]any{
		"session_id": idPart,
		"message":    "run build",
	})
	if !strings.Contains(sent, "Message delivered") {
		t.Fatalf("unexpected send response: %s", sent)
	}
	if !strings.Contains(sent, "total messages: 1") {
		t.Fatalf("expected message count update, got: %s", sent)
	}
}

func TestExecuteSessionsSendMissingSession(t *testing.T) {
	a := &Agent{subSessions: NewSubSessionStore()}
	out := a.executeSessionsSend(map[string]any{
		"session_id": "missing",
		"message":    "hello",
	})
	if !strings.Contains(out, "Error: session not found") {
		t.Fatalf("unexpected error message: %s", out)
	}
}
