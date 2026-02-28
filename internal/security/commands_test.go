package security

import "testing"

func TestNormalizeCommandPatterns(t *testing.T) {
	got := NormalizeCommandPatterns(
		[]string{" MKFS ", "custom unsafe", "", "mkfs"},
		[]string{"rm -rf /", "mkfs"},
	)

	want := []string{"rm -rf /", "mkfs", "custom unsafe"}
	if len(got) != len(want) {
		t.Fatalf("unexpected length: got %d want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("unexpected value at %d: got %q want %q", i, got[i], want[i])
		}
	}
}

func TestMatchCommandPattern(t *testing.T) {
	patterns := []string{"rm -rf /", "custom unsafe"}

	if matched, ok := MatchCommandPattern("echo hello", patterns); ok || matched != "" {
		t.Fatalf("expected no match, got ok=%v matched=%q", ok, matched)
	}

	matched, ok := MatchCommandPattern("sudo RM -rf /tmp", patterns)
	if !ok {
		t.Fatalf("expected match")
	}
	if matched != "rm -rf /" {
		t.Fatalf("unexpected pattern: %q", matched)
	}
}
