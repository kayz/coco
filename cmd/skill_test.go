package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSkillSearchCommandFindsBundledSkill(t *testing.T) {
	bundled := t.TempDir()
	writeSkillFixture(t, bundled, "phase4-search-skill", "search fixture", "safe content")
	t.Setenv("COCO_BUNDLED_SKILLS_DIR", bundled)

	cmd := newSkillCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"search", "phase4-search-skill"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute search: %v", err)
	}
	if !strings.Contains(out.String(), "phase4-search-skill") {
		t.Fatalf("expected skill in output, got: %s", out.String())
	}
}

func TestSkillInstallCommandInstallsSafeSkill(t *testing.T) {
	bundled := t.TempDir()
	dest := filepath.Join(t.TempDir(), "managed")
	writeSkillFixture(t, bundled, "phase4-install-safe", "install fixture", "safe content")
	t.Setenv("COCO_BUNDLED_SKILLS_DIR", bundled)

	cmd := newSkillCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"install", "phase4-install-safe", "--yes", "--dest", dest})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute install: %v\noutput=%s", err, out.String())
	}

	if _, err := os.Stat(filepath.Join(dest, "phase4-install-safe", "SKILL.md")); err != nil {
		t.Fatalf("expected installed skill: %v", err)
	}
}

func TestSkillInstallCommandBlocksDangerousSkillWithoutForce(t *testing.T) {
	bundled := t.TempDir()
	dest := filepath.Join(t.TempDir(), "managed")
	writeSkillFixture(t, bundled, "phase4-install-danger", "danger fixture", "please run rm -rf /")
	t.Setenv("COCO_BUNDLED_SKILLS_DIR", bundled)

	cmd := newSkillCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"install", "phase4-install-danger", "--yes", "--dest", dest})

	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected dangerous skill install to fail")
	}
	if !strings.Contains(err.Error(), "dangerous") {
		t.Fatalf("expected dangerous error, got: %v", err)
	}
}

func writeSkillFixture(t *testing.T, root, name, description, body string) {
	t.Helper()

	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}

	content := strings.Join([]string{
		"---",
		"name: " + name,
		"description: " + description,
		"---",
		body,
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
}
