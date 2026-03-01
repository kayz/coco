package agent

import "testing"

func TestInferKeeperBaseURLForCron(t *testing.T) {
	got := inferKeeperBaseURLForCron("https://keeper.example.com/webhook", "")
	if got != "https://keeper.example.com" {
		t.Fatalf("unexpected webhook base url: %s", got)
	}

	got = inferKeeperBaseURLForCron("", "wss://keeper.example.com/ws")
	if got != "https://keeper.example.com" {
		t.Fatalf("unexpected ws base url: %s", got)
	}

	got = inferKeeperBaseURLForCron("", "ws://127.0.0.1:8080/ws")
	if got != "http://127.0.0.1:8080" {
		t.Fatalf("unexpected local ws base url: %s", got)
	}
}
