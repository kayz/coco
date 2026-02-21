package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

// FileListOld lists files that haven't been modified for a specified duration
func FileListOld(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path, ok := req.Params.Arguments["path"].(string)
	if !ok || path == "" {
		return mcp.NewToolResultError("path is required"), nil
	}

	// Default: 30 days
	days := 30
	if d, ok := req.Params.Arguments["days"].(float64); ok && d > 0 {
		days = int(d)
	}

	// Expand home directory
	if strings.HasPrefix(path, "~") {
		path = ExpandTilde(path)
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("invalid path: %v", err)), nil
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("path not found: %v", err)), nil
	}

	cutoff := time.Now().AddDate(0, 0, -days)
	var oldFiles []fileInfo

	if info.IsDir() {
		// List files in directory
		entries, err := os.ReadDir(absPath)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to read directory: %v", err)), nil
		}

		for _, entry := range entries {
			info, err := entry.Info()
			if err != nil {
				continue
			}

			if info.ModTime().Before(cutoff) {
				oldFiles = append(oldFiles, fileInfo{
					path:    filepath.Join(absPath, entry.Name()),
					name:    entry.Name(),
					size:    info.Size(),
					modTime: info.ModTime(),
					isDir:   entry.IsDir(),
				})
			}
		}
	} else {
		// Single file
		if info.ModTime().Before(cutoff) {
			oldFiles = append(oldFiles, fileInfo{
				path:    absPath,
				name:    info.Name(),
				size:    info.Size(),
				modTime: info.ModTime(),
				isDir:   false,
			})
		}
	}

	if len(oldFiles) == 0 {
		return mcp.NewToolResultText(fmt.Sprintf("No files older than %d days found in %s", days, absPath)), nil
	}

	// Sort by modification time (oldest first)
	sort.Slice(oldFiles, func(i, j int) bool {
		return oldFiles[i].modTime.Before(oldFiles[j].modTime)
	})

	var result strings.Builder
	result.WriteString(fmt.Sprintf("Files not modified for %d+ days in %s:\n\n", days, absPath))

	var totalSize int64
	for _, f := range oldFiles {
		typeStr := "file"
		if f.isDir {
			typeStr = "dir "
		}
		result.WriteString(fmt.Sprintf("[%s] %s | %s | %s\n",
			typeStr,
			f.modTime.Format("2006-01-02"),
			FormatBytes(uint64(f.size)),
			f.name,
		))
		totalSize += f.size
	}

	result.WriteString(fmt.Sprintf("\nTotal: %d items, %s", len(oldFiles), FormatBytes(uint64(totalSize))))

	return mcp.NewToolResultText(result.String()), nil
}

// FileDeleteOld deletes files that haven't been modified for a specified duration
func FileDeleteOld(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path, ok := req.Params.Arguments["path"].(string)
	if !ok || path == "" {
		return mcp.NewToolResultError("path is required"), nil
	}

	// Default: 30 days
	days := 30
	if d, ok := req.Params.Arguments["days"].(float64); ok && d > 0 {
		days = int(d)
	}

	// Whether to delete directories too
	includeDirs := false
	if id, ok := req.Params.Arguments["include_dirs"].(bool); ok {
		includeDirs = id
	}

	// Dry run mode (just show what would be deleted)
	dryRun := false
	if dr, ok := req.Params.Arguments["dry_run"].(bool); ok {
		dryRun = dr
	}

	// Expand home directory
	if strings.HasPrefix(path, "~") {
		path = ExpandTilde(path)
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("invalid path: %v", err)), nil
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("path not found: %v", err)), nil
	}

	if !info.IsDir() {
		return mcp.NewToolResultError("path must be a directory"), nil
	}

	cutoff := time.Now().AddDate(0, 0, -days)
	entries, err := os.ReadDir(absPath)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to read directory: %v", err)), nil
	}

	var deleted []string
	var failed []string
	var totalSize int64

	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}

		if info.ModTime().Before(cutoff) {
			if entry.IsDir() && !includeDirs {
				continue
			}

			filePath := filepath.Join(absPath, entry.Name())
			totalSize += info.Size()

			if dryRun {
				deleted = append(deleted, entry.Name())
			} else {
				var err error
				if entry.IsDir() {
					err = os.RemoveAll(filePath)
				} else {
					err = os.Remove(filePath)
				}

				if err != nil {
					failed = append(failed, fmt.Sprintf("%s: %v", entry.Name(), err))
				} else {
					deleted = append(deleted, entry.Name())
				}
			}
		}
	}

	var result strings.Builder
	if dryRun {
		result.WriteString(fmt.Sprintf("[DRY RUN] Would delete %d items (%s) from %s:\n\n",
			len(deleted), FormatBytes(uint64(totalSize)), absPath))
	} else {
		result.WriteString(fmt.Sprintf("Deleted %d items (%s) from %s:\n\n",
			len(deleted), FormatBytes(uint64(totalSize)), absPath))
	}

	for _, name := range deleted {
		result.WriteString(fmt.Sprintf("  - %s\n", name))
	}

	if len(failed) > 0 {
		result.WriteString(fmt.Sprintf("\nFailed to delete %d items:\n", len(failed)))
		for _, f := range failed {
			result.WriteString(fmt.Sprintf("  - %s\n", f))
		}
	}

	if len(deleted) == 0 && len(failed) == 0 {
		return mcp.NewToolResultText(fmt.Sprintf("No files older than %d days found in %s", days, absPath)), nil
	}

	return mcp.NewToolResultText(result.String()), nil
}

