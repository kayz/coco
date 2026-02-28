package promptbuild

// PromptAssemblySpec defines a YAML-driven prompt assembly contract for an agent/scene.
type PromptAssemblySpec struct {
	Version     string                `yaml:"version" json:"version"`
	Agent       string                `yaml:"agent" json:"agent"`
	Description string                `yaml:"description,omitempty" json:"description,omitempty"`
	Defaults    PromptAssemblyDefault `yaml:"defaults,omitempty" json:"defaults,omitempty"`
	Sections    []SectionSpec         `yaml:"sections" json:"sections"`
}

// PromptAssemblyDefault defines optional defaults for a spec.
type PromptAssemblyDefault struct {
	IncludeSectionHeaders *bool `yaml:"include_section_headers,omitempty" json:"include_section_headers,omitempty"`
	MaxHistory            int   `yaml:"max_history,omitempty" json:"max_history,omitempty"`
	MaxPromptChars        int   `yaml:"max_prompt_chars,omitempty" json:"max_prompt_chars,omitempty"`
}

// SectionSpec defines one section in the assembly plan.
type SectionSpec struct {
	ID         string   `yaml:"id" json:"id"`
	Title      string   `yaml:"title,omitempty" json:"title,omitempty"`
	Required   bool     `yaml:"required,omitempty" json:"required,omitempty"`
	SourceType string   `yaml:"source_type" json:"source_type"`
	Source     string   `yaml:"source,omitempty" json:"source,omitempty"`
	Templates  []string `yaml:"templates,omitempty" json:"templates,omitempty"`
	Order      int      `yaml:"order,omitempty" json:"order,omitempty"`
	MaxChars   int      `yaml:"max_chars,omitempty" json:"max_chars,omitempty"`
}
