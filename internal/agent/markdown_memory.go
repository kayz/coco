package agent

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/kayz/coco/internal/config"
	"github.com/kayz/coco/internal/logger"
)

var defaultCoreMemoryFiles = []string{
	"memory/MEMORY.md",
	"memory/user_profile.md",
	"memory/response_style.md",
	"memory/project_context.md",
}

// MarkdownMemoryResult represents one recalled markdown memory fragment.
type MarkdownMemoryResult struct {
	Path       string
	Title      string
	Content    string
	ModifiedAt time.Time
	Score      float64
	Source     string // core | obsidian
}

type cachedMarkdownFile struct {
	modTime time.Time
	title   string
	content string
}

type cachedEmbedding struct {
	modTime time.Time
	vector  []float32
}

type memoryCandidate struct {
	Path         string
	Title        string
	Content      string
	Excerpt      string
	ModifiedAt   time.Time
	Source       string
	LexicalScore float64
	RecencyScore float64
	Semantic     float64
	Score        float64
	Embedding    []float32
}

// MarkdownMemory provides markdown-first long-term memory based on local files.
type MarkdownMemory struct {
	enabled       bool
	obsidianVault string
	coreFiles     []string
	maxResults    int
	maxFileBytes  int

	mu    sync.RWMutex
	cache map[string]cachedMarkdownFile

	embMu          sync.RWMutex
	embeddingCache map[string]cachedEmbedding
	embProvider    EmbeddingProvider
	semanticReady  bool

	watchMu     sync.Mutex
	watchCancel context.CancelFunc
}

// NewMarkdownMemory creates a markdown memory service.
func NewMarkdownMemory(cfg config.MemoryConfig) *MarkdownMemory {
	coreFiles := cfg.CoreFiles
	if len(coreFiles) == 0 {
		coreFiles = defaultCoreMemoryFiles
	}

	maxResults := cfg.MaxSearchResults
	if maxResults <= 0 {
		maxResults = 6
	}

	maxFileBytes := cfg.MaxFileBytes
	if maxFileBytes <= 0 {
		maxFileBytes = 200 * 1024
	}

	return &MarkdownMemory{
		enabled:        cfg.Enabled,
		obsidianVault:  normalizePath(cfg.ObsidianVault),
		coreFiles:      append([]string{}, coreFiles...),
		maxResults:     maxResults,
		maxFileBytes:   maxFileBytes,
		cache:          map[string]cachedMarkdownFile{},
		embeddingCache: map[string]cachedEmbedding{},
	}
}

// EnableSemanticSearch configures embedding-based semantic retrieval for hybrid ranking.
func (m *MarkdownMemory) EnableSemanticSearch(cfg config.EmbeddingConfig) error {
	if m == nil {
		return nil
	}

	if !cfg.Enabled {
		m.semanticReady = false
		m.embProvider = nil
		return nil
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		m.semanticReady = false
		m.embProvider = nil
		return fmt.Errorf("embedding api key is required when semantic memory search is enabled")
	}

	provider, err := NewEmbeddingProvider(EmbeddingConfig{
		Provider: cfg.Provider,
		APIKey:   cfg.APIKey,
		BaseURL:  cfg.BaseURL,
		Model:    cfg.Model,
	})
	if err != nil {
		m.semanticReady = false
		m.embProvider = nil
		return err
	}

	m.embProvider = provider
	m.semanticReady = true
	return nil
}

func (m *MarkdownMemory) IsEnabled() bool {
	return m != nil && m.enabled
}

// StartWatcher starts a lightweight polling watcher and evicts stale cache entries.
func (m *MarkdownMemory) StartWatcher(interval time.Duration) {
	if !m.IsEnabled() {
		return
	}
	if interval <= 0 {
		interval = 10 * time.Second
	}

	m.watchMu.Lock()
	if m.watchCancel != nil {
		m.watchCancel()
		m.watchCancel = nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.watchCancel = cancel
	m.watchMu.Unlock()

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.reconcileCache()
			}
		}
	}()
}

// StopWatcher stops the markdown cache watcher.
func (m *MarkdownMemory) StopWatcher() {
	m.watchMu.Lock()
	defer m.watchMu.Unlock()
	if m.watchCancel != nil {
		m.watchCancel()
		m.watchCancel = nil
	}
}

