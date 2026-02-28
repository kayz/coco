package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFromPathReadsSecuritySection(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, ".coco.yaml")
	content := `security:
  allowed_paths:
    - "/tmp/work"
  blocked_commands:
    - "danger-one"
  require_confirmation:
    - "needs-confirm"
  allow_from:
    - "telegram:1001"
  require_mention_in_group: true
  enable_ssrf_protection: false
  disable_file_tools: true
`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadFromPath(cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if !cfg.Security.DisableFileTools {
		t.Fatalf("expected disable_file_tools=true")
	}
	if len(cfg.Security.BlockedCommands) != 1 || cfg.Security.BlockedCommands[0] != "danger-one" {
		t.Fatalf("unexpected blocked commands: %#v", cfg.Security.BlockedCommands)
	}
	if len(cfg.Security.RequireConfirmation) != 1 || cfg.Security.RequireConfirmation[0] != "needs-confirm" {
		t.Fatalf("unexpected require confirmation: %#v", cfg.Security.RequireConfirmation)
	}
	if len(cfg.Security.AllowFrom) != 1 || cfg.Security.AllowFrom[0] != "telegram:1001" {
		t.Fatalf("unexpected allow_from: %#v", cfg.Security.AllowFrom)
	}
	if !cfg.Security.RequireMentionInGroup {
		t.Fatalf("expected require_mention_in_group=true")
	}
	if cfg.Security.EnableSSRFProtection {
		t.Fatalf("expected enable_ssrf_protection=false")
	}
}
