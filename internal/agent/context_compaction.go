package agent

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

const (
	defaultCompactThresholdChars = 24000
	defaultCompactKeepRecentMsgs = 18
	maxCompactSummaryChars       = 5000
)

func contextCompactionSettings() (thresholdChars int, keepRecent int) {
	thresholdChars = defaultCompactThresholdChars
	keepRecent = defaultCompactKeepRecentMsgs

	if v := strings.TrimSpace(os.Getenv("COCO_CONTEXT_COMPACT_THRESHOLD_CHARS")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			thresholdChars = n
		}
	}
	if v := strings.TrimSpace(os.Getenv("COCO_CONTEXT_COMPACT_KEEP_RECENT")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			keepRecent = n
		}
	}
	return thresholdChars, keepRecent
}

func compactHistoryForPrompt(history []Message, thresholdChars, keepRecent int) ([]Message, bool) {
	if len(history) == 0 {
		return history, false
	}
	if keepRecent <= 0 {
		keepRecent = defaultCompactKeepRecentMsgs
	}
	if thresholdChars <= 0 {
		thresholdChars = defaultCompactThresholdChars
	}
	if len(history) <= keepRecent+2 {
		return history, false
	}

	total := 0
	for _, m := range history {
		total += len(m.Content)
		if m.ToolResult != nil {
			total += len(m.ToolResult.Content)
		}
	}
	if total <= thresholdChars {
		return history, false
	}

	cutoff := len(history) - keepRecent
	if cutoff < 1 {
		cutoff = 1
	}
	old := history[:cutoff]
	recent := history[cutoff:]

	summary := summarizeHistoryMessages(old, maxCompactSummaryChars)
	compacted := make([]Message, 0, 1+len(recent))
	compacted = append(compacted, Message{
		Role:    "assistant",
		Content: summary,
	})
	compacted = append(compacted, recent...)
	return compacted, true
}

func summarizeHistoryMessages(messages []Message, maxChars int) string {
	if maxChars <= 0 {
		maxChars = maxCompactSummaryChars
	}
	var sb strings.Builder
	sb.WriteString("## Conversation Summary\n")
	sb.WriteString("Older turns were compacted to preserve context budget.\n")

	for _, m := range messages {
		content := strings.TrimSpace(m.Content)
		if content == "" && m.ToolResult != nil {
			content = strings.TrimSpace(m.ToolResult.Content)
		}
		if content == "" {
			continue
		}
		if len(content) > 220 {
			content = content[:220] + "..."
		}
		role := strings.TrimSpace(m.Role)
		if role == "" {
			role = "unknown"
		}
		sb.WriteString(fmt.Sprintf("- %s: %s\n", role, content))
		if sb.Len() >= maxChars {
			break
		}
	}

	out := sb.String()
	if len(out) > maxChars {
		out = out[:maxChars]
	}
	return strings.TrimSpace(out)
}
