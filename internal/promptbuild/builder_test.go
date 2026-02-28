package promptbuild

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kayz/coco/internal/config"
)

func configForTest(root string) config.PromptBuildConfig {
	return config.PromptBuildConfig{
		RootDir:            root,
		TemplatesDir:       "prompts",
		SQLitePath:         ".coco.db",
		AuditEnabled:       false,
		AuditDir:           ".coco/promptbuild-audit",
		AuditRetentionDays: 7,
		AuditFilePrefix:    "promptbuild",
	}
}

func TestBuildLegacyCompatibilityWithoutSpec(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "prompts", "system"), 0755); err != nil {
		t.Fatalf("mkdir system templates: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "prompts", "task"), 0755); err != nil {
		t.Fatalf("mkdir task templates: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "prompts", "system", "role.md"), []byte("system role"), 0644); err != nil {
		t.Fatalf("write system template: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "prompts", "task", "goal.md"), []byte("task goal"), 0644); err != nil {
		t.Fatalf("write task template: %v", err)
	}

	b := NewBuilder(configForTest(dir))
	out, err := b.Build(BuildRequest{
		System:       []string{"system/role.md"},
		Task:         []string{"task/goal.md"},
		Requirements: "do something",
		UserInput:    "hello",
	})
	if err != nil {
		t.Fatalf("Build legacy failed: %v", err)
	}

	expectedOrder := []string{"### System", "### Task", "### Requirements", "### User Input"}
	lastPos := -1
	for _, marker := range expectedOrder {
		idx := strings.Index(out, marker)
		if idx == -1 {
			t.Fatalf("expected output to contain %q", marker)
		}
		if idx <= lastPos {
			t.Fatalf("expected marker %q after previous marker", marker)
		}
		lastPos = idx
	}
}

func TestBuildWithSpecOrderAndRequired(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "prompts", "specs"), 0755); err != nil {
		t.Fatalf("mkdir specs dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "refs"), 0755); err != nil {
		t.Fatalf("mkdir refs dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "refs", "evidence.md"), []byte("evidence content"), 0644); err != nil {
		t.Fatalf("write reference: %v", err)
	}

	spec := `version: v1
agent: demo
sections:
  - id: step2
    title: Second
    required: true
    source_type: inline_text
    source: inline_value
    order: 20
  - id: step1
    title: First
    required: true
    source_type: request_field
    source: requirements
    order: 10
  - id: step3
    title: Third
    required: false
    source_type: references
    source: refs/evidence.md
    order: 30
`
	if err := os.WriteFile(filepath.Join(dir, "prompts", "specs", "demo.yaml"), []byte(spec), 0644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	b := NewBuilder(configForTest(dir))
	out, err := b.Build(BuildRequest{
		Agent:        "demo",
		Requirements: "must appear first",
		Inputs: map[string]string{
			"inline_value": "must appear second",
		},
	})
	if err != nil {
		t.Fatalf("Build with spec failed: %v", err)
	}

	first := strings.Index(out, "### First")
	second := strings.Index(out, "### Second")
	third := strings.Index(out, "### Third")
	if !(first >= 0 && second > first && third > second) {
		t.Fatalf("expected spec section order First->Second->Third, got: %s", out)
	}

	_, err = b.Build(BuildRequest{Agent: "demo"})
	if err == nil {
		t.Fatalf("expected required section error when requirements/inputs missing")
	}
}

func TestBuildWithSpecSectionMaxChars(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "prompts", "specs"), 0755); err != nil {
		t.Fatalf("mkdir specs dir: %v", err)
	}

	spec := `version: v1
agent: demo
sections:
  - id: long
    title: Long
    required: true
    source_type: inline_text
    source: long_text
    order: 10
    max_chars: 40
`
	if err := os.WriteFile(filepath.Join(dir, "prompts", "specs", "demo.yaml"), []byte(spec), 0644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	b := NewBuilder(configForTest(dir))
	out, err := b.Build(BuildRequest{
		Agent: "demo",
		Inputs: map[string]string{
			"long_text": "abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyz",
		},
	})
	if err != nil {
		t.Fatalf("Build with section max chars failed: %v", err)
	}
	if runeCount(out) <= 40 {
		t.Fatalf("expected full output (with header) to exceed 40 chars for this test: %s", out)
	}
	if !strings.Contains(out, "### Long") {
		t.Fatalf("expected Long section header: %s", out)
	}
	if strings.Contains(out, "abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyz") {
		t.Fatalf("expected long content to be truncated: %s", out)
	}
}

func TestBuildWithSpecMaxPromptChars(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "prompts", "specs"), 0755); err != nil {
		t.Fatalf("mkdir specs dir: %v", err)
	}

	spec := `version: v1
agent: demo
defaults:
  max_prompt_chars: 140
sections:
  - id: required_main
    title: Main
    required: true
    source_type: request_field
    source: requirements
    order: 10
  - id: optional_tail
    title: Tail
    required: false
    source_type: inline_text
    source: tail
    order: 20
`
	if err := os.WriteFile(filepath.Join(dir, "prompts", "specs", "demo.yaml"), []byte(spec), 0644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	b := NewBuilder(configForTest(dir))
	out, err := b.Build(BuildRequest{
		Agent:        "demo",
		Requirements: strings.Repeat("A", 80),
		Inputs: map[string]string{
			"tail": strings.Repeat("B", 300),
		},
	})
	if err != nil {
		t.Fatalf("Build with max prompt chars failed: %v", err)
	}
	if runeCount(out) > 140 {
		t.Fatalf("expected output <= 140 chars, got %d: %s", runeCount(out), out)
	}
	if !strings.Contains(out, "### Main") {
		t.Fatalf("required section should remain in output: %s", out)
	}
}
