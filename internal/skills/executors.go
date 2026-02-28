package skills

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"text/template"
	"time"

	"github.com/kayz/coco/internal/security"
)

// ShellExecutor executes shell commands
type ShellExecutor struct {
	Timeout time.Duration
	Shell   string
}

// NewShellExecutor creates a new shell executor
func NewShellExecutor() *ShellExecutor {
	return &ShellExecutor{
		Timeout: 30 * time.Second,
		Shell:   "/bin/sh",
	}
}

// Execute runs a shell command
func (e *ShellExecutor) Execute(ctx ExecutionContext, action Action) ExecutionResult {
	command, ok := action.Config["command"].(string)
	if !ok || command == "" {
		return ExecutionResult{
			Success: false,
			Error:   fmt.Errorf("shell action requires 'command' config"),
		}
	}

	// Template substitution
	command = substituteVariables(command, ctx)

	// Safety check
	if containsDangerousCommand(command) {
		return ExecutionResult{
			Success: false,
			Error:   fmt.Errorf("command blocked for safety"),
		}
	}

	timeout := e.Timeout
	if t, ok := action.Config["timeout"].(float64); ok {
		timeout = time.Duration(t) * time.Second
	}

	execCtx, cancel := context.WithTimeout(ctx.Context, timeout)
	defer cancel()

	cmd := exec.CommandContext(execCtx, e.Shell, "-c", command)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Set working directory if specified
	if dir, ok := action.Config["dir"].(string); ok {
		cmd.Dir = os.ExpandEnv(dir)
	}

	err := cmd.Run()

	output := stdout.String()
	if stderr.Len() > 0 {
		output += "\nstderr: " + stderr.String()
	}

	if err != nil {
		return ExecutionResult{
			Success:  false,
			Output:   output,
			Error:    err,
			Continue: action.Config["continue_on_error"] == true,
		}
	}

	return ExecutionResult{
		Success: true,
		Output:  strings.TrimSpace(output),
	}
}

// HTTPExecutor makes HTTP requests
type HTTPExecutor struct {
	Client  *http.Client
	Timeout time.Duration
}

// NewHTTPExecutor creates a new HTTP executor
func NewHTTPExecutor() *HTTPExecutor {
	return &HTTPExecutor{
		Client:  http.DefaultClient,
		Timeout: 30 * time.Second,
	}
}

// Execute makes an HTTP request
func (e *HTTPExecutor) Execute(ctx ExecutionContext, action Action) ExecutionResult {
	url, ok := action.Config["url"].(string)
	if !ok || url == "" {
		return ExecutionResult{
			Success: false,
			Error:   fmt.Errorf("http action requires 'url' config"),
		}
	}

	// Template substitution
	url = substituteVariables(url, ctx)

	method := "GET"
	if m, ok := action.Config["method"].(string); ok {
		method = strings.ToUpper(m)
	}

	timeout := e.Timeout
	if t, ok := action.Config["timeout"].(float64); ok {
		timeout = time.Duration(t) * time.Second
	}

	execCtx, cancel := context.WithTimeout(ctx.Context, timeout)
	defer cancel()

	var body io.Reader
	if b, ok := action.Config["body"].(string); ok {
		b = substituteVariables(b, ctx)
		body = strings.NewReader(b)
	}

	req, err := http.NewRequestWithContext(execCtx, method, url, body)
	if err != nil {
		return ExecutionResult{
			Success: false,
			Error:   fmt.Errorf("failed to create request: %w", err),
		}
	}

	// Set headers
	if headers, ok := action.Config["headers"].(map[string]any); ok {
		for key, val := range headers {
			if v, ok := val.(string); ok {
				req.Header.Set(key, substituteVariables(v, ctx))
			}
		}
	}

	// Set content type for POST/PUT
	if body != nil && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := e.Client.Do(req)
	if err != nil {
		return ExecutionResult{
			Success:  false,
			Error:    err,
			Continue: action.Config["continue_on_error"] == true,
		}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return ExecutionResult{
			Success: false,
			Error:   fmt.Errorf("failed to read response: %w", err),
		}
	}

	success := resp.StatusCode >= 200 && resp.StatusCode < 300
	return ExecutionResult{
		Success:  success,
		Output:   string(respBody),
		Continue: action.Config["continue_on_error"] == true,
	}
}

// PromptExecutor sends prompts to AI (placeholder for integration)
type PromptExecutor struct {
	Handler func(ctx context.Context, prompt string) (string, error)
}

