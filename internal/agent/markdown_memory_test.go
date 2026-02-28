package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kayz/coco/internal/config"
)

func TestMarkdownMemorySearchPrefersRecent(t *testing.T) {
	tmp := t.TempDir()
	vaultDir := filepath.Join(tmp, "vault")
	if err := os.MkdirAll(vaultDir, 0755); err != nil {
		t.Fatalf("mkdir vault: %v", err)
	}

	oldFile := filepath.Join(vaultDir, "old.md")
	newFile := filepath.Join(vaultDir, "new.md")
	if err := os.WriteFile(oldFile, []byte("# Old\npython release notes"), 0644); err != nil {
		t.Fatalf("write old file: %v", err)
	}
	if err := os.WriteFile(newFile, []byte("# New\npython release notes"), 0644); err != nil {
		t.Fatalf("write new file: %v", err)
	}

	oldTime := time.Now().Add(-10 * 24 * time.Hour)
	if err := os.Chtimes(oldFile, oldTime, oldTime); err != nil {
		t.Fatalf("set old mtime: %v", err)
	}

	mem := NewMarkdownMemory(config.MemoryConfig{
		Enabled:          true,
		ObsidianVault:    vaultDir,
		MaxSearchResults: 5,
	})

	got, err := mem.Search(context.Background(), "python", 5)
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(got) < 2 {
		t.Fatalf("expected at least 2 results, got %d", len(got))
	}
	if !strings.HasSuffix(got[0].Path, "new.md") {
		t.Fatalf("expected newest file first, got %s", got[0].Path)
	}
}

func TestMarkdownMemoryGetScopeAndRefresh(t *testing.T) {
	tmp := t.TempDir()
	coreDir := filepath.Join(tmp, "memory")
	vaultDir := filepath.Join(tmp, "vault")
	if err := os.MkdirAll(coreDir, 0755); err != nil {
		t.Fatalf("mkdir core: %v", err)
	}
	if err := os.MkdirAll(vaultDir, 0755); err != nil {
		t.Fatalf("mkdir vault: %v", err)
	}

	coreFile := filepath.Join(coreDir, "MEMORY.md")
	if err := os.WriteFile(coreFile, []byte("# Core\nv1"), 0644); err != nil {
		t.Fatalf("write core: %v", err)
	}

	mem := NewMarkdownMemory(config.MemoryConfig{
		Enabled:       true,
		ObsidianVault: vaultDir,
		CoreFiles:     []string{coreFile},
	})

	first, err := mem.Get(coreFile)
	if err != nil {
		t.Fatalf("get core failed: %v", err)
	}
	if !strings.Contains(first.Content, "v1") {
		t.Fatalf("unexpected content: %s", first.Content)
	}

	time.Sleep(20 * time.Millisecond)
	if err := os.WriteFile(coreFile, []byte("# Core\nv2"), 0644); err != nil {
		t.Fatalf("rewrite core: %v", err)
	}

	second, err := mem.Get(coreFile)
	if err != nil {
		t.Fatalf("get core after update failed: %v", err)
	}
	if !strings.Contains(second.Content, "v2") {
		t.Fatalf("expected refreshed content, got: %s", second.Content)
	}

	outside := filepath.Join(tmp, "outside.md")
	if err := os.WriteFile(outside, []byte("x"), 0644); err != nil {
		t.Fatalf("write outside: %v", err)
	}
	if _, err := mem.Get(outside); err == nil {
		t.Fatalf("expected scope error for outside file")
	}
}

func TestTemporalDecayScoreMonotonic(t *testing.T) {
	now := time.Now()
	recent := temporalDecayScore(now.Add(-24 * time.Hour))
	old := temporalDecayScore(now.Add(-60 * 24 * time.Hour))
	veryOld := temporalDecayScore(now.Add(-365 * 24 * time.Hour))

	if !(recent > old && old > veryOld) {
		t.Fatalf("expected monotonic decay, got recent=%f old=%f veryOld=%f", recent, old, veryOld)
	}
}

func TestMMRRerankCandidatesDiversifies(t *testing.T) {
	cands := []memoryCandidate{
		{
			Path:      "a.md",
			Score:     0.95,
			Embedding: []float32{1, 0},
		},
		{
			Path:      "b.md",
			Score:     0.90,
			Embedding: []float32{0.99, 0.01},
		},
		{
			Path:      "c.md",
			Score:     0.86,
			Embedding: []float32{0, 1},
		},
	}

	got := mmrRerankCandidates(cands, 0.6)
	if len(got) != 3 {
		t.Fatalf("expected 3 candidates, got %d", len(got))
	}
	if got[0].Path != "a.md" {
		t.Fatalf("expected first candidate a.md, got %s", got[0].Path)
	}
	if got[1].Path != "c.md" {
		t.Fatalf("expected second candidate to be diversified c.md, got %s", got[1].Path)
	}
}