// Search recalls markdown memories by keyword relevance and file recency.
func (m *MarkdownMemory) Search(ctx context.Context, query string, limit int) ([]MarkdownMemoryResult, error) {
	_ = ctx
	if !m.IsEnabled() {
		return nil, nil
	}

	if limit <= 0 {
		limit = m.maxResults
	}
	if limit <= 0 {
		limit = 6
	}

	query = strings.TrimSpace(query)
	queryTokens := tokenizeQuery(query)
	corePaths := m.resolveCoreFiles()

	type candidate struct {
		path   string
		source string
	}

	seen := map[string]bool{}
	candidates := make([]candidate, 0, len(corePaths)+32)
	for _, p := range corePaths {
		cp := normalizePath(p)
		if cp == "" || seen[cp] {
			continue
		}
		seen[cp] = true
		candidates = append(candidates, candidate{path: cp, source: "core"})
	}

	if m.obsidianVault != "" {
		_ = filepath.WalkDir(m.obsidianVault, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			name := strings.ToLower(d.Name())
			if d.IsDir() {
				if name == ".obsidian" || name == ".trash" || name == ".git" || name == "node_modules" {
					return filepath.SkipDir
				}
				return nil
			}
			if strings.ToLower(filepath.Ext(name)) != ".md" {
				return nil
			}
			np := normalizePath(path)
			if np == "" || seen[np] {
				return nil
			}
			seen[np] = true
			candidates = append(candidates, candidate{path: np, source: "obsidian"})
			return nil
		})
	}

	candidateItems := make([]memoryCandidate, 0, len(candidates))
	for _, c := range candidates {
		item, ok, err := m.loadFile(c.path)
		if err != nil {
			logger.Warn("[Memory] failed to load markdown file: %s (%v)", c.path, err)
			continue
		}
		if !ok {
			continue
		}

		lexical := lexicalScore(queryTokens, c.path, item.title, item.content)
		if len(queryTokens) > 0 && lexical <= 0 {
			continue
		}
		recency := temporalDecayScore(item.modTime)
		score := lexical + 0.65*recency
		if c.source == "core" {
			score += 0.8
		}

		excerpt := buildExcerpt(item.content, query, 460)
		candidateItems = append(candidateItems, memoryCandidate{
			Path:         c.path,
			Title:        item.title,
			Content:      item.content,
			Excerpt:      excerpt,
			ModifiedAt:   item.modTime,
			Score:        score,
			Source:       c.source,
			LexicalScore: lexical,
			RecencyScore: recency,
		})
	}

	sort.Slice(candidateItems, func(i, j int) bool {
		if candidateItems[i].Score == candidateItems[j].Score {
			return candidateItems[i].ModifiedAt.After(candidateItems[j].ModifiedAt)
		}
		return candidateItems[i].Score > candidateItems[j].Score
	})

	semanticUsed := false
	if m.semanticReady && m.embProvider != nil && len(queryTokens) > 0 && len(candidateItems) > 1 {
		const semanticPool = 40
		if len(candidateItems) > semanticPool {
			candidateItems = candidateItems[:semanticPool]
		}

		applied, err := m.applySemanticAndMMR(ctx, query, candidateItems)
		if err != nil {
			logger.Warn("[Memory] semantic search degraded to lexical mode: %v", err)
		} else {
			candidateItems = applied
			semanticUsed = true
		}
	}

	if !semanticUsed {
		sort.Slice(candidateItems, func(i, j int) bool {
			if candidateItems[i].Score == candidateItems[j].Score {
				return candidateItems[i].ModifiedAt.After(candidateItems[j].ModifiedAt)
			}
			return candidateItems[i].Score > candidateItems[j].Score
		})
	}

	if len(candidateItems) > limit {
		candidateItems = candidateItems[:limit]
	}

	results := make([]MarkdownMemoryResult, 0, len(candidateItems))
	for _, c := range candidateItems {
		results = append(results, MarkdownMemoryResult{
			Path:       c.Path,
			Title:      c.Title,
			Content:    c.Excerpt,
			ModifiedAt: c.ModifiedAt,
			Score:      c.Score,
			Source:     c.Source,
		})
	}

	return results, nil
}

