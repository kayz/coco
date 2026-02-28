package agent

import (
	"os"
	"strings"
	"testing"
)

func TestContextCompactionSettingsDefaultsAndEnv(t *testing.T) {
	_ = os.Unsetenv("COCO_CONTEXT_COMPACT_THRESHOLD_CHARS")
	_ = os.Unsetenv("COCO_CONTEXT_COMPACT_KEEP_RECENT")

	threshold, keepRecent := contextCompactionSettings()
	if threshold != defaultCompactThresholdChars {
		t.Fatalf("unexpected default threshold: %d", threshold)
	}
	if keepRecent != defaultCompactKeepRecentMsgs {
		t.Fatalf("unexpected default keepRecent: %d", keepRecent)
	}

	_ = os.Setenv("COCO_CONTEXT_COMPACT_THRESHOLD_CHARS", "4096")
	_ = os.Setenv("COCO_CONTEXT_COMPACT_KEEP_RECENT", "7")
	defer func() {
		_ = os.Unsetenv("COCO_CONTEXT_COMPACT_THRESHOLD_CHARS")
		_ = os.Unsetenv("COCO_CONTEXT_COMPACT_KEEP_RECENT")
	}()

	threshold, keepRecent = contextCompactionSettings()
	if threshold != 4096 || keepRecent != 7 {
		t.Fatalf("unexpected env settings: threshold=%d keepRecent=%d", threshold, keepRecent)
	}
}

func TestCompactHistoryForPromptTriggeredByThreshold(t *testing.T) {
	history := []Message{
		{Role: "user", Content: strings.Repeat("a", 180)},
		{Role: "assistant", Content: strings.Repeat("b", 170)},
		{Role: "user", Content: strings.Repeat("c", 160)},
		{Role: "assistant", Content: strings.Repeat("d", 150)},
		{Role: "user", Content: "recent-1"},
		{Role: "assistant", Content: "recent-2"},
	}

	compacted, ok := compactHistoryForPrompt(history, 300, 2)
	if !ok {
		t.Fatalf("expected compaction to be triggered")
	}
	if len(compacted) != 3 {
		t.Fatalf("expected summary + 2 recent messages, got %d", len(compacted))
	}
	if compacted[0].Role != "assistant" {
		t.Fatalf("expected summary injected as assistant message")
	}
	if !strings.Contains(compacted[0].Content, "Conversation Summary") {
		t.Fatalf("expected summary header in compacted message")
	}
	if compacted[1].Content != "recent-1" || compacted[2].Content != "recent-2" {
		t.Fatalf("expected recent messages preserved")
	}
}

func TestCompactHistoryForPromptNotTriggeredWhenSmall(t *testing.T) {
	history := []Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "world"},
	}

	compacted, ok := compactHistoryForPrompt(history, 5000, 4)
	if ok {
		t.Fatalf("expected no compaction")
	}
	if len(compacted) != len(history) {
		t.Fatalf("unexpected history size: %d", len(compacted))
	}
}

func TestSummarizeHistoryMessagesUsesToolResult(t *testing.T) {
	messages := []Message{
		{Role: "user", Content: "Ask tool"},
		{
			Role:    "tool",
			Content: "",
			ToolResult: &ToolResult{
				Content: "tool output payload",
			},
		},
	}

	summary := summarizeHistoryMessages(messages, 400)
	if !strings.Contains(summary, "- user: Ask tool") {
		t.Fatalf("expected user content in summary: %q", summary)
	}
	if !strings.Contains(summary, "- tool: tool output payload") {
		t.Fatalf("expected tool result content in summary: %q", summary)
	}
}
