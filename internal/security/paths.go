package security

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var (
	exeDirCache string
)

// PathChecker validates file paths against an allowed list.
// An empty allowed list means no restrictions.
type PathChecker struct {
	allowedPaths []string // resolved absolute paths
}

// getExecutableDir returns the directory where the executable is located
func getExecutableDir() string {
	if exeDirCache != "" {
		return exeDirCache
	}
	execPath, err := os.Executable()
	if err != nil {
		exeDirCache = "."
		return exeDirCache
	}
	execPath, err = filepath.EvalSymlinks(execPath)
	if err != nil {
		exeDirCache = "."
		return exeDirCache
	}
	exeDirCache = filepath.Dir(execPath)
	return exeDirCache
}

// expandTilde expands ~ to the executable directory instead of user home
func expandTilde(path string) string {
	if len(path) > 0 && path[0] == '~' {
		exeDir := getExecutableDir()
		if len(path) == 1 {
			return exeDir
		}
		return filepath.Join(exeDir, path[1:])
	}
	return path
}

// NewPathChecker creates a PathChecker from a list of allowed paths.
// Paths are expanded (~) and resolved to absolute paths.
func NewPathChecker(allowedPaths []string) *PathChecker {
	resolved := make([]string, 0, len(allowedPaths))
	for _, p := range allowedPaths {
		p = expandTilde(p)
		abs, err := filepath.Abs(p)
		if err != nil {
			continue
		}
		resolved = append(resolved, filepath.Clean(abs))
	}
	return &PathChecker{allowedPaths: resolved}
}

// IsAllowed returns true if the path is under any allowed path.
// Returns true if no restrictions are configured.
func (pc *PathChecker) IsAllowed(path string) bool {
	if len(pc.allowedPaths) == 0 {
		return true
	}
	path = expandTilde(path)
	abs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	abs = filepath.Clean(abs)
	for _, allowed := range pc.allowedPaths {
		if abs == allowed || strings.HasPrefix(abs, allowed+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// CheckPath returns an error if the path is not allowed.
func (pc *PathChecker) CheckPath(path string) error {
	if pc.IsAllowed(path) {
		return nil
	}
	return fmt.Errorf("ACCESS DENIED: path %q is outside the allowed directories %v. Do NOT retry this operation. Inform the user that access to this path is restricted by security policy.", path, pc.allowedPaths)
}

// HasRestrictions returns true if path restrictions are configured.
func (pc *PathChecker) HasRestrictions() bool {
	return len(pc.allowedPaths) > 0
}

// AllowedPaths returns the resolved allowed paths.
func (pc *PathChecker) AllowedPaths() []string {
	return pc.allowedPaths
}
