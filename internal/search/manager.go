package search

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/pltanton/lingti-bot/internal/config"
)

type Manager struct {
	registry        *Registry
	engines         map[string]Engine
	primaryEngine   string
	secondaryEngine string
	autoSearch      bool
	mu              sync.RWMutex
}

func NewManager(cfg config.SearchConfig, registry *Registry) (*Manager, error) {
	m := &Manager{
		registry:        registry,
		engines:         make(map[string]Engine),
		primaryEngine:   cfg.PrimaryEngine,
		secondaryEngine: cfg.SecondaryEngine,
		autoSearch:      cfg.AutoSearch,
	}

	for _, engineCfg := range cfg.Engines {
		if engineCfg.Enabled && engineCfg.APIKey != "" {
			searchCfg := SearchEngineConfig{
				Name:     engineCfg.Name,
				Type:     engineCfg.Type,
				APIKey:   engineCfg.APIKey,
				BaseURL:  engineCfg.BaseURL,
				Enabled:  engineCfg.Enabled,
				Priority: engineCfg.Priority,
				Options:  engineCfg.Options,
			}
			engine, err := registry.CreateEngine(searchCfg)
			if err != nil {
				return nil, err
			}
			m.engines[engineCfg.Name] = engine
		}
	}

	return m, nil
}

func (m *Manager) AddEngine(config SearchEngineConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	engine, err := m.registry.CreateEngine(config)
	if err != nil {
		return err
	}

	m.engines[config.Name] = engine
	return nil
}

func (m *Manager) RemoveEngine(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.engines, name)
}

func (m *Manager) ListEngines() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	names := make([]string, 0, len(m.engines))
	for name := range m.engines {
		names = append(names, name)
	}
	return names
}

func (m *Manager) Search(ctx context.Context, query string, limit int) (*SearchResponse, error) {
	m.mu.RLock()
	engines := make([]Engine, 0, len(m.engines))
	for _, e := range m.engines {
		if e.IsEnabled() {
			engines = append(engines, e)
		}
	}
	m.mu.RUnlock()

	if len(engines) == 0 {
		return nil, fmt.Errorf("no available search engine")
	}

	for i := range engines {
		for j := i + 1; j < len(engines); j++ {
			if engines[i].Priority() > engines[j].Priority() {
				engines[i], engines[j] = engines[j], engines[i]
			}
		}
	}

	var lastErr error
	for _, engine := range engines {
		resp, err := engine.Search(ctx, query, limit)
		if err == nil && len(resp.Results) > 0 {
			return resp, nil
		}
		if err != nil {
			lastErr = err
		}
	}

	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("all search engines failed to return results")
}

func (m *Manager) SearchWithEngine(ctx context.Context, engineName, query string, limit int) (*SearchResponse, error) {
	m.mu.RLock()
	engine, ok := m.engines[engineName]
	m.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("engine not found: %s", engineName)
	}

	return engine.Search(ctx, query, limit)
}

func (m *Manager) SearchAll(ctx context.Context, query string, limit int) (*CombinedSearchResponse, error) {
	m.mu.RLock()
	engines := make([]Engine, 0, len(m.engines))
	for _, e := range m.engines {
		engines = append(engines, e)
	}
	m.mu.RUnlock()

	var wg sync.WaitGroup
	responses := make(map[string]SearchResponse)
	var mu sync.Mutex

	for _, engine := range engines {
		if !engine.IsEnabled() {
			continue
		}
		wg.Add(1)
		go func(eng Engine) {
			defer wg.Done()
			if resp, err := eng.Search(ctx, query, limit); err == nil {
				mu.Lock()
				responses[eng.Name()] = *resp
				mu.Unlock()
			}
		}(engine)
	}

	wg.Wait()

	combined := m.combineResults(responses)

	return &CombinedSearchResponse{
		Query:     query,
		Responses: responses,
		Combined:  combined,
	}, nil
}

func (m *Manager) selectEngine(query string) Engine {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if isEnglishQuery(query) {
		if engine, ok := m.engines[m.secondaryEngine]; ok && engine.IsEnabled() {
			return engine
		}
	}

	if engine, ok := m.engines[m.primaryEngine]; ok && engine.IsEnabled() {
		return engine
	}

	if engine, ok := m.engines[m.secondaryEngine]; ok && engine.IsEnabled() {
		return engine
	}

	for _, engine := range m.engines {
		if engine.IsEnabled() {
			return engine
		}
	}

	return nil
}

