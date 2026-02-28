package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kayz/coco/internal/security"
)

type SecurityLevel string

const (
	SecuritySafe      SecurityLevel = "safe"
	SecurityWarning   SecurityLevel = "warning"
	SecurityDangerous SecurityLevel = "dangerous"
)

type SecurityAssessment struct {
	Level   SecurityLevel `json:"level"`
	Score   int           `json:"score"`
	Reasons []string      `json:"reasons"`
}

type InstallOptions struct {
	ManagedDir string
	Overwrite  bool
}

type InstallResult struct {
	InstalledPath string             `json:"installed_path"`
	AlreadyExists bool               `json:"already_exists"`
	Assessment    SecurityAssessment `json:"assessment"`
}

// EvaluateSkillSecurity scores a skill for install-time risk based on metadata and body content.
func EvaluateSkillSecurity(skill SkillEntry) SecurityAssessment {
	content := strings.ToLower(skill.Content)
	score := 0
	reasons := make(map[string]struct{})

	for _, pattern := range security.DefaultBlockedCommandPatterns {
		if strings.Contains(content, strings.ToLower(pattern)) {
			score += 80
			reasons[fmt.Sprintf("contains blocked command pattern: %s", pattern)] = struct{}{}
		}
	}

	dangerousSnippets := []string{
		"curl | sh",
		"curl|sh",
		"wget | sh",
		"powershell -enc",
		"invoke-expression",
		"sudo rm -rf",
	}
	for _, snippet := range dangerousSnippets {
		if strings.Contains(content, snippet) {
			score += 60
			reasons[fmt.Sprintf("contains high-risk snippet: %s", snippet)] = struct{}{}
		}
	}

	if strings.Contains(content, "http://") {
		score += 20
		reasons["contains insecure http download endpoint"] = struct{}{}
	}
	if strings.Contains(content, "curl ") || strings.Contains(content, "wget ") {
		score += 12
		reasons["contains network download command"] = struct{}{}
	}
	if strings.Contains(content, "bash ") || strings.Contains(content, "sh ") || strings.Contains(content, "powershell") {
		score += 10
		reasons["contains shell execution command"] = struct{}{}
	}

	if len(skill.Metadata.Install) > 0 {
		score += 10
		reasons["declares install actions in metadata"] = struct{}{}
	}
	if len(skill.Metadata.Requires.Env) > 0 {
		score += 5
		reasons["requires environment variables"] = struct{}{}
	}

	level := SecuritySafe
	switch {
	case score >= 70:
		level = SecurityDangerous
	case score >= 30:
		level = SecurityWarning
	}

	if len(reasons) == 0 {
		reasons["no high-risk patterns detected"] = struct{}{}
	}

	reasonList := make([]string, 0, len(reasons))
	for reason := range reasons {
		reasonList = append(reasonList, reason)
	}
	sort.Strings(reasonList)

	return SecurityAssessment{
		Level:   level,
		Score:   score,
		Reasons: reasonList,
	}
}

// FindSkillByName discovers skills and returns one by exact name.
func FindSkillByName(name string, disabledList []string, extraDirs []string) (SkillEntry, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return SkillEntry{}, false
	}
	entries := DiscoverSkills(disabledList, extraDirs)
	for _, entry := range entries {
		if entry.Name == name {
			return entry, true
		}
	}
	return SkillEntry{}, false
}

// InstallSkillEntry copies one discovered skill into the managed skills directory.
func InstallSkillEntry(entry SkillEntry, opts InstallOptions) (InstallResult, error) {
	if strings.TrimSpace(entry.Name) == "" {
		return InstallResult{}, fmt.Errorf("skill name is required")
	}
	if strings.TrimSpace(entry.BaseDir) == "" {
		return InstallResult{}, fmt.Errorf("skill %s has empty base dir", entry.Name)
	}

	managedDir := strings.TrimSpace(opts.ManagedDir)
	if managedDir == "" {
		managedDir = managedSkillsDir()
	}
	if err := os.MkdirAll(managedDir, 0755); err != nil {
		return InstallResult{}, fmt.Errorf("create managed dir: %w", err)
	}

	src := filepath.Clean(entry.BaseDir)
	dst := filepath.Clean(filepath.Join(managedDir, entry.Name))
	assessment := EvaluateSkillSecurity(entry)

	if src == dst {
		return InstallResult{
			InstalledPath: dst,
			AlreadyExists: true,
			Assessment:    assessment,
		}, nil
	}

	if _, err := os.Stat(filepath.Join(src, "SKILL.md")); err != nil {
		return InstallResult{}, fmt.Errorf("invalid skill directory %s: missing SKILL.md", src)
	}

	if _, err := os.Stat(dst); err == nil {
		if !opts.Overwrite {
			return InstallResult{
				InstalledPath: dst,
				AlreadyExists: true,
				Assessment:    assessment,
			}, nil
		}
		if err := os.RemoveAll(dst); err != nil {
			return InstallResult{}, fmt.Errorf("remove existing skill directory: %w", err)
		}
	}

	if err := copyDir(src, dst); err != nil {
		return InstallResult{}, err
	}

	return InstallResult{
		InstalledPath: dst,
		Assessment:    assessment,
	}, nil
}

func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)

		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0644)
	})
}
