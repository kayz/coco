package ai

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

type ModelRouter struct {
	registry      *Registry
	currentModel  *ModelConfig
	failoverStats map[string]*ModelStats
	cooldowns     map[string]time.Time
	cooldownTime  time.Duration
	mu            sync.RWMutex
}

type ModelStats struct {
	successCount int
	failureCount int
	lastSuccess  time.Time
	lastFailure  time.Time
}

func NewModelRouter(registry *Registry, cooldownTime time.Duration) *ModelRouter {
	r := &ModelRouter{
		registry:      registry,
		failoverStats: make(map[string]*ModelStats),
		cooldowns:     make(map[string]time.Time),
		cooldownTime:  cooldownTime,
	}

	defaultModel := registry.GetDefaultModel()
	if defaultModel != nil {
		r.currentModel = defaultModel
	}

	return r
}

func (r *ModelRouter) ListModels() []*ModelConfig {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.registry.ListModels()
}

func (r *ModelRouter) GetCurrentModel() *ModelConfig {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.currentModel
}

func (r *ModelRouter) SwitchToModel(name string, force bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	model, ok := r.registry.GetModel(name)
	if !ok {
		return fmt.Errorf("model not found: %s", name)
	}

	if !force && r.IsInCooldown(name) {
		return fmt.Errorf("model %s is in cooldown", name)
	}

	r.currentModel = model
	return nil
}



func (r *ModelRouter) RecordSuccess(model *ModelConfig) {
	r.mu.Lock()
	defer r.mu.Unlock()

	stats, ok := r.failoverStats[model.Name]
	if !ok {
		stats = &ModelStats{}
		r.failoverStats[model.Name] = stats
	}
	stats.successCount++
	stats.lastSuccess = time.Now()
}

func (r *ModelRouter) RecordFailure(model *ModelConfig) {
	r.mu.Lock()
	defer r.mu.Unlock()

	stats, ok := r.failoverStats[model.Name]
	if !ok {
		stats = &ModelStats{}
		r.failoverStats[model.Name] = stats
	}
	stats.failureCount++
	stats.lastFailure = time.Now()

	r.cooldowns[model.Name] = time.Now().Add(r.cooldownTime)
}

func (r *ModelRouter) Failover() (*ModelConfig, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	allModels := r.registry.ListModels()
	if len(allModels) == 0 {
		return nil, fmt.Errorf("no models available")
	}

	currentIntellectRank := 0
	if r.currentModel != nil {
		currentIntellectRank = r.currentModel.IntellectRank()
	}

	type candidate struct {
		model          *ModelConfig
		intellectRank  int
		speedMatch     bool
		failureRank    int
	}

	var candidates []candidate
	for _, m := range allModels {
		if r.IsInCooldown(m.Name) {
			continue
		}

		stats := r.failoverStats[m.Name]
		failureRank := 0
		if stats != nil {
			failureRank = stats.failureCount
		}

		candidates = append(candidates, candidate{
			model:         m,
			intellectRank: m.IntellectRank(),
			speedMatch:    r.currentModel != nil && m.Speed == r.currentModel.Speed,
			failureRank:   failureRank,
		})
	}

	if len(candidates) == 0 {
		return nil, fmt.Errorf("no available models for failover")
	}

	sort.Slice(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]

		aDistance := abs(a.intellectRank - currentIntellectRank)
		bDistance := abs(b.intellectRank - currentIntellectRank)
		if aDistance != bDistance {
			return aDistance < bDistance
		}

		if a.speedMatch != b.speedMatch {
			return a.speedMatch
		}

		if a.intellectRank != b.intellectRank {
			return a.intellectRank > b.intellectRank
		}

		return a.failureRank < b.failureRank
	})

	r.currentModel = candidates[0].model
	return r.currentModel, nil
}

func (r *ModelRouter) IsInCooldown(modelName string) bool {
	cooldownUntil, ok := r.cooldowns[modelName]
	if !ok {
		return false
	}
	return time.Now().Before(cooldownUntil)
}

func (r *ModelRouter) FormatModelsPrompt() string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var sb strings.Builder
	sb.WriteString("## 可用模型\n\n")
	sb.WriteString("以下是你可以使用的 AI 模型及其能力：\n\n")

	for _, m := range r.registry.ListModels() {
		sb.WriteString(fmt.Sprintf("- %s\n", m.Name))
		sb.WriteString(fmt.Sprintf("  - 智力：%s\n", m.IntellectText()))
		sb.WriteString(fmt.Sprintf("  - 速度：%s\n", m.SpeedText()))
		sb.WriteString(fmt.Sprintf("  - 费用：%s\n", m.CostText()))
		sb.WriteString(fmt.Sprintf("  - 能力：%s\n", m.SkillsText()))
		sb.WriteString("\n")
	}

	sb.WriteString("## 选择模型原则\n\n")
	sb.WriteString("1. 普通聊天：优先用\"优秀\"或\"满分\"模型，需要多模态时用支持多模态的\n")
	sb.WriteString("2. 简单任务（cron/心跳）：用\"快\"且\"低费用\"的模型\n")
	sb.WriteString("3. 复杂思考：用带\"思维链\"能力的模型\n")
	sb.WriteString("4. 图片/文档理解：用支持\"多模态\"的模型\n")
	sb.WriteString("5. 你可以通过 `ai.switch_model` 工具切换模型\n")
	sb.WriteString("6. 如果当前模型失败，会自动 failover 到下一个可用模型\n")

	return sb.String()
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