func TestMarkdownMemoryReconcileCacheRemovesDeletedFiles(t *testing.T) {
	tmp := t.TempDir()
	coreDir := filepath.Join(tmp, "memory")
	vaultDir := filepath.Join(tmp, "vault")
	if err := os.MkdirAll(coreDir, 0755); err != nil {
		t.Fatalf("mkdir core: %v", err)
	}
	if err := os.MkdirAll(vaultDir, 0755); err != nil {
		t.Fatalf("mkdir vault: %v", err)
	}

	coreFile := filepath.Join(coreDir, "MEMORY.md")
	vaultFile := filepath.Join(vaultDir, "note.md")
	if err := os.WriteFile(coreFile, []byte("# Core\nstable"), 0644); err != nil {
		t.Fatalf("write core: %v", err)
	}
	if err := os.WriteFile(vaultFile, []byte("# Note\ntransient"), 0644); err != nil {
		t.Fatalf("write vault file: %v", err)
	}

	mem := NewMarkdownMemory(config.MemoryConfig{
		Enabled:       true,
		ObsidianVault: vaultDir,
		CoreFiles:     []string{coreFile},
	})

	loaded, err := mem.Get(vaultFile)
	if err != nil {
		t.Fatalf("load vault file: %v", err)
	}
	mem.setCachedEmbedding(vaultFile, loaded.ModifiedAt, []float32{1, 0.2})

	if err := os.Remove(vaultFile); err != nil {
		t.Fatalf("remove vault file: %v", err)
	}

	mem.reconcileCache()

	mem.mu.RLock()
	_, cached := mem.cache[vaultFile]
	mem.mu.RUnlock()
	if cached {
		t.Fatalf("expected markdown cache eviction for deleted file")
	}

	mem.embMu.RLock()
	_, embCached := mem.embeddingCache[vaultFile]
	mem.embMu.RUnlock()
	if embCached {
		t.Fatalf("expected embedding cache eviction for deleted file")
	}
}

func TestMarkdownMemoryWatcherPollsAndEvictsDeletedFiles(t *testing.T) {
	tmp := t.TempDir()
	coreDir := filepath.Join(tmp, "memory")
	vaultDir := filepath.Join(tmp, "vault")
	if err := os.MkdirAll(coreDir, 0755); err != nil {
		t.Fatalf("mkdir core: %v", err)
	}
	if err := os.MkdirAll(vaultDir, 0755); err != nil {
		t.Fatalf("mkdir vault: %v", err)
	}

	coreFile := filepath.Join(coreDir, "MEMORY.md")
	vaultFile := filepath.Join(vaultDir, "watch.md")
	if err := os.WriteFile(coreFile, []byte("# Core\nok"), 0644); err != nil {
		t.Fatalf("write core: %v", err)
	}
	if err := os.WriteFile(vaultFile, []byte("# Watch\ncontent"), 0644); err != nil {
		t.Fatalf("write vault file: %v", err)
	}

	mem := NewMarkdownMemory(config.MemoryConfig{
		Enabled:       true,
		ObsidianVault: vaultDir,
		CoreFiles:     []string{coreFile},
	})

	loaded, err := mem.Get(vaultFile)
	if err != nil {
		t.Fatalf("load vault file: %v", err)
	}
	mem.setCachedEmbedding(vaultFile, loaded.ModifiedAt, []float32{0.4, 0.8})

	mem.StartWatcher(20 * time.Millisecond)
	defer mem.StopWatcher()

	if err := os.Remove(vaultFile); err != nil {
		t.Fatalf("remove vault file: %v", err)
	}

	deadline := time.Now().Add(800 * time.Millisecond)
	for time.Now().Before(deadline) {
		mem.mu.RLock()
		_, fileCached := mem.cache[vaultFile]
		mem.mu.RUnlock()
		mem.embMu.RLock()
		_, embCached := mem.embeddingCache[vaultFile]
		mem.embMu.RUnlock()
		if !fileCached && !embCached {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}

	t.Fatalf("watcher did not evict deleted file from cache in time")
}