// FileDeleteList deletes specific files by their paths
func FileDeleteList(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	filesArg, ok := req.Params.Arguments["files"]
	if !ok {
		return mcp.NewToolResultError("files is required (array of file paths)"), nil
	}

	// Parse files array
	var files []string
	switch v := filesArg.(type) {
	case []interface{}:
		for _, f := range v {
			if s, ok := f.(string); ok {
				files = append(files, s)
			}
		}
	case string:
		// Single file as string
		files = append(files, v)
	default:
		return mcp.NewToolResultError("files must be an array of file paths"), nil
	}

	if len(files) == 0 {
		return mcp.NewToolResultError("no files specified"), nil
	}

	var deleted []string
	var failed []string

	for _, file := range files {
		path := file
		// Expand home directory
		if strings.HasPrefix(path, "~") {
			home, _ := os.UserHomeDir()
			path = filepath.Join(home, path[1:])
		}

		absPath, err := filepath.Abs(path)
		if err != nil {
			failed = append(failed, fmt.Sprintf("%s: invalid path", file))
			continue
		}

		info, err := os.Stat(absPath)
		if err != nil {
			failed = append(failed, fmt.Sprintf("%s: not found", file))
			continue
		}

		if info.IsDir() {
			err = os.RemoveAll(absPath)
		} else {
			err = os.Remove(absPath)
		}

		if err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", file, err))
		} else {
			deleted = append(deleted, file)
		}
	}

	var result strings.Builder
	result.WriteString(fmt.Sprintf("Deleted %d of %d items:\n\n", len(deleted), len(files)))

	for _, name := range deleted {
		result.WriteString(fmt.Sprintf("  - %s\n", name))
	}

	if len(failed) > 0 {
		result.WriteString(fmt.Sprintf("\nFailed to delete %d items:\n", len(failed)))
		for _, f := range failed {
			result.WriteString(fmt.Sprintf("  - %s\n", f))
		}
	}

	return mcp.NewToolResultText(result.String()), nil
}

// FileMoveToTrash moves files to trash instead of permanently deleting
func FileMoveToTrash(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	filesArg, ok := req.Params.Arguments["files"]
	if !ok {
		return mcp.NewToolResultError("files is required (array of file paths)"), nil
	}

	// Parse files array
	var files []string
	switch v := filesArg.(type) {
	case []interface{}:
		for _, f := range v {
			if s, ok := f.(string); ok {
				files = append(files, s)
			}
		}
	case string:
		files = append(files, v)
	default:
		return mcp.NewToolResultError("files must be an array of file paths"), nil
	}

	if len(files) == 0 {
		return mcp.NewToolResultError("no files specified"), nil
	}

	var trashed []string
	var failed []string

	for _, file := range files {
		path := file
		if strings.HasPrefix(path, "~") {
			home, _ := os.UserHomeDir()
			path = filepath.Join(home, path[1:])
		}

		absPath, err := filepath.Abs(path)
		if err != nil {
			failed = append(failed, fmt.Sprintf("%s: invalid path", file))
			continue
		}

		// Use AppleScript to move to trash on macOS
		script := fmt.Sprintf(`
			tell application "Finder"
				delete POSIX file "%s"
			end tell
		`, absPath)

		cmd := exec.CommandContext(ctx, "osascript", "-e", script)
		if err := cmd.Run(); err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", file, err))
		} else {
			trashed = append(trashed, file)
		}
	}

	var result strings.Builder
	result.WriteString(fmt.Sprintf("Moved %d of %d items to Trash:\n\n", len(trashed), len(files)))

	for _, name := range trashed {
		result.WriteString(fmt.Sprintf("  - %s\n", name))
	}

	if len(failed) > 0 {
		result.WriteString(fmt.Sprintf("\nFailed to trash %d items:\n", len(failed)))
		for _, f := range failed {
			result.WriteString(fmt.Sprintf("  - %s\n", f))
		}
	}

	return mcp.NewToolResultText(result.String()), nil
}

type fileInfo struct {
	path    string
	name    string
	size    int64
	modTime time.Time
	isDir   bool
}
