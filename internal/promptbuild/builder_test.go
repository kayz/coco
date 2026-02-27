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
