package tools

import (
	"fmt"
	"os"
	"path/filepath"
)

var (
	exeDirCache string
)

// GetExecutableDir returns the directory where the executable is located
func GetExecutableDir() string {
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

// ExpandTilde expands ~ to the executable directory instead of user home
func ExpandTilde(path string) string {
	if len(path) > 0 && path[0] == '~' {
		exeDir := GetExecutableDir()
		if len(path) == 1 {
			return exeDir
		}
		return filepath.Join(exeDir, path[1:])
	}
	return path
}

// FormatBytes formats bytes to human readable format
func FormatBytes(bytes uint64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := uint64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
