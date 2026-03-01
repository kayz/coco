package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kayz/coco/internal/router"
)

func TestSplitMarkdownFrontMatter(t *testing.T) {
	fm, body := splitMarkdownFrontMatter("---\nenabled: true\n---\n# HEARTBEAT\nx")
	if fm == "" {
		t.Fatalf("frontmatter should not be empty")
	}
	if body == "" {
		t.Fatalf("body should not be empty")
	}
}

func TestLoadHeartbeatSpecFromChecksAndInterval(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("COCO_WORKSPACE_DIR", tmp)

	content := `---
enabled: true
interval: 30m
checks:
  - name: memory-watch
    prompt: 检查近期记忆是否有冲突，输出简短结论
---
# HEARTBEAT
`
	if err := os.WriteFile(filepath.Join(tmp, "HEARTBEAT.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write HEARTBEAT.md: %v", err)
	}

	spec, err := loadHeartbeatSpec()
	if err != nil {
		t.Fatalf("loadHeartbeatSpec failed: %v", err)
	}
	if spec == nil {
		t.Fatalf("expected non-nil spec")
	}
	if len(spec.Tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(spec.Tasks))
	}
	if spec.Tasks[0].Schedule != "@every 30m" {
		t.Fatalf("expected @every 30m, got %q", spec.Tasks[0].Schedule)
	}
}

func TestResolveHeartbeatTargetAlwaysKeepsConversationContext(t *testing.T) {
	msg := routerMessageForHeartbeatTest()
	p, c, u := resolveHeartbeatTarget(heartbeatTask{Name: "x", Prompt: "y"}, msg)
	if p != msg.Platform || c != msg.ChannelID || u != msg.UserID {
		t.Fatalf("heartbeat should keep context by default")
	}

	p, c, u = resolveHeartbeatTarget(heartbeatTask{Name: "x", Prompt: "y", Notify: "always"}, msg)
	if p != msg.Platform || c != msg.ChannelID || u != msg.UserID {
		t.Fatalf("notify=always should target current conversation")
	}
}

func TestHeartbeatNotifyNormalizationAndDecorate(t *testing.T) {
	if got := normalizeHeartbeatNotify("on_change"); got != "on_change" {
		t.Fatalf("expected on_change, got %q", got)
	}
	if got := normalizeHeartbeatNotify("AUTO"); got != "auto" {
		t.Fatalf("expected auto, got %q", got)
	}
	if got := normalizeHeartbeatNotify("???"); got != "never" {
		t.Fatalf("unexpected fallback: %q", got)
	}

	prompt := decorateHeartbeatPrompt("巡检内容", "on_change")
	if prompt == "" || prompt[:18] != "[HEARTBEAT_NOTIFY=" {
		t.Fatalf("decorated prompt missing metadata: %q", prompt)
	}
}

func routerMessageForHeartbeatTest() router.Message {
	return router.Message{
		Platform:  "wecom",
		ChannelID: "c1",
		UserID:    "u1",
	}
}
