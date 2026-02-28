package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

func (a *Agent) executeSpawnAgent(ctx context.Context, args map[string]any) string {
	endpoint, _ := args["endpoint"].(string)
	prompt, _ := args["prompt"].(string)
	authHeader, _ := args["auth"].(string)

	endpoint = strings.TrimSpace(endpoint)
	prompt = strings.TrimSpace(prompt)
	if endpoint == "" {
		return "Error: endpoint is required"
	}
	if prompt == "" {
		return "Error: prompt is required"
	}

	timeout := 60.0
	if v, ok := args["timeout"].(float64); ok && v > 0 {
		timeout = v
	}

	payload := map[string]any{
		"type":      "spawn_agent",
		"source":    "external-agent",
		"prompt":    prompt,
		"platform":  a.currentMsg.Platform,
		"channelID": a.currentMsg.ChannelID,
		"userID":    a.currentMsg.UserID,
		"username":  a.currentMsg.Username,
		"requested": time.Now().Format(time.RFC3339),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Sprintf("Error: failed to encode payload: %v", err)
	}

	reqCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Sprintf("Error: invalid endpoint: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Coco-Source", "external-agent")
	if strings.TrimSpace(authHeader) != "" {
		req.Header.Set("Authorization", strings.TrimSpace(authHeader))
	}

	resp, err := (&http.Client{Timeout: time.Duration(timeout) * time.Second}).Do(req)
	if err != nil {
		return fmt.Sprintf("Error: external agent request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Sprintf("Error: external agent returned status %d", resp.StatusCode)
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 200*1024))
	if err != nil {
		return fmt.Sprintf("Error: failed reading external response: %v", err)
	}
	if len(raw) == 0 {
		return "External agent completed with empty response."
	}

	var result struct {
		Text    string `json:"text"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(raw, &result); err == nil {
		text := strings.TrimSpace(result.Text)
		if text == "" {
			text = strings.TrimSpace(result.Message)
		}
		if text != "" {
			return fmt.Sprintf("[external-agent] %s", text)
		}
	}

	return fmt.Sprintf("[external-agent] %s", strings.TrimSpace(string(raw)))
}