// NewPromptExecutor creates a new prompt executor
func NewPromptExecutor(handler func(ctx context.Context, prompt string) (string, error)) *PromptExecutor {
	return &PromptExecutor{Handler: handler}
}

// Execute sends a prompt
func (e *PromptExecutor) Execute(ctx ExecutionContext, action Action) ExecutionResult {
	prompt, ok := action.Config["prompt"].(string)
	if !ok || prompt == "" {
		return ExecutionResult{
			Success: false,
			Error:   fmt.Errorf("prompt action requires 'prompt' config"),
		}
	}

	// Template substitution
	prompt = substituteVariables(prompt, ctx)

	if e.Handler == nil {
		return ExecutionResult{
			Success: false,
			Error:   fmt.Errorf("no prompt handler configured"),
		}
	}

	result, err := e.Handler(ctx.Context, prompt)
	if err != nil {
		return ExecutionResult{
			Success:  false,
			Error:    err,
			Continue: action.Config["continue_on_error"] == true,
		}
	}

	return ExecutionResult{
		Success: true,
		Output:  result,
	}
}

// WorkflowExecutor executes multi-step workflows
type WorkflowExecutor struct {
	Registry *Registry
}

// NewWorkflowExecutor creates a new workflow executor
func NewWorkflowExecutor(registry *Registry) *WorkflowExecutor {
	return &WorkflowExecutor{Registry: registry}
}

// Execute runs a workflow
func (e *WorkflowExecutor) Execute(ctx ExecutionContext, action Action) ExecutionResult {
	steps, ok := action.Config["steps"].([]any)
	if !ok || len(steps) == 0 {
		return ExecutionResult{
			Success: false,
			Error:   fmt.Errorf("workflow action requires 'steps' config"),
		}
	}

	var outputs []string
	variables := make(map[string]string)
	if ctx.Variables != nil {
		for k, v := range ctx.Variables {
			variables[k] = v
		}
	}

	for i, step := range steps {
		stepMap, ok := step.(map[string]any)
		if !ok {
			continue
		}

		// Parse step as Action
		stepData, _ := json.Marshal(stepMap)
		var stepAction Action
		if err := json.Unmarshal(stepData, &stepAction); err != nil {
			return ExecutionResult{
				Success: false,
				Error:   fmt.Errorf("failed to parse step %d: %w", i, err),
			}
		}

		// Execute step
		stepCtx := ctx
		stepCtx.Variables = variables

		results := e.Registry.Execute(stepCtx, &Skill{
			Actions: []Action{stepAction},
		})

		if len(results) > 0 {
			result := results[0]
			if !result.Success && !result.Continue {
				return ExecutionResult{
					Success: false,
					Error:   fmt.Errorf("step %d failed: %w", i, result.Error),
				}
			}
			if result.Output != "" {
				outputs = append(outputs, result.Output)
				variables[fmt.Sprintf("step%d", i)] = result.Output
			}
		}
	}

	return ExecutionResult{
		Success: true,
		Output:  strings.Join(outputs, "\n"),
	}
}

// Helper functions

func substituteVariables(text string, ctx ExecutionContext) string {
	// Replace context variables
	text = strings.ReplaceAll(text, "{{.Message}}", ctx.Message)
	text = strings.ReplaceAll(text, "{{.SessionID}}", ctx.SessionID)
	text = strings.ReplaceAll(text, "{{.UserID}}", ctx.UserID)
	text = strings.ReplaceAll(text, "{{.Platform}}", ctx.Platform)

	// Replace match groups
	for i, match := range ctx.Matches {
		text = strings.ReplaceAll(text, fmt.Sprintf("{{.Match%d}}", i), match)
	}

	// Replace custom variables
	for key, val := range ctx.Variables {
		text = strings.ReplaceAll(text, fmt.Sprintf("{{.%s}}", key), val)
	}

	// Environment variables
	text = os.ExpandEnv(text)

	// Use text/template for more complex substitutions
	tmpl, err := template.New("cmd").Parse(text)
	if err == nil {
		var buf bytes.Buffer
		tmpl.Execute(&buf, map[string]any{
			"Message":   ctx.Message,
			"SessionID": ctx.SessionID,
			"UserID":    ctx.UserID,
			"Platform":  ctx.Platform,
			"Matches":   ctx.Matches,
			"Variables": ctx.Variables,
		})
		if buf.Len() > 0 {
			text = buf.String()
		}
	}

	return text
}

func containsDangerousCommand(cmd string) bool {
	_, blocked := security.MatchCommandPattern(cmd, security.DefaultBlockedCommandPatterns)
	return blocked
}