func (m *MarkdownMemory) applySemanticAndMMR(ctx context.Context, query string, candidates []memoryCandidate) ([]memoryCandidate, error) {
	if len(candidates) == 0 || m.embProvider == nil {
		return candidates, nil
	}

	queryEmbeddings, err := m.embProvider.CreateEmbedding(ctx, []string{query})
	if err != nil {
		return nil, fmt.Errorf("query embedding failed: %w", err)
	}
	if len(queryEmbeddings) == 0 {
		return nil, fmt.Errorf("empty query embedding")
	}
	queryVec := queryEmbeddings[0]

	missingIdx := make([]int, 0, len(candidates))
	missingTexts := make([]string, 0, len(candidates))
	for i := range candidates {
		if vec, ok := m.getCachedEmbedding(candidates[i].Path, candidates[i].ModifiedAt); ok {
			candidates[i].Embedding = vec
			continue
		}
		missingIdx = append(missingIdx, i)
		missingTexts = append(missingTexts, buildSemanticText(candidates[i]))
	}

	if len(missingTexts) > 0 {
		vectors, err := m.embProvider.CreateEmbedding(ctx, missingTexts)
		if err != nil {
			return nil, fmt.Errorf("candidate embeddings failed: %w", err)
		}
		if len(vectors) != len(missingTexts) {
			return nil, fmt.Errorf("embedding count mismatch: want %d got %d", len(missingTexts), len(vectors))
		}
		for i, idx := range missingIdx {
			vec := vectors[i]
			candidates[idx].Embedding = vec
			m.setCachedEmbedding(candidates[idx].Path, candidates[idx].ModifiedAt, vec)
		}
	}

	maxLex := 0.0
	for _, c := range candidates {
		if c.LexicalScore > maxLex {
			maxLex = c.LexicalScore
		}
	}
	if maxLex <= 0 {
		maxLex = 1
	}

	for i := range candidates {
		semantic := cosineSimilarity(queryVec, candidates[i].Embedding)
		if semantic < 0 {
			semantic = 0
		}
		candidates[i].Semantic = semantic
		lexNorm := candidates[i].LexicalScore / maxLex
		score := 0.50*semantic + 0.30*lexNorm + 0.20*candidates[i].RecencyScore
		if candidates[i].Source == "core" {
			score += 0.05
		}
		candidates[i].Score = score
	}

	return mmrRerankCandidates(candidates, 0.72), nil
}

// Get returns full markdown content from a memory file path.
func (m *MarkdownMemory) Get(path string) (MarkdownMemoryResult, error) {
	if !m.IsEnabled() {
		return MarkdownMemoryResult{}, fmt.Errorf("markdown memory is disabled")
	}

	resolved, err := m.resolveAllowedPath(path)
	if err != nil {
		return MarkdownMemoryResult{}, err
	}

	item, ok, err := m.loadFile(resolved)
	if err != nil {
		return MarkdownMemoryResult{}, err
	}
	if !ok {
		return MarkdownMemoryResult{}, os.ErrNotExist
	}

	source := "core"
	if m.isUnderVault(resolved) {
		source = "obsidian"
	}

	return MarkdownMemoryResult{
		Path:       resolved,
		Title:      item.title,
		Content:    item.content,
		ModifiedAt: item.modTime,
		Score:      0,
		Source:     source,
	}, nil
}

func (m *MarkdownMemory) resolveCoreFiles() []string {
	files := make([]string, 0, len(m.coreFiles))
	for _, p := range m.coreFiles {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		resolved := resolveBestEffortPath(p)
		if resolved != "" {
			files = append(files, resolved)
		}
	}
	return files
}

func (m *MarkdownMemory) resolveAllowedPath(input string) (string, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", fmt.Errorf("path is required")
	}

	absInput := resolveBestEffortPath(input)
	if absInput == "" {
		return "", fmt.Errorf("invalid path")
	}

	if m.isAllowedPath(absInput) {
		return absInput, nil
	}

	if m.obsidianVault != "" {
		candidate := normalizePath(filepath.Join(m.obsidianVault, input))
		if candidate != "" && m.isAllowedPath(candidate) {
			return candidate, nil
		}
	}

	return "", fmt.Errorf("path is outside memory scope: %s", input)
}

