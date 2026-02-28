package promptbuild

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kayz/coco/internal/config"
	"github.com/kayz/coco/internal/logger"
)

// Builder assembles prompts from templates, references, history and user input.
type Builder struct {
	cfg config.PromptBuildConfig
}

// NewBuilder creates a new Builder from config.
func NewBuilder(cfg config.PromptBuildConfig) *Builder {
	return &Builder{cfg: cfg}
}

// Build assembles a prompt and returns the final text.
func (b *Builder) Build(req BuildRequest) (string, error) {
	requestedHeaders := req.IncludeSectionHeaders != nil
	requestedMaxHistory := req.MaxHistory > 0

	req = b.applyDefaults(req)

	spec, err := b.loadPromptAssemblySpec(req)
	if err != nil {
		return "", err
	}
	if spec != nil {
		if !requestedHeaders && spec.Defaults.IncludeSectionHeaders != nil {
			v := *spec.Defaults.IncludeSectionHeaders
			req.IncludeSectionHeaders = &v
		}
		if !requestedMaxHistory && spec.Defaults.MaxHistory > 0 {
			req.MaxHistory = spec.Defaults.MaxHistory
		}
	}

	includeHeaders := true
	if req.IncludeSectionHeaders != nil {
		includeHeaders = *req.IncludeSectionHeaders
	}

	var sections []section
	if spec == nil {
		sections, err = b.buildLegacySections(req, includeHeaders)
	} else {
		sections, err = b.buildSectionsFromSpec(req, spec, includeHeaders)
	}
	if err != nil {
		return "", err
	}
	if spec != nil && spec.Defaults.MaxPromptChars > 0 {
		sections = trimSectionsToPromptBudget(sections, spec.Defaults.MaxPromptChars)
	}

	finalPrompt := renderSections(sections)
	if err := b.writeAuditRecord(req, finalPrompt, sections); err != nil {
		logger.Warn("Prompt audit write failed: %v", err)
	}

	return finalPrompt, nil
}

func (b *Builder) buildLegacySections(req BuildRequest, includeHeaders bool) ([]section, error) {
	var sections []section

	systemText := b.readTemplateGroup(req.System)
	taskText := b.readTemplateGroup(req.Task)
	formatText := b.readTemplateGroup(req.Format)
	styleText := b.readTemplateGroup(req.Style)
	referenceText := b.readReferences(req.References)
	historyText, err := b.buildHistory(req)
	if err != nil {
		return nil, err
	}

	sections = b.appendSection(sections, "System", systemText, includeHeaders, false)
	sections = b.appendSection(sections, "Task", taskText, includeHeaders, false)
	sections = b.appendSection(sections, "Requirements", strings.TrimSpace(req.Requirements), includeHeaders, false)
	sections = b.appendSection(sections, "Format", formatText, includeHeaders, false)
	sections = b.appendSection(sections, "Style", styleText, includeHeaders, false)
	sections = b.appendSection(sections, "References", referenceText, includeHeaders, false)
	sections = b.appendSection(sections, "Chat History", historyText, includeHeaders, false)
	sections = b.appendSection(sections, "User Input", strings.TrimSpace(req.UserInput), includeHeaders, false)

	return sections, nil
}

func (b *Builder) buildSectionsFromSpec(req BuildRequest, spec *PromptAssemblySpec, includeHeaders bool) ([]section, error) {
	var sections []section
	for _, sec := range spec.Sections {
		title := strings.TrimSpace(sec.Title)
		if title == "" {
			title = strings.TrimSpace(sec.ID)
		}

		content, err := b.resolveSpecSectionContent(req, sec)
		if err != nil {
			return nil, err
		}
		if sec.MaxChars > 0 {
			content = truncateTextByRunes(content, sec.MaxChars)
		}
		if sec.Required && strings.TrimSpace(content) == "" {
			return nil, fmt.Errorf("required section %q is empty", sec.ID)
		}
		sections = b.appendSection(sections, title, content, includeHeaders, sec.Required)
	}
	return sections, nil
}

func (b *Builder) resolveSpecSectionContent(req BuildRequest, sec SectionSpec) (string, error) {
	switch strings.TrimSpace(sec.SourceType) {
	case "templates":
		return b.readTemplateGroup(sec.Templates), nil
	case "request_field":
		return b.resolveRequestField(req, sec.Source), nil
	case "references":
		if strings.TrimSpace(sec.Source) == "" {
			return b.readReferences(req.References), nil
		}
		return b.readReferences(splitCSV(sec.Source)), nil
	case "history":
		return b.buildHistory(req)
	case "user_input":
		return strings.TrimSpace(req.UserInput), nil
	case "inline_text":
		if v, ok := req.Inputs[strings.TrimSpace(sec.Source)]; ok {
			return strings.TrimSpace(v), nil
		}
		return strings.TrimSpace(sec.Source), nil
	default:
		return "", fmt.Errorf("unsupported source_type %q in section %q", sec.SourceType, sec.ID)
	}
}

