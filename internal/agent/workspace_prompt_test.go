package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadWorkspacePromptBundleRequiresAgentsAndSoul(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("COCO_WORKSPACE_DIR", tmp)

	if err := os.WriteFile(filepath.Join(tmp, "AGENTS.md"), []byte("# only agents"), 0644); err != nil {
		t.Fatalf("write AGENTS.md: %v", err)
	}

	got := loadWorkspacePromptBundle()
	if got != "" {
		t.Fatalf("expected empty bundle when SOUL.md missing, got: %q", got)
	}
}

func TestLoadWorkspacePromptBundleIncludesWorkspaceFiles(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("COCO_WORKSPACE_DIR", tmp)

	mustWrite := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(tmp, name), []byte(content), 0644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	mustWrite("AGENTS.md", "---\ntitle: x\n---\nAgent rules")
	mustWrite("SOUL.md", "Soul values")
	mustWrite("PROFILE.md", "User profile")
	mustWrite("MEMORY.md", "Memory notes")
	mustWrite("HEARTBEAT.md", "Heartbeat tasks")

	got := loadWorkspacePromptBundle()
	if got == "" {
		t.Fatalf("expected non-empty bundle")
	}

	for _, name := range []string{"AGENTS.md", "SOUL.md", "PROFILE.md", "MEMORY.md", "HEARTBEAT.md"} {
		if !strings.Contains(got, "# "+name) {
			t.Fatalf("expected section for %s in bundle: %q", name, got)
		}
	}

	if strings.Contains(got, "title: x") {
		t.Fatalf("expected YAML frontmatter stripped: %q", got)
	}
	if !strings.Contains(got, "Agent rules") {
		t.Fatalf("expected AGENTS content in bundle: %q", got)
	}
}

func TestLoadWorkspaceBootstrapPrompt(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("COCO_WORKSPACE_DIR", tmp)

	if err := os.WriteFile(filepath.Join(tmp, "BOOTSTRAP.md"), []byte("---\nname: b\n---\nBootstrap flow"), 0644); err != nil {
		t.Fatalf("write BOOTSTRAP.md: %v", err)
	}

	got := loadWorkspaceBootstrapPrompt()
	if got != "Bootstrap flow" {
		t.Fatalf("unexpected bootstrap prompt: %q", got)
	}
}

func TestEnsureWorkspaceContractFiles(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("COCO_WORKSPACE_DIR", tmp)

	if err := ensureWorkspaceContractFiles(); err != nil {
		t.Fatalf("ensure workspace files: %v", err)
	}

	for _, name := range []string{"AGENTS.md", "SOUL.md", "USER.md", "JD.md", "PROFILE.md", "MEMORY.md", "HEARTBEAT.md", "BOOTSTRAP.md"} {
		if _, err := os.Stat(filepath.Join(tmp, name)); err != nil {
			t.Fatalf("expected %s to exist: %v", name, err)
		}
	}
}

func TestTargetsWorkspaceSOUL(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("COCO_WORKSPACE_DIR", tmp)
	soulPath := filepath.Join(tmp, "SOUL.md")
	if err := os.WriteFile(soulPath, []byte("# SOUL"), 0o644); err != nil {
		t.Fatalf("write SOUL.md: %v", err)
	}

	if !targetsWorkspaceSOUL(map[string]any{"path": soulPath}) {
		t.Fatalf("expected SOUL path to be protected")
	}
	if targetsWorkspaceSOUL(map[string]any{"path": filepath.Join(tmp, "other.md")}) {
		t.Fatalf("non-SOUL path should not be protected")
	}
}

func TestIsExplicitSoulAppendIntent(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		{text: "在你的SOUL文件里加上和蔼可亲的性格", want: true},
		{text: "please append this to soul.md", want: true},
		{text: "帮我总结今天任务", want: false},
		{text: "灵魂很重要", want: false},
	}

	for _, c := range cases {
		got := isExplicitSoulAppendIntent(c.text)
		if got != c.want {
			t.Fatalf("intent mismatch for %q: got=%v want=%v", c.text, got, c.want)
		}
	}
}
