package agent

import (
	"os"
	"reflect"
	"testing"
)

func TestNormalizeMemoryQueries(t *testing.T) {
	in := []string{"  Project Plan ", "project plan", "", "User Style", "x", "Another"}
	got := normalizeMemoryQueries(in, 3)
	want := []string{"Project Plan", "User Style", "Another"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected normalized queries, got=%v want=%v", got, want)
	}
}

func TestExtractJSONObject(t *testing.T) {
	content := "```json\n{\"need_clarification\":false,\"task_complexity\":\"normal\"}\n```"
	got := extractJSONObject(content)
	if got == "" {
		t.Fatalf("expected JSON payload")
	}
	if got[0] != '{' || got[len(got)-1] != '}' {
		t.Fatalf("invalid json extraction: %s", got)
	}
}

func TestNormalizeTaskComplexity(t *testing.T) {
	if normalizeTaskComplexity("COMPLEX") != "complex" {
		t.Fatalf("expected complex")
	}
	if normalizeTaskComplexity("unknown") != "normal" {
		t.Fatalf("expected fallback normal")
	}
}

func TestIsTwoStageOrchestrationEnabled(t *testing.T) {
	key := "COCO_AGENT_ORCHESTRATION_ENABLE"
	old := os.Getenv(key)
	defer func() {
		_ = os.Setenv(key, old)
	}()

	_ = os.Unsetenv(key)
	if !isTwoStageOrchestrationEnabled() {
		t.Fatalf("expected default enabled")
	}
	_ = os.Setenv(key, "false")
	if isTwoStageOrchestrationEnabled() {
		t.Fatalf("expected disabled")
	}
	_ = os.Setenv(key, "1")
	if !isTwoStageOrchestrationEnabled() {
		t.Fatalf("expected enabled")
	}
}