func (b *Builder) resolveRequestField(req BuildRequest, field string) string {
	key := strings.TrimSpace(field)
	if key == "" {
		return ""
	}
	if v, ok := req.Inputs[key]; ok {
		return strings.TrimSpace(v)
	}

	switch key {
	case "requirements":
		return strings.TrimSpace(req.Requirements)
	case "user_input":
		return strings.TrimSpace(req.UserInput)
	case "agent":
		return strings.TrimSpace(req.Agent)
	case "spec_path":
		return strings.TrimSpace(req.SpecPath)
	default:
		return ""
	}
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

type section struct {
	title         string
	content       string
	includeHeader bool
	required      bool
}

func (b *Builder) appendSection(list []section, title, content string, includeHeader bool, required bool) []section {
	if strings.TrimSpace(content) == "" {
		return list
	}
	return append(list, section{title: title, content: content, includeHeader: includeHeader, required: required})
}

func renderSections(sections []section) string {
	var out strings.Builder
	for i, s := range sections {
		if i > 0 {
			out.WriteString("\n\n")
		}
		if s.includeHeader && s.title != "" {
			out.WriteString("### ")
			out.WriteString(s.title)
			out.WriteString("\n\n")
		}
		out.WriteString(s.content)
	}
	return out.String()
}

func trimSectionsToPromptBudget(sections []section, maxPromptChars int) []section {
	if maxPromptChars <= 0 || len(sections) == 0 {
		return sections
	}

	current := runeCount(renderSections(sections))
	if current <= maxPromptChars {
		return sections
	}

	over := current - maxPromptChars
	for i := len(sections) - 1; i >= 0 && over > 0; i-- {
		if sections[i].required {
			continue
		}
		reduced := reduceSectionRunes(&sections[i], over)
		over -= reduced
	}
	for i := len(sections) - 1; i >= 0 && over > 0; i-- {
		reduced := reduceSectionRunes(&sections[i], over)
		over -= reduced
	}

	out := make([]section, 0, len(sections))
	for _, s := range sections {
		if strings.TrimSpace(s.content) == "" {
			continue
		}
		out = append(out, s)
	}
	return out
}

func reduceSectionRunes(s *section, need int) int {
	if s == nil || need <= 0 {
		return 0
	}
	contentLen := runeCount(s.content)
	if contentLen == 0 {
		return 0
	}
	target := contentLen - need
	if target <= 0 {
		s.content = ""
		return contentLen
	}
	newContent := truncateTextByRunes(s.content, target)
	reduced := contentLen - runeCount(newContent)
	s.content = newContent
	return reduced
}

func truncateTextByRunes(text string, maxRunes int) string {
	text = strings.TrimSpace(text)
	if text == "" || maxRunes <= 0 {
		return ""
	}

	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}

	const suffix = "\n\n...[truncated by prompt budget]"
	suffixRunes := []rune(suffix)
	if maxRunes <= len(suffixRunes)+8 {
		return strings.TrimSpace(string(runes[:maxRunes]))
	}
	keep := maxRunes - len(suffixRunes)
	return strings.TrimSpace(string(runes[:keep])) + suffix
}

func runeCount(s string) int {
	return len([]rune(s))
}

func (b *Builder) applyDefaults(req BuildRequest) BuildRequest {
	if req.IncludeSectionHeaders == nil {
		defaultValue := true
		req.IncludeSectionHeaders = &defaultValue
	}
	if req.MaxHistory <= 0 {
		req.MaxHistory = 200
	}
	if b.cfg.RootDir == "" {
		b.cfg.RootDir = "."
	}
	if b.cfg.TemplatesDir == "" {
		b.cfg.TemplatesDir = "prompts"
	}
	if b.cfg.SQLitePath == "" {
		b.cfg.SQLitePath = ".coco.db"
	}

	defaultAuditEnabled := b.cfg.AuditDir == "" && b.cfg.AuditRetentionDays <= 0 && strings.TrimSpace(b.cfg.AuditFilePrefix) == ""
	if defaultAuditEnabled {
		b.cfg.AuditEnabled = true
	}
	if b.cfg.AuditDir == "" {
		b.cfg.AuditDir = ".coco/promptbuild-audit"
	}
	if b.cfg.AuditRetentionDays <= 0 {
		b.cfg.AuditRetentionDays = 7
	}
	if strings.TrimSpace(b.cfg.AuditFilePrefix) == "" {
		b.cfg.AuditFilePrefix = "promptbuild"
	}
	return req
}

func (b *Builder) readTemplateGroup(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	var parts []string
	for _, p := range paths {
		if strings.TrimSpace(p) == "" {
			continue
		}
		content := b.readTemplate(p)
		if content != "" {
			parts = append(parts, content)
		}
	}
	return strings.Join(parts, "\n\n")
}

func (b *Builder) readTemplate(path string) string {
	fullPath := b.resolveTemplatePath(path)
	content, err := os.ReadFile(fullPath)
	if err != nil {
		logger.Warn("Prompt template not found, skipping: %s", fullPath)
		return ""
	}
	return strings.TrimSpace(string(content))
}

func (b *Builder) readReferences(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	var blocks []string
	for _, p := range paths {
		if strings.TrimSpace(p) == "" {
			continue
		}
		fullPath := b.resolvePath(p)
		content, err := os.ReadFile(fullPath)
		if err != nil {
			logger.Warn("Reference file not found, skipping: %s", fullPath)
			continue
		}
		block := fmt.Sprintf("[REFERENCE:%s]\n%s\n[/REFERENCE]", p, strings.TrimSpace(string(content)))
		blocks = append(blocks, block)
	}
	return strings.Join(blocks, "\n\n")
}

func (b *Builder) resolveTemplatePath(p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(b.cfg.RootDir, b.cfg.TemplatesDir, p)
}

func (b *Builder) resolvePath(p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(b.cfg.RootDir, p)
}
