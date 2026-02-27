package promptbuild

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadPromptAssemblySpecSortsAndValidates(t *testing.T) {
	dir := t.TempDir()
	specPath := filepath.Join(dir, "spec.yaml")
	content := `version: v1
agent: demo
sections:
  - id: b
    title: B
    source_type: inline_text
    source: two
    order: 20
  - id: a
    title: A
    source_type: inline_text
    source: one
    order: 10
`
	if err := os.WriteFile(specPath, []byte(content), 0644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	b := NewBuilder(configForTest(dir))
	spec, err := b.loadPromptAssemblySpec(BuildRequest{SpecPath: specPath})
	if err != nil {
		t.Fatalf("loadPromptAssemblySpec failed: %v", err)
	}
	if len(spec.Sections) != 2 {
		t.Fatalf("expected 2 sections, got %d", len(spec.Sections))
	}
	if spec.Sections[0].ID != "a" || spec.Sections[1].ID != "b" {
		t.Fatalf("expected sorted sections [a,b], got [%s,%s]", spec.Sections[0].ID, spec.Sections[1].ID)
	}
}

func TestValidatePromptAssemblySpecRequiredChecks(t *testing.T) {
	tests := []struct {
		name string
		spec PromptAssemblySpec
	}{
		{
			name: "missing agent",
			spec: PromptAssemblySpec{Sections: []SectionSpec{{ID: "s1", SourceType: "inline_text", Source: "x"}}},
		},
		{
			name: "missing section id",
			spec: PromptAssemblySpec{Agent: "a", Sections: []SectionSpec{{SourceType: "inline_text", Source: "x"}}},
		},
		{
			name: "missing templates",
			spec: PromptAssemblySpec{Agent: "a", Sections: []SectionSpec{{ID: "s1", SourceType: "templates"}}},
		},
		{
			name: "unsupported source_type",
			spec: PromptAssemblySpec{Agent: "a", Sections: []SectionSpec{{ID: "s1", SourceType: "unknown"}}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := validatePromptAssemblySpec(&tc.spec); err == nil {
				t.Fatalf("expected validation error")
			}
		})
	}
}
