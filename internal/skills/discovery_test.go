package skills

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseSkillMD_OpenclawFormat(t *testing.T) {
	// Test parsing openclaw-style metadata
	content := `---
name: discord
description: Discord skill
metadata: {"openclaw":{"emoji":"🎮","requires":{"config":["channels.discord"]}}}
---

# Discord`

	dir := t.TempDir()
	path := filepath.Join(dir, "SKILL.md")
	os.WriteFile(path, []byte(content), 0644)

	entry, err := ParseSkillMD(path)
	if err != nil {
		t.Fatalf("ParseSkillMD failed: %v", err)
	}

	if entry.Name != "discord" {
		t.Errorf("expected name=discord, got %q", entry.Name)
	}
	if entry.Metadata.Emoji != "🎮" {
		t.Errorf("expected emoji=🎮, got %q", entry.Metadata.Emoji)
	}
}

func TestParseSkillMD_FlatFormat(t *testing.T) {
	content := `---
name: test-skill
description: A test skill
metadata:
  emoji: "🧪"
  requires:
    bins: ["go"]
---

# Test`

	dir := t.TempDir()
	path := filepath.Join(dir, "SKILL.md")
	os.WriteFile(path, []byte(content), 0644)

	entry, err := ParseSkillMD(path)
	if err != nil {
		t.Fatalf("ParseSkillMD failed: %v", err)
	}

	if entry.Name != "test-skill" {
		t.Errorf("expected name=test-skill, got %q", entry.Name)
	}
	if entry.Metadata.Emoji != "🧪" {
		t.Errorf("expected emoji=🧪, got %q", entry.Metadata.Emoji)
	}
	if len(entry.Metadata.Requires.Bins) != 1 || entry.Metadata.Requires.Bins[0] != "go" {
		t.Errorf("expected bins=[go], got %v", entry.Metadata.Requires.Bins)
	}
}

func TestParseSkillMD_MultiLineOpenclaw(t *testing.T) {
	content := `---
name: github
description: "GitHub CLI"
metadata:
  {
    "openclaw":
      {
        "emoji": "🐙",
        "requires": { "bins": ["gh"] },
        "install":
          [
            {
              "id": "brew",
              "kind": "brew",
              "formula": "gh",
              "bins": ["gh"],
              "label": "Install GitHub CLI (brew)",
            },
          ],
      },
  }
---

# GitHub`

	dir := t.TempDir()
	path := filepath.Join(dir, "SKILL.md")
	os.WriteFile(path, []byte(content), 0644)

	entry, err := ParseSkillMD(path)
	if err != nil {
		t.Fatalf("ParseSkillMD failed: %v", err)
	}

	if entry.Metadata.Emoji != "🐙" {
		t.Errorf("expected emoji=🐙, got %q", entry.Metadata.Emoji)
	}
	if len(entry.Metadata.Requires.Bins) != 1 || entry.Metadata.Requires.Bins[0] != "gh" {
		t.Errorf("expected bins=[gh], got %v", entry.Metadata.Requires.Bins)
	}
	if len(entry.Metadata.Install) != 1 {
		t.Errorf("expected 1 install spec, got %d", len(entry.Metadata.Install))
	}
}
