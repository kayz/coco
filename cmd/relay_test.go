package cmd

import (
	"os"
	"strings"
	"testing"
)

func TestNormalizePlatformArg(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "empty", args: nil, want: ""},
		{name: "trim and lower", args: []string{"  WeCom  "}, want: "wecom"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizePlatformArg(tt.args)
			if got != tt.want {
				t.Fatalf("normalizePlatformArg() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSanitizeID(t *testing.T) {
	if got := sanitizeID(" User@Name "); got != "user-name" {
		t.Fatalf("sanitizeID unexpected result: %q", got)
	}
	if got := sanitizeID("###"); got != "x" {
		t.Fatalf("sanitizeID should fallback to x, got %q", got)
	}
}

func TestBuildFallbackRelayUserID(t *testing.T) {
	oldUser := os.Getenv("USER")
	oldUsername := os.Getenv("USERNAME")
	t.Cleanup(func() {
		_ = os.Setenv("USER", oldUser)
		_ = os.Setenv("USERNAME", oldUsername)
	})

	_ = os.Setenv("USERNAME", "Test.User")
	_ = os.Unsetenv("USER")

	got := buildFallbackRelayUserID("wecom")
	if !strings.HasPrefix(got, "wecom-test-user-") {
		t.Fatalf("buildFallbackRelayUserID prefix mismatch: %q", got)
	}
}
