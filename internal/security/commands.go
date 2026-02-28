package security

import "strings"

// DefaultBlockedCommandPatterns are always blocked, even if not configured.
var DefaultBlockedCommandPatterns = []string{
	"rm -rf /",
	"rm -rf /*",
	"mkfs",
	"dd if=",
	":(){:|:&};:",
	":(){ :|:& };:",
	"> /dev/sda",
	"chmod -R 777 /",
}

// NormalizeCommandPatterns lowercases, trims, deduplicates, and merges defaults.
func NormalizeCommandPatterns(configured []string, defaults []string) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0, len(defaults)+len(configured))
	appendUnique := func(patterns []string) {
		for _, p := range patterns {
			n := strings.TrimSpace(strings.ToLower(p))
			if n == "" {
				continue
			}
			if _, ok := seen[n]; ok {
				continue
			}
			seen[n] = struct{}{}
			out = append(out, n)
		}
	}
	appendUnique(defaults)
	appendUnique(configured)
	return out
}

// MatchCommandPattern returns the matched pattern (if any) for a command.
func MatchCommandPattern(command string, patterns []string) (string, bool) {
	cmdLower := strings.ToLower(command)
	for _, p := range patterns {
		if p == "" {
			continue
		}
		if strings.Contains(cmdLower, strings.ToLower(p)) {
			return p, true
		}
	}
	return "", false
}
