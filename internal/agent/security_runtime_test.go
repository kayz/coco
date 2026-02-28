package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestValidateShellCommandBlockedByPolicy(t *testing.T) {
	a := &Agent{}
	a.applySecurityConfig(nil, false, []string{"danger-cmd"}, nil, nil, false)

	msg := a.validateShellCommand("echo hello && danger-cmd --now")
	if !strings.Contains(msg, "ACCESS DENIED") {
		t.Fatalf("expected blocked message, got: %q", msg)
	}
}

func TestValidateShellCommandRequireConfirmation(t *testing.T) {
	a := &Agent{autoApprove: false}
	a.applySecurityConfig(nil, false, nil, []string{"git push"}, nil, false)

	msg := a.validateShellCommand("git push origin main")
	if !strings.Contains(msg, "CONFIRMATION REQUIRED") {
		t.Fatalf("expected confirmation required, got: %q", msg)
	}
}

func TestValidateShellCommandBypassConfirmationWhenAutoApprove(t *testing.T) {
	a := &Agent{autoApprove: true}
	a.applySecurityConfig(nil, false, nil, []string{"git push"}, nil, false)

	msg := a.validateShellCommand("git push origin main")
	if msg != "" {
		t.Fatalf("expected no confirmation in auto approve mode, got: %q", msg)
	}
}

func TestRefreshRuntimeSecurityConfigReloadsFromFile(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, ".coco.yaml")
	content := `security:
  allowed_paths:
    - "."
  blocked_commands:
    - "custom-block"
  require_confirmation:
    - "custom-confirm"
  allow_from:
    - "telegram:1234"
  require_mention_in_group: true
  disable_file_tools: true
model_cooldown: "3m"
`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	a := &Agent{
		configPath: cfgPath,
	}
	a.applySecurityConfig(nil, false, nil, nil, nil, false)
	a.refreshRuntimeSecurityConfig()

	snapshot := a.securitySnapshot()
	if !snapshot.disableFileTools {
		t.Fatalf("expected disable file tools to reload from config")
	}
	if len(snapshot.blockedCommands) == 0 {
		t.Fatalf("expected blocked commands to be loaded")
	}
	if len(snapshot.allowFrom) != 1 || snapshot.allowFrom[0] != "telegram:1234" {
		t.Fatalf("expected allow_from to be loaded, got %#v", snapshot.allowFrom)
	}
	if !snapshot.requireMentionInGroup {
		t.Fatalf("expected require_mention_in_group to be loaded")
	}
	if a.searchManager == nil {
		t.Fatalf("expected search manager to be reloaded")
	}

	if msg := a.validateShellCommand("custom-block now"); !strings.Contains(msg, "ACCESS DENIED") {
		t.Fatalf("expected blocked command after reload, got %q", msg)
	}

	if msg := a.validateShellCommand("custom-confirm now"); !strings.Contains(msg, "CONFIRMATION REQUIRED") {
		t.Fatalf("expected confirmation command after reload, got %q", msg)
	}

	// Ensure mtime cache prevents panics on quick no-op reload.
	time.Sleep(10 * time.Millisecond)
	a.refreshRuntimeSecurityConfig()
}
