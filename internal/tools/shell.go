package tools

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/kayz/coco/internal/config"
	"github.com/kayz/coco/internal/security"
	"github.com/mark3labs/mcp-go/mcp"
)

// ShellExecute executes a shell command
func ShellExecute(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	command, ok := req.Params.Arguments["command"].(string)
	if !ok {
		return mcp.NewToolResultError("command is required"), nil
	}

	cfg, err := config.Load()
	blocked := security.DefaultBlockedCommandPatterns
	requireConfirmation := []string{}
	if err == nil {
		blocked = security.NormalizeCommandPatterns(cfg.Security.BlockedCommands, security.DefaultBlockedCommandPatterns)
		requireConfirmation = security.NormalizeCommandPatterns(cfg.Security.RequireConfirmation, nil)
	}

	if matched, blocked := security.MatchCommandPattern(command, blocked); blocked {
		return mcp.NewToolResultError(fmt.Sprintf("command blocked for safety: contains '%s'", matched)), nil
	}
	if matched, needsConfirm := security.MatchCommandPattern(command, requireConfirmation); needsConfirm {
		return mcp.NewToolResultError(fmt.Sprintf("confirmation required by security policy: contains '%s'", matched)), nil
	}

	// Get timeout (default 30 seconds)
	timeout := 30.0
	if t, ok := req.Params.Arguments["timeout"].(float64); ok && t > 0 {
		timeout = t
	}

	// Get working directory
	workDir := ""
	if wd, ok := req.Params.Arguments["working_directory"].(string); ok {
		workDir = wd
	}

	// Create context with timeout
	execCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	// Execute command
	cmd := exec.CommandContext(execCtx, "sh", "-c", command)
	if workDir != "" {
		cmd.Dir = workDir
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()

	// Build result
	var result strings.Builder
	result.WriteString(fmt.Sprintf("Command: %s\n", command))

	if stdout.Len() > 0 {
		result.WriteString(fmt.Sprintf("\n--- stdout ---\n%s", stdout.String()))
	}

	if stderr.Len() > 0 {
		result.WriteString(fmt.Sprintf("\n--- stderr ---\n%s", stderr.String()))
	}

	if err != nil {
		if execCtx.Err() == context.DeadlineExceeded {
			result.WriteString(fmt.Sprintf("\n--- error ---\nCommand timed out after %.0f seconds", timeout))
		} else {
			result.WriteString(fmt.Sprintf("\n--- error ---\n%v", err))
		}
	} else {
		result.WriteString("\n--- exit code: 0 ---")
	}

	return mcp.NewToolResultText(result.String()), nil
}

// ShellWhich finds the path of an executable
func ShellWhich(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name, ok := req.Params.Arguments["name"].(string)
	if !ok {
		return mcp.NewToolResultError("name is required"), nil
	}

	path, err := exec.LookPath(name)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("executable not found: %s", name)), nil
	}

	return mcp.NewToolResultText(path), nil
}
