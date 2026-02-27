package promptbuild

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var auditMu sync.Mutex

type auditRecord struct {
	Timestamp     string         `json:"timestamp"`
	RequestDigest string         `json:"request_digest"`
	FinalPrompt   string         `json:"final_prompt"`
	Sections      []string       `json:"sections"`
	HistoryMeta   map[string]any `json:"history_meta,omitempty"`
}

func (b *Builder) writeAuditRecord(req BuildRequest, finalPrompt string, sections []section) error {
	if !b.cfg.AuditEnabled {
		return nil
	}

	auditDir := b.resolvePath(b.cfg.AuditDir)
	if err := os.MkdirAll(auditDir, 0755); err != nil {
		return fmt.Errorf("create audit dir: %w", err)
	}

	prefix := strings.TrimSpace(b.cfg.AuditFilePrefix)
	if prefix == "" {
		prefix = "promptbuild"
	}

	now := time.Now()
	fileName := fmt.Sprintf("%s-%s.jsonl", prefix, now.Format("2006-01-02"))
	filePath := filepath.Join(auditDir, fileName)

	record := auditRecord{
		Timestamp:     now.Format(time.RFC3339),
		RequestDigest: buildRequestDigest(req),
		FinalPrompt:   finalPrompt,
		Sections:      sectionTitles(sections),
		HistoryMeta: map[string]any{
			"conversation_id": req.History.ConversationID,
			"platform":        req.History.Platform,
			"channel_id":      req.History.ChannelID,
			"user_id":         req.History.UserID,
			"limit":           effectiveHistoryLimit(req),
		},
	}

	line, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("marshal audit record: %w", err)
	}

	auditMu.Lock()
	defer auditMu.Unlock()

	if err := appendJSONL(filePath, line); err != nil {
		return err
	}

	if err := b.cleanupOldAuditFilesWithNow(now); err != nil {
		return err
	}

	return nil
}

func appendJSONL(filePath string, line []byte) error {
	f, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("open audit file: %w", err)
	}
	defer f.Close()

	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("write audit file: %w", err)
	}
	return nil
}

func (b *Builder) CleanupOldAuditFiles() error {
	auditMu.Lock()
	defer auditMu.Unlock()
	return b.cleanupOldAuditFilesWithNow(time.Now())
}

func (b *Builder) cleanupOldAuditFilesWithNow(now time.Time) error {
	if !b.cfg.AuditEnabled || b.cfg.AuditRetentionDays <= 0 {
		return nil
	}

	auditDir := b.resolvePath(b.cfg.AuditDir)
	entries, err := os.ReadDir(auditDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("list audit dir: %w", err)
	}

	prefix := strings.TrimSpace(b.cfg.AuditFilePrefix)
	if prefix == "" {
		prefix = "promptbuild"
	}

	cutoff := now.AddDate(0, 0, -b.cfg.AuditRetentionDays)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, prefix+"-") || !strings.HasSuffix(name, ".jsonl") {
			continue
		}

		filePath := filepath.Join(auditDir, name)
		fileDate, ok := parseAuditDate(name, prefix)
		if ok {
			if fileDate.Before(startOfDay(cutoff)) {
				if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
					return fmt.Errorf("remove old audit file %s: %w", filePath, err)
				}
			}
			continue
		}

		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("stat audit file %s: %w", filePath, err)
		}
		if info.ModTime().Before(cutoff) {
			if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("remove old audit file %s: %w", filePath, err)
			}
		}
	}

	return nil
}

func parseAuditDate(filename, prefix string) (time.Time, bool) {
	raw := strings.TrimSuffix(filename, ".jsonl")
	raw = strings.TrimPrefix(raw, prefix+"-")
	t, err := time.Parse("2006-01-02", raw)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

func sectionTitles(sections []section) []string {
	titles := make([]string, 0, len(sections))
	for _, s := range sections {
		title := strings.TrimSpace(s.title)
		if title != "" {
			titles = append(titles, title)
		}
	}
	return titles
}

func buildRequestDigest(req BuildRequest) string {
	digestInput := struct {
		Agent       string `json:"agent,omitempty"`
		SpecPath    string `json:"spec_path,omitempty"`
		SystemCount int    `json:"system_count"`
		TaskCount   int    `json:"task_count"`
		FormatCount int    `json:"format_count"`
		StyleCount  int    `json:"style_count"`
		RefCount    int    `json:"ref_count"`
		ReqLen      int    `json:"requirements_len"`
		UserLen     int    `json:"user_input_len"`
		HasHistory  bool   `json:"has_history"`
		HistoryKey  string `json:"history_key,omitempty"`
		InputCount  int    `json:"input_count"`
	}{
		Agent:       strings.TrimSpace(req.Agent),
		SpecPath:    strings.TrimSpace(req.SpecPath),
		SystemCount: len(req.System),
		TaskCount:   len(req.Task),
		FormatCount: len(req.Format),
		StyleCount:  len(req.Style),
		RefCount:    len(req.References),
		ReqLen:      len(strings.TrimSpace(req.Requirements)),
		UserLen:     len(strings.TrimSpace(req.UserInput)),
		HasHistory:  req.History.ConversationID > 0 || (req.History.Platform != "" && req.History.ChannelID != "" && req.History.UserID != ""),
		HistoryKey:  strings.TrimSpace(req.History.Platform + ":" + req.History.ChannelID + ":" + req.History.UserID),
		InputCount:  len(req.Inputs),
	}
	payload, _ := json.Marshal(digestInput)
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func effectiveHistoryLimit(req BuildRequest) int {
	if req.History.Limit > 0 {
		return req.History.Limit
	}
	if req.MaxHistory > 0 {
		return req.MaxHistory
	}
	return 200
}

func startOfDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}