func (m *MarkdownMemory) isAllowedPath(path string) bool {
	path = normalizePath(path)
	if path == "" {
		return false
	}

	for _, core := range m.resolveCoreFiles() {
		if normalizePath(core) == path {
			return true
		}
	}
	return m.isUnderVault(path)
}

func (m *MarkdownMemory) isUnderVault(path string) bool {
	if m.obsidianVault == "" || path == "" {
		return false
	}
	rel, err := filepath.Rel(m.obsidianVault, path)
	if err != nil {
		return false
	}
	return rel == "." || (!strings.HasPrefix(rel, "..") && rel != "")
}

func (m *MarkdownMemory) loadFile(path string) (cachedMarkdownFile, bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cachedMarkdownFile{}, false, nil
		}
		return cachedMarkdownFile{}, false, err
	}
	if info.IsDir() {
		return cachedMarkdownFile{}, false, nil
	}

	m.mu.RLock()
	if cached, ok := m.cache[path]; ok && cached.modTime.Equal(info.ModTime()) {
		m.mu.RUnlock()
		return cached, true, nil
	}
	m.mu.RUnlock()

	data, err := os.ReadFile(path)
	if err != nil {
		return cachedMarkdownFile{}, false, err
	}

	if len(data) > m.maxFileBytes {
		data = data[:m.maxFileBytes]
	}

	content := strings.TrimSpace(string(data))
	if content == "" {
		return cachedMarkdownFile{}, false, nil
	}

	item := cachedMarkdownFile{
		modTime: info.ModTime(),
		title:   extractMarkdownTitle(path, content),
		content: content,
	}

	m.mu.Lock()
	m.cache[path] = item
	m.mu.Unlock()

	return item, true, nil
}

func (m *MarkdownMemory) reconcileCache() {
	allowed := map[string]bool{}
	for _, p := range m.resolveCoreFiles() {
		if p != "" {
			allowed[p] = true
		}
	}

	if m.obsidianVault != "" {
		_ = filepath.WalkDir(m.obsidianVault, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				name := strings.ToLower(d.Name())
				if name == ".obsidian" || name == ".trash" || name == ".git" || name == "node_modules" {
					return filepath.SkipDir
				}
				return nil
			}
			if strings.EqualFold(filepath.Ext(d.Name()), ".md") {
				allowed[normalizePath(path)] = true
			}
			return nil
		})
	}

	m.mu.Lock()
	for path := range m.cache {
		if !allowed[path] {
			delete(m.cache, path)
			continue
		}
		if _, err := os.Stat(path); err != nil {
			delete(m.cache, path)
		}
	}
	m.mu.Unlock()

	m.embMu.Lock()
	for path := range m.embeddingCache {
		if !allowed[path] {
			delete(m.embeddingCache, path)
			continue
		}
		if _, err := os.Stat(path); err != nil {
			delete(m.embeddingCache, path)
		}
	}
	m.embMu.Unlock()
}

func extractMarkdownTitle(path, content string) string {
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") {
			title := strings.TrimSpace(strings.TrimLeft(line, "#"))
			if title != "" {
				return title
			}
		}
	}
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	if base == "" {
		return "untitled"
	}
	return base
}

func buildExcerpt(content, query string, maxLen int) string {
	if maxLen <= 0 {
		maxLen = 460
	}
	plain := strings.TrimSpace(strings.ReplaceAll(content, "\r", ""))
	if plain == "" {
		return ""
	}
	if len(plain) <= maxLen {
		return plain
	}

	lower := strings.ToLower(plain)
	for _, token := range tokenizeQuery(query) {
		idx := strings.Index(lower, token)
		if idx >= 0 {
			start := idx - maxLen/3
			if start < 0 {
				start = 0
			}
			end := start + maxLen
			if end > len(plain) {
				end = len(plain)
			}
			return strings.TrimSpace(plain[start:end]) + "..."
		}
	}
	return strings.TrimSpace(plain[:maxLen]) + "..."
}

func lexicalScore(tokens []string, path, title, content string) float64 {
	if len(tokens) == 0 {
		return 0
	}
	score := 0.0
	p := strings.ToLower(path)
	t := strings.ToLower(title)
	c := strings.ToLower(content)
	for _, tok := range tokens {
		if tok == "" {
			continue
		}
		if strings.Contains(p, tok) {
			score += 2.0
		}
		if strings.Contains(t, tok) {
			score += 1.8
		}
		if n := strings.Count(c, tok); n > 0 {
			score += float64(n) * 0.45
		}
	}
	return score
}

