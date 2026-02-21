package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

// ScreenshotCapture captures a screenshot
func ScreenshotCapture(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Determine output path
	outputPath := ""
	if p, ok := req.Params.Arguments["path"].(string); ok && p != "" {
		outputPath = p
	} else {
		// Default to executable directory with timestamp
		exeDir := GetExecutableDir()
		timestamp := time.Now().Format("2006-01-02_15-04-05")
		outputPath = filepath.Join(exeDir, fmt.Sprintf("screenshot_%s.png", timestamp))
	}

	// Expand home directory
	if len(outputPath) > 0 && outputPath[0] == '~' {
		outputPath = ExpandTilde(outputPath)
	}

	// Make path absolute
	absPath, err := filepath.Abs(outputPath)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("invalid path: %v", err)), nil
	}

	// Ensure directory exists
	dir := filepath.Dir(absPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to create directory: %v", err)), nil
	}

	// Get screenshot type
	captureType := "fullscreen"
	if t, ok := req.Params.Arguments["type"].(string); ok {
		captureType = t
	}

	switch runtime.GOOS {
	case "darwin":
		return screenshotMacOS(ctx, absPath, captureType)
	case "linux":
		return screenshotLinux(ctx, absPath, captureType)
	case "windows":
		return screenshotWindows(ctx, absPath)
	default:
		return mcp.NewToolResultError(fmt.Sprintf("screenshot not supported on %s", runtime.GOOS)), nil
	}
}

func screenshotMacOS(ctx context.Context, path, captureType string) (*mcp.CallToolResult, error) {
	var cmd *exec.Cmd

	switch captureType {
	case "window":
		// Capture a specific window (interactive)
		cmd = exec.CommandContext(ctx, "screencapture", "-w", path)
	case "selection":
		// Capture a selection (interactive)
		cmd = exec.CommandContext(ctx, "screencapture", "-i", path)
	default:
		// Full screen capture
		cmd = exec.CommandContext(ctx, "screencapture", "-x", path)
	}

	if err := cmd.Run(); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to capture screenshot: %v", err)), nil
	}

	// Check if file was created
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return mcp.NewToolResultText("Screenshot cancelled"), nil
	}

	return mcp.NewToolResultText(fmt.Sprintf("Screenshot saved to: %s", path)), nil
}

func screenshotLinux(ctx context.Context, path, captureType string) (*mcp.CallToolResult, error) {
	// Try gnome-screenshot first, then scrot
	var cmd *exec.Cmd

	// Check if gnome-screenshot is available
	if _, err := exec.LookPath("gnome-screenshot"); err == nil {
		switch captureType {
		case "window":
			cmd = exec.CommandContext(ctx, "gnome-screenshot", "-w", "-f", path)
		case "selection":
			cmd = exec.CommandContext(ctx, "gnome-screenshot", "-a", "-f", path)
		default:
			cmd = exec.CommandContext(ctx, "gnome-screenshot", "-f", path)
		}
	} else if _, err := exec.LookPath("scrot"); err == nil {
		switch captureType {
		case "window":
			cmd = exec.CommandContext(ctx, "scrot", "-u", path)
		case "selection":
			cmd = exec.CommandContext(ctx, "scrot", "-s", path)
		default:
			cmd = exec.CommandContext(ctx, "scrot", path)
		}
	} else {
		return mcp.NewToolResultError("no screenshot tool found (install gnome-screenshot or scrot)"), nil
	}

	if err := cmd.Run(); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to capture screenshot: %v", err)), nil
	}

	return mcp.NewToolResultText(fmt.Sprintf("Screenshot saved to: %s", path)), nil
}

func screenshotWindows(ctx context.Context, path string) (*mcp.CallToolResult, error) {
	// Use PowerShell to capture screenshot
	script := fmt.Sprintf(`
		Add-Type -AssemblyName System.Windows.Forms
		$screen = [System.Windows.Forms.Screen]::PrimaryScreen
		$bitmap = New-Object System.Drawing.Bitmap($screen.Bounds.Width, $screen.Bounds.Height)
		$graphics = [System.Drawing.Graphics]::FromImage($bitmap)
		$graphics.CopyFromScreen($screen.Bounds.Location, [System.Drawing.Point]::Empty, $screen.Bounds.Size)
		$bitmap.Save("%s")
		$graphics.Dispose()
		$bitmap.Dispose()
	`, path)

	cmd := exec.CommandContext(ctx, "powershell", "-command", script)
	if err := cmd.Run(); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to capture screenshot: %v", err)), nil
	}

	return mcp.NewToolResultText(fmt.Sprintf("Screenshot saved to: %s", path)), nil
}
