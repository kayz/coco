package promptbuild

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

func (b *Builder) loadPromptAssemblySpec(req BuildRequest) (*PromptAssemblySpec, error) {
	specPath := strings.TrimSpace(req.SpecPath)
	if specPath == "" {
		agent := strings.TrimSpace(req.Agent)
		if agent == "" {
			return nil, nil
		}
		specPath = filepath.Join("prompts", "specs", agent+".yaml")
	}

	fullPath := b.resolvePath(specPath)
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return nil, fmt.Errorf("read spec file %s: %w", fullPath, err)
	}

	var spec PromptAssemblySpec
	if err := yaml.Unmarshal(data, &spec); err != nil {
		return nil, fmt.Errorf("parse spec file %s: %w", fullPath, err)
	}
	if err := validatePromptAssemblySpec(&spec); err != nil {
		return nil, fmt.Errorf("invalid spec file %s: %w", fullPath, err)
	}

	sort.SliceStable(spec.Sections, func(i, j int) bool {
		return spec.Sections[i].Order < spec.Sections[j].Order
	})

	return &spec, nil
}

func validatePromptAssemblySpec(spec *PromptAssemblySpec) error {
	if spec == nil {
		return fmt.Errorf("spec is nil")
	}
	if strings.TrimSpace(spec.Agent) == "" {
		return fmt.Errorf("agent is required")
	}
	if len(spec.Sections) == 0 {
		return fmt.Errorf("sections is required")
	}

	seenIDs := make(map[string]struct{}, len(spec.Sections))
	for _, sec := range spec.Sections {
		id := strings.TrimSpace(sec.ID)
		if id == "" {
			return fmt.Errorf("section id is required")
		}
		if _, exists := seenIDs[id]; exists {
			return fmt.Errorf("duplicate section id: %s", id)
		}
		seenIDs[id] = struct{}{}

		st := strings.TrimSpace(sec.SourceType)
		if st == "" {
			return fmt.Errorf("section %s source_type is required", id)
		}
		if !isSupportedSourceType(st) {
			return fmt.Errorf("section %s has unsupported source_type: %s", id, st)
		}

		switch st {
		case "templates":
			if len(sec.Templates) == 0 {
				return fmt.Errorf("section %s templates source requires templates", id)
			}
		case "request_field", "inline_text":
			if strings.TrimSpace(sec.Source) == "" {
				return fmt.Errorf("section %s source is required for source_type %s", id, st)
			}
		}
	}

	return nil
}

func isSupportedSourceType(sourceType string) bool {
	switch strings.TrimSpace(sourceType) {
	case "templates", "request_field", "references", "history", "user_input", "inline_text":
		return true
	default:
		return false
	}
}