func temporalDecayScore(modifiedAt time.Time) float64 {
	if modifiedAt.IsZero() {
		return 0
	}
	days := time.Since(modifiedAt).Hours() / 24
	if days < 0 {
		days = 0
	}
	const halfLifeDays = 30.0
	return math.Exp(-math.Ln2 * days / halfLifeDays)
}

func buildSemanticText(c memoryCandidate) string {
	excerpt := buildExcerpt(c.Content, "", 900)
	return strings.TrimSpace(c.Title + "\n" + excerpt)
}

func (m *MarkdownMemory) getCachedEmbedding(path string, modTime time.Time) ([]float32, bool) {
	m.embMu.RLock()
	cached, ok := m.embeddingCache[path]
	m.embMu.RUnlock()
	if !ok || !cached.modTime.Equal(modTime) || len(cached.vector) == 0 {
		return nil, false
	}
	vec := make([]float32, len(cached.vector))
	copy(vec, cached.vector)
	return vec, true
}

func (m *MarkdownMemory) setCachedEmbedding(path string, modTime time.Time, vector []float32) {
	if len(vector) == 0 {
		return
	}
	cp := make([]float32, len(vector))
	copy(cp, vector)
	m.embMu.Lock()
	m.embeddingCache[path] = cachedEmbedding{
		modTime: modTime,
		vector:  cp,
	}
	m.embMu.Unlock()
}

func cosineSimilarity(a, b []float32) float64 {
	if len(a) == 0 || len(b) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, normA, normB float64
	for i := 0; i < len(a); i++ {
		ai := float64(a[i])
		bi := float64(b[i])
		dot += ai * bi
		normA += ai * ai
		normB += bi * bi
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

func mmrRerankCandidates(candidates []memoryCandidate, lambda float64) []memoryCandidate {
	if len(candidates) <= 1 {
		return candidates
	}
	if lambda <= 0 || lambda >= 1 {
		lambda = 0.72
	}

	remaining := make([]memoryCandidate, len(candidates))
	copy(remaining, candidates)
	selected := make([]memoryCandidate, 0, len(candidates))

	for len(remaining) > 0 {
		bestIdx := 0
		bestScore := -1e9
		for i := range remaining {
			relevance := remaining[i].Score
			diversityPenalty := 0.0
			for _, s := range selected {
				sim := cosineSimilarity(remaining[i].Embedding, s.Embedding)
				if sim > diversityPenalty {
					diversityPenalty = sim
				}
			}
			mmr := lambda*relevance - (1.0-lambda)*diversityPenalty
			if mmr > bestScore {
				bestScore = mmr
				bestIdx = i
			}
		}
		selected = append(selected, remaining[bestIdx])
		remaining = append(remaining[:bestIdx], remaining[bestIdx+1:]...)
	}

	return selected
}

func tokenizeQuery(query string) []string {
	query = strings.TrimSpace(strings.ToLower(query))
	if query == "" {
		return nil
	}
	parts := strings.FieldsFunc(query, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r)
	})
	seen := map[string]bool{}
	tokens := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if len(p) < 2 || seen[p] {
			continue
		}
		seen[p] = true
		tokens = append(tokens, p)
	}
	return tokens
}

func resolveBestEffortPath(path string) string {
	path = normalizePath(path)
	if path == "" {
		return ""
	}

	if filepath.IsAbs(path) {
		return path
	}

	if wd, err := os.Getwd(); err == nil {
		candidate := normalizePath(filepath.Join(wd, path))
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}

	exeDir := normalizePath(getExecutableDir())
	if exeDir != "" {
		candidate := normalizePath(filepath.Join(exeDir, path))
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}

	if wd, err := os.Getwd(); err == nil {
		return normalizePath(filepath.Join(wd, path))
	}
	return normalizePath(path)
}

func normalizePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if strings.HasPrefix(path, "~") {
		home, err := os.UserHomeDir()
		if err == nil && home != "" {
			if path == "~" {
				path = home
			} else if strings.HasPrefix(path, "~/") || strings.HasPrefix(path, "~\\") {
				path = filepath.Join(home, path[2:])
			}
		}
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return filepath.Clean(abs)
}