func isEnglishQuery(query string) bool {
	query = strings.TrimSpace(query)
	if query == "" {
		return false
	}

	englishChars := 0
	for _, r := range query {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			englishChars++
		}
	}

	return float64(englishChars)/float64(len(query)) > 0.6
}

func IsExplicitSearchRequest(query string) bool {
	query = strings.ToLower(strings.TrimSpace(query))
	patterns := []string{
		`^搜索`,
		`^search`,
		`^查找`,
		`^查一下`,
		`^帮我搜`,
		`^用.*搜索`,
		`^search.*with`,
	}

	for _, pattern := range patterns {
		if matched, _ := regexp.MatchString(pattern, query); matched {
			return true
		}
	}
	return false
}

func (m *Manager) combineResults(responses map[string]SearchResponse) []SearchResult {
	seen := make(map[string]bool)
	var combined []SearchResult

	m.mu.RLock()
	var engines []Engine
	for _, e := range m.engines {
		engines = append(engines, e)
	}
	m.mu.RUnlock()

	for i := range engines {
		for j := i + 1; j < len(engines); j++ {
			if engines[i].Priority() > engines[j].Priority() {
				engines[i], engines[j] = engines[j], engines[i]
			}
		}
	}

	for _, engine := range engines {
		if resp, ok := responses[engine.Name()]; ok {
			for _, result := range resp.Results {
				if !seen[result.URL] {
					seen[result.URL] = true
					combined = append(combined, result)
				}
			}
		}
	}

	return combined
}

func (m *Manager) ShouldAutoSearch(query string) bool {
	if !m.autoSearch {
		return false
	}
	questionWords := []string{"什么", "怎么", "如何", "为什么", "在哪", "哪里", "who", "what", "when", "where", "why", "how"}
	queryLower := strings.ToLower(query)
	for _, word := range questionWords {
		if strings.Contains(queryLower, word) {
			return true
		}
	}
	return false
}

func (m *Manager) SetPrimaryEngine(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.engines[name]; !ok {
		return fmt.Errorf("engine not found: %s", name)
	}

	m.primaryEngine = name
	return nil
}

func (m *Manager) SetSecondaryEngine(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.engines[name]; !ok {
		return fmt.Errorf("engine not found: %s", name)
	}

	m.secondaryEngine = name
	return nil
}

// FormatSearchResults 格式化搜索结果为可读字符串
func FormatSearchResults(resp *SearchResponse) string {
	if resp == nil || len(resp.Results) == 0 {
		return "未找到搜索结果"
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("🔍 搜索结果（%s，耗时：%v）\n\n", resp.Engine, resp.Duration))

	for i, result := range resp.Results {
		sb.WriteString(fmt.Sprintf("%d. **%s**\n", i+1, result.Title))
		sb.WriteString(fmt.Sprintf("   🔗 %s\n", result.URL))
		if result.Snippet != "" {
			sb.WriteString(fmt.Sprintf("   📝 %s\n", result.Snippet))
		}
		if !result.PublishedAt.IsZero() {
			sb.WriteString(fmt.Sprintf("   📅 发布时间：%s\n", result.PublishedAt.Format("2006-01-02 15:04:05")))
		}
		if result.Score > 0 {
			sb.WriteString(fmt.Sprintf("   ⭐ 相关性：%.2f\n", result.Score))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// FormatCombinedResults 格式化多引擎综合搜索结果
func FormatCombinedResults(resp *CombinedSearchResponse) string {
	if resp == nil {
		return "未找到搜索结果"
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("🔍 综合搜索结果：%s\n\n", resp.Query))

	if len(resp.Responses) > 0 {
		sb.WriteString("📊 各引擎搜索情况：\n")
		for engineName, engineResp := range resp.Responses {
			sb.WriteString(fmt.Sprintf("  - %s：%d 条结果，耗时 %v\n", 
				engineName, len(engineResp.Results), engineResp.Duration))
		}
		sb.WriteString("\n")
	}

	if len(resp.Combined) > 0 {
		sb.WriteString("📋 综合结果（去重后）：\n\n")
		for i, result := range resp.Combined {
			sb.WriteString(fmt.Sprintf("%d. **%s** [%s]\n", i+1, result.Title, result.Source))
			sb.WriteString(fmt.Sprintf("   🔗 %s\n", result.URL))
			if result.Snippet != "" {
				sb.WriteString(fmt.Sprintf("   📝 %s\n", result.Snippet))
			}
			sb.WriteString("\n")
		}
	}

	if resp.Analysis != "" {
		sb.WriteString("💡 分析摘要：\n")
		sb.WriteString(resp.Analysis)
		sb.WriteString("\n")
	}

	return sb.String()
}
