package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mark3labs/mcp-go/mcp"
)

// FileRead reads the contents of a file
func FileRead(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path, ok := req.Params.Arguments["path"].(string)
	if !ok {
		return mcp.NewToolResultError("path is required"), nil
	}

	// Expand ~ to executable directory
	path = ExpandTilde(path)

	// Make path absolute
	absPath, err := filepath.Abs(path)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("invalid path: %v", err)), nil
	}

	content, err := os.ReadFile(absPath)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to read file: %v", err)), nil
	}

	return mcp.NewToolResultText(string(content)), nil
}

// FileWrite writes content to a file
func FileWrite(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path, ok := req.Params.Arguments["path"].(string)
	if !ok {
		return mcp.NewToolResultError("path is required"), nil
	}

	content, ok := req.Params.Arguments["content"].(string)
	if !ok {
		return mcp.NewToolResultError("content is required"), nil
	}

	// Expand ~ to executable directory
	path = ExpandTilde(path)

	// Make path absolute
	absPath, err := filepath.Abs(path)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("invalid path: %v", err)), nil
	}

	// Ensure parent directory exists
	dir := filepath.Dir(absPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to create directory: %v", err)), nil
	}

	if err := os.WriteFile(absPath, []byte(content), 0644); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to write file: %v", err)), nil
	}

	return mcp.NewToolResultText(fmt.Sprintf("Successfully wrote %d bytes to %s", len(content), absPath)), nil
}

// FileList lists contents of a directory
func FileList(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path, ok := req.Params.Arguments["path"].(string)
	if !ok {
		path = "."
	}

	// Expand ~ to executable directory
	path = ExpandTilde(path)

	// Make path absolute
	absPath, err := filepath.Abs(path)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("invalid path: %v", err)), nil
	}

	entries, err := os.ReadDir(absPath)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to read directory: %v", err)), nil
	}

	var result string
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}

		typeChar := "-"
		if entry.IsDir() {
			typeChar = "d"
		} else if info.Mode()&os.ModeSymlink != 0 {
			typeChar = "l"
		}

		result += fmt.Sprintf("%s %10d %s %s\n",
			typeChar,
			info.Size(),
			info.ModTime().Format("Jan 02 15:04"),
			entry.Name(),
		)
	}

	return mcp.NewToolResultText(result), nil
}

// FileSearch searches for files matching a pattern
func FileSearch(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	pattern, ok := req.Params.Arguments["pattern"].(string)
	if !ok {
		return mcp.NewToolResultError("pattern is required"), nil
	}

	path, ok := req.Params.Arguments["path"].(string)
	if !ok {
		path = "."
	}

	// Expand ~ to executable directory
	path = ExpandTilde(path)

	// Make path absolute
	absPath, err := filepath.Abs(path)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("invalid path: %v", err)), nil
	}

	var matches []string
	err = filepath.Walk(absPath, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip errors
		}

		matched, err := filepath.Match(pattern, info.Name())
		if err != nil {
			return nil
		}

		if matched {
			matches = append(matches, p)
		}

		// Limit results
		if len(matches) >= 100 {
			return filepath.SkipAll
		}

		return nil
	})

	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("search failed: %v", err)), nil
	}

	var result string
	for _, m := range matches {
		result += m + "\n"
	}

	if result == "" {
		result = "No matches found"
	}

	return mcp.NewToolResultText(result), nil
}

// FileInfo gets detailed information about a file
func FileInfo(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path, ok := req.Params.Arguments["path"].(string)
	if !ok {
		return mcp.NewToolResultError("path is required"), nil
	}

	// Expand ~ to executable directory
	path = ExpandTilde(path)

	// Make path absolute
	absPath, err := filepath.Abs(path)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("invalid path: %v", err)), nil
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to stat file: %v", err)), nil
	}

	fileType := "file"
	if info.IsDir() {
		fileType = "directory"
	} else if info.Mode()&os.ModeSymlink != 0 {
		fileType = "symlink"
	}

	result := fmt.Sprintf(`Path: %s
Type: %s
Size: %d bytes
Mode: %s
Modified: %s
`, absPath, fileType, info.Size(), info.Mode().String(), info.ModTime().Format("2006-01-02 15:04:05"))

	return mcp.NewToolResultText(result), nil
}
