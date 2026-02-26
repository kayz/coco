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
	req = b.applyDefaults(req)

	var sections []section
	includeHeaders := true
	if req.IncludeSectionHeaders != nil {
		includeHeaders = *req.IncludeSectionHeaders
	}

	systemText := b.readTemplateGroup(req.System)
	taskText := b.readTemplateGroup(req.Task)
	formatText := b.readTemplateGroup(req.Format)
	styleText := b.readTemplateGroup(req.Style)
	referenceText := b.readReferences(req.References)
	historyText, err := b.buildHistory(req)
	if err != nil {
		return "", err
	}

	sections = b.appendSection(sections, "System", systemText, includeHeaders)
	sections = b.appendSection(sections, "Task", taskText, includeHeaders)
	sections = b.appendSection(sections, "Requirements", strings.TrimSpace(req.Requirements), includeHeaders)
	sections = b.appendSection(sections, "Format", formatText, includeHeaders)
	sections = b.appendSection(sections, "Style", styleText, includeHeaders)
	sections = b.appendSection(sections, "References", referenceText, includeHeaders)
	sections = b.appendSection(sections, "Chat History", historyText, includeHeaders)
	sections = b.appendSection(sections, "User Input", strings.TrimSpace(req.UserInput), includeHeaders)

	return renderSections(sections), nil
}

type section struct {
	title   string
	content string
}

func (b *Builder) appendSection(list []section, title, content string, includeHeader bool) []section {
	if strings.TrimSpace(content) == "" {
		return list
	}
	if !includeHeader {
		title = ""
	}
	return append(list, section{title: title, content: content})
}

func renderSections(sections []section) string {
	var out strings.Builder
	for i, s := range sections {
		if i > 0 {
			out.WriteString("\n\n")
		}
		if s.title != "" {
			out.WriteString("### ")
			out.WriteString(s.title)
			out.WriteString("\n\n")
		}
		out.WriteString(s.content)
	}
	return out.String()
}

func (b *Builder) applyDefaults(req BuildRequest) BuildRequest {
	if req.MaxHistory <= 0 {
		req.MaxHistory = 200
	}
	if req.IncludeSectionHeaders == nil {
		// default should be true
		defaultValue := true
		req.IncludeSectionHeaders = &defaultValue
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
