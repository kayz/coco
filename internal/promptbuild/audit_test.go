package promptbuild

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kayz/coco/internal/config"
)

func TestWriteAuditRecordAppendsSameDay(t *testing.T) {
	dir := t.TempDir()
	b := NewBuilder(config.PromptBuildConfig{
		RootDir:            dir,
		AuditEnabled:       true,
		AuditDir:           "audit",
		AuditRetentionDays: 7,
		AuditFilePrefix:    "promptbuild",
	})

	req := BuildRequest{Requirements: "first"}
	sections := []section{{title: "Requirements", content: "first", includeHeader: true}}
	if err := b.writeAuditRecord(req, "first prompt", sections); err != nil {
		t.Fatalf("write first audit record: %v", err)
	}
	if err := b.writeAuditRecord(req, "second prompt", sections); err != nil {
		t.Fatalf("write second audit record: %v", err)
	}

	auditFile := filepath.Join(dir, "audit", "promptbuild-"+time.Now().Format("2006-01-02")+".jsonl")
	data, err := os.ReadFile(auditFile)
	if err != nil {
		t.Fatalf("read audit file: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 audit lines, got %d", len(lines))
	}

	var rec auditRecord
	if err := json.Unmarshal([]byte(lines[0]), &rec); err != nil {
		t.Fatalf("unmarshal first line: %v", err)
	}
	if rec.Timestamp == "" || rec.RequestDigest == "" {
		t.Fatalf("expected timestamp and request_digest to be set")
	}
}

func TestCleanupOldAuditFilesByDateAndModTime(t *testing.T) {
	dir := t.TempDir()
	auditDir := filepath.Join(dir, "audit")
	if err := os.MkdirAll(auditDir, 0755); err != nil {
		t.Fatalf("mkdir audit dir: %v", err)
	}

	now := time.Date(2026, 2, 27, 10, 0, 0, 0, time.UTC)
	prefix := "promptbuild"

	oldByName := filepath.Join(auditDir, prefix+"-2026-02-18.jsonl")
	if err := os.WriteFile(oldByName, []byte("old"), 0644); err != nil {
		t.Fatalf("write old-by-name file: %v", err)
	}

	newByName := filepath.Join(auditDir, prefix+"-2026-02-26.jsonl")
	if err := os.WriteFile(newByName, []byte("new"), 0644); err != nil {
		t.Fatalf("write new-by-name file: %v", err)
	}

	fallbackOld := filepath.Join(auditDir, prefix+"-not-a-date.jsonl")
	if err := os.WriteFile(fallbackOld, []byte("fallback"), 0644); err != nil {
		t.Fatalf("write fallback file: %v", err)
	}
	oldModTime := now.AddDate(0, 0, -10)
	if err := os.Chtimes(fallbackOld, oldModTime, oldModTime); err != nil {
		t.Fatalf("set fallback old modtime: %v", err)
	}

	b := NewBuilder(config.PromptBuildConfig{
		RootDir:            dir,
		AuditEnabled:       true,
		AuditDir:           "audit",
		AuditRetentionDays: 7,
		AuditFilePrefix:    prefix,
	})

	if err := b.cleanupOldAuditFilesWithNow(now); err != nil {
		t.Fatalf("cleanup old audit files: %v", err)
	}

	if _, err := os.Stat(oldByName); !os.IsNotExist(err) {
		t.Fatalf("expected old-by-name file removed")
	}
	if _, err := os.Stat(newByName); err != nil {
		t.Fatalf("expected new-by-name file kept: %v", err)
	}
	if _, err := os.Stat(fallbackOld); !os.IsNotExist(err) {
		t.Fatalf("expected fallback old-modtime file removed")
	}
}
