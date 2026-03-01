package cron

import "testing"

func TestParseHeartbeatPromptMeta(t *testing.T) {
	mode, prompt := parseHeartbeatPromptMeta("[HEARTBEAT_NOTIFY=on_change]\ncheck memory")
	if mode != "on_change" {
		t.Fatalf("mode mismatch: %q", mode)
	}
	if prompt != "check memory" {
		t.Fatalf("prompt mismatch: %q", prompt)
	}

	mode, prompt = parseHeartbeatPromptMeta("plain prompt")
	if mode != "never" || prompt != "plain prompt" {
		t.Fatalf("default parse mismatch: mode=%q prompt=%q", mode, prompt)
	}
}

func TestDecideHeartbeatNotificationOnChange(t *testing.T) {
	job := &Job{Tag: "heartbeat"}

	notify, _ := decideHeartbeatNotification(job, "on_change", "first report")
	if notify {
		t.Fatalf("first run should establish baseline without notify")
	}
	if heartbeatStoredHash(job.Source) == "" {
		t.Fatalf("expected hash stored after first run")
	}

	notify, _ = decideHeartbeatNotification(job, "on_change", "first report")
	if notify {
		t.Fatalf("same report should not notify")
	}

	notify, text := decideHeartbeatNotification(job, "on_change", "second report")
	if !notify {
		t.Fatalf("changed report should notify")
	}
	if text != "second report" {
		t.Fatalf("unexpected text: %q", text)
	}
}

func TestDecideHeartbeatNotificationAutoExplicit(t *testing.T) {
	job := &Job{Tag: "heartbeat"}

	notify, text := decideHeartbeatNotification(job, "auto", "HEARTBEAT_NOTIFY: yes\nplease ping user")
	if !notify {
		t.Fatalf("explicit auto yes should notify")
	}
	if text != "please ping user" {
		t.Fatalf("unexpected auto body: %q", text)
	}

	notify, _ = decideHeartbeatNotification(job, "auto", "HEARTBEAT_NOTIFY: no\nskip")
	if notify {
		t.Fatalf("explicit auto no should not notify")
	}
}
