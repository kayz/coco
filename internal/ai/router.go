package ai

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	RolePrimary = "primary"
	RoleCron    = "cron"
	RoleExpert  = "expert"
)

type ModelRouter struct {
	registry        *Registry
	currentModel    *ModelConfig
	failoverStats   map[string]*ModelStats
	cooldowns       map[string]time.Time
	quarantines     map[string]time.Time
	cooldownTime    time.Duration
	quarantineTime  time.Duration
	failoverAfter   int
	quarantineAfter int
	mu              sync.RWMutex
}

type ModelStats struct {
	successCount      int
	failureCount      int
	consecutiveFailed int
	lastSuccess       time.Time
	lastFailure       time.Time
}

func NewModelRouter(registry *Registry, cooldownTime time.Duration) *ModelRouter {
	r := &ModelRouter{
		registry:        registry,
		failoverStats:   make(map[string]*ModelStats),
		cooldowns:       make(map[string]time.Time),
		quarantines:     make(map[string]time.Time),
		cooldownTime:    cooldownTime,
		quarantineTime:  maxDuration(30*time.Minute, cooldownTime*6),
		failoverAfter:   3,
		quarantineAfter: 6,
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

func normalizeRole(role string) string {
	role = strings.ToLower(strings.TrimSpace(role))
	switch role {
	case RolePrimary, RoleCron, RoleExpert:
		return role
	default:
		return RolePrimary
	}
}

func speedRank(speed string) int {
	switch strings.ToLower(strings.TrimSpace(speed)) {
	case "fast":
		return 3
	case "medium":
		return 2
	case "slow":
		return 1
	default:
		return 0
	}
}

func costRank(cost string) int {
	switch strings.ToLower(strings.TrimSpace(cost)) {
	case "free":
		return 0
	case "low":
		return 1
	case "medium":
		return 2
	case "high":
		return 3
	case "expensive":
		return 4
	default:
		return 5
	}
}

func sameClassModel(a, b *ModelConfig) bool {
	if a == nil || b == nil {
		return true
	}
	if a.HasSkill("multimodal") != b.HasSkill("multimodal") {
		return false
	}
	if a.HasSkill("thinking") && !b.HasSkill("thinking") {
		return false
	}
	return true
}

func (r *ModelRouter) roleModelsUnlocked(role string) []*ModelConfig {
	role = normalizeRole(role)
	all := r.registry.ListModels()
	if len(all) == 0 {
		return nil
	}
	now := time.Now()

	explicit := make([]*ModelConfig, 0, len(all))
	for _, m := range all {
		if !r.isModelAvailableUnlocked(m, now) {
			continue
		}
		if m.HasRole(role) {
			explicit = append(explicit, m)
		}
	}
	if len(explicit) > 0 {
		return explicit
	}

	derived := make([]*ModelConfig, 0, len(all))
	for _, m := range all {
		if !r.isModelAvailableUnlocked(m, now) {
			continue
		}
		derived = append(derived, m)
	}
	if len(derived) == 0 {
		return nil
	}
	switch role {
	case RoleCron:
		sort.SliceStable(derived, func(i, j int) bool {
			a, b := derived[i], derived[j]
			if costRank(a.Cost) != costRank(b.Cost) {
				return costRank(a.Cost) < costRank(b.Cost)
			}
			if speedRank(a.Speed) != speedRank(b.Speed) {
				return speedRank(a.Speed) > speedRank(b.Speed)
			}
			if a.IntellectRank() != b.IntellectRank() {
				return a.IntellectRank() > b.IntellectRank()
			}
			return a.Name < b.Name
		})
	case RoleExpert:
		sort.SliceStable(derived, func(i, j int) bool {
			a, b := derived[i], derived[j]
			if a.IntellectRank() != b.IntellectRank() {
				return a.IntellectRank() > b.IntellectRank()
			}
			if a.HasSkill("thinking") != b.HasSkill("thinking") {
				return a.HasSkill("thinking")
			}
			if a.HasSkill("multimodal") != b.HasSkill("multimodal") {
				return a.HasSkill("multimodal")
			}
			if costRank(a.Cost) != costRank(b.Cost) {
				return costRank(a.Cost) < costRank(b.Cost)
			}
			return a.Name < b.Name
		})
	default:
		// primary: preserve declared order.
	}
	return derived
}

func (r *ModelRouter) PickModelForRole(role string) *ModelConfig {
	r.mu.RLock()
	defer r.mu.RUnlock()
	role = normalizeRole(role)
	now := time.Now()

	if role == RolePrimary && r.currentModel != nil && r.isModelAvailableUnlocked(r.currentModel, now) && !r.IsInCooldown(r.currentModel.Name) {
		return r.currentModel
	}

	candidates := r.roleModelsUnlocked(role)
	for _, c := range candidates {
		if !r.IsInCooldown(c.Name) {
			return c
		}
	}
	if len(candidates) > 0 {
		return candidates[0]
	}
	return nil
}

func (r *ModelRouter) ListModelsForRole(role string) []*ModelConfig {
	r.mu.RLock()
	defer r.mu.RUnlock()
	models := r.roleModelsUnlocked(role)
	out := make([]*ModelConfig, 0, len(models))
	out = append(out, models...)
	return out
}

func (r *ModelRouter) RecordSuccess(model *ModelConfig) {
	if model == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	stats, ok := r.failoverStats[model.Name]
	if !ok {
		stats = &ModelStats{}
		r.failoverStats[model.Name] = stats
	}
	stats.successCount++
	stats.consecutiveFailed = 0
	stats.lastSuccess = time.Now()
}

func (r *ModelRouter) RecordFailure(model *ModelConfig) {
	if model == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	stats, ok := r.failoverStats[model.Name]
	if !ok {
		stats = &ModelStats{}
		r.failoverStats[model.Name] = stats
	}
	stats.failureCount++
	stats.consecutiveFailed++
	stats.lastFailure = time.Now()

	if stats.consecutiveFailed >= r.failoverAfter {
		r.cooldowns[model.Name] = time.Now().Add(r.cooldownTime)
	}
	if stats.consecutiveFailed >= r.quarantineAfter {
		r.quarantines[model.Name] = time.Now().Add(r.quarantineTime)
	}
}

func (r *ModelRouter) ConsecutiveFailures(modelName string) int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	stats, ok := r.failoverStats[modelName]
	if !ok {
		return 0
	}
	return stats.consecutiveFailed
}

func (r *ModelRouter) ShouldRotatePrimary(model *ModelConfig) bool {
	if model == nil {
		return false
	}
	return r.ConsecutiveFailures(model.Name) >= r.failoverAfter
}

func (r *ModelRouter) FailoverForRole(role string, failed *ModelConfig) (*ModelConfig, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	role = normalizeRole(role)

	candidates := r.roleModelsUnlocked(role)
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no models available")
	}

	filtered := make([]*ModelConfig, 0, len(candidates))
	for _, m := range candidates {
		if failed != nil && m.Name == failed.Name {
			continue
		}
		if r.IsInCooldown(m.Name) {
			continue
		}
		filtered = append(filtered, m)
	}
	if len(filtered) == 0 {
		return nil, fmt.Errorf("no available models for failover")
	}

	if failed != nil {
		sameClass := make([]*ModelConfig, 0, len(filtered))
		for _, m := range filtered {
			if sameClassModel(failed, m) {
				sameClass = append(sameClass, m)
			}
		}
		if len(sameClass) > 0 {
			filtered = sameClass
		}
	}

	sort.SliceStable(filtered, func(i, j int) bool {
		a, b := filtered[i], filtered[j]
		// cron failover prefers cheap+fast
		if role == RoleCron {
			if costRank(a.Cost) != costRank(b.Cost) {
				return costRank(a.Cost) < costRank(b.Cost)
			}
			if speedRank(a.Speed) != speedRank(b.Speed) {
				return speedRank(a.Speed) > speedRank(b.Speed)
			}
			return a.IntellectRank() > b.IntellectRank()
		}
		// primary/expert failover prefers capability first.
		if a.IntellectRank() != b.IntellectRank() {
			return a.IntellectRank() > b.IntellectRank()
		}
		if a.HasSkill("thinking") != b.HasSkill("thinking") {
			return a.HasSkill("thinking")
		}
		if speedRank(a.Speed) != speedRank(b.Speed) {
			return speedRank(a.Speed) > speedRank(b.Speed)
		}
		return costRank(a.Cost) < costRank(b.Cost)
	})

	return filtered[0], nil
}

func (r *ModelRouter) Failover() (*ModelConfig, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	next, err := r.failoverUnlocked(RolePrimary, r.currentModel)
	if err != nil {
		return nil, err
	}
	r.currentModel = next
	return next, nil
}

func (r *ModelRouter) failoverUnlocked(role string, failed *ModelConfig) (*ModelConfig, error) {
	role = normalizeRole(role)
	candidates := r.roleModelsUnlocked(role)
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no models available")
	}
	var filtered []*ModelConfig
	for _, m := range candidates {
		if failed != nil && m.Name == failed.Name {
			continue
		}
		if r.IsInCooldown(m.Name) {
			continue
		}
		filtered = append(filtered, m)
	}
	if len(filtered) == 0 {
		return nil, fmt.Errorf("no available models for failover")
	}
	if failed != nil {
		var same []*ModelConfig
		for _, m := range filtered {
			if sameClassModel(failed, m) {
				same = append(same, m)
			}
		}
		if len(same) > 0 {
			filtered = same
		}
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		a, b := filtered[i], filtered[j]
		if a.IntellectRank() != b.IntellectRank() {
			return a.IntellectRank() > b.IntellectRank()
		}
		if a.HasSkill("thinking") != b.HasSkill("thinking") {
			return a.HasSkill("thinking")
		}
		if speedRank(a.Speed) != speedRank(b.Speed) {
			return speedRank(a.Speed) > speedRank(b.Speed)
		}
		return costRank(a.Cost) < costRank(b.Cost)
	})
	return filtered[0], nil
}

func (r *ModelRouter) IsInCooldown(modelName string) bool {
	cooldownUntil, ok := r.cooldowns[modelName]
	if !ok {
		return false
	}
	return time.Now().Before(cooldownUntil)
}

func (r *ModelRouter) IsQuarantined(modelName string) bool {
	quarantineUntil, ok := r.quarantines[modelName]
	if !ok {
		return false
	}
	return time.Now().Before(quarantineUntil)
}

func (r *ModelRouter) isModelAvailableUnlocked(model *ModelConfig, now time.Time) bool {
	if model == nil {
		return false
	}
	if !model.IsAvailable(now) {
		return false
	}
	quarantineUntil, ok := r.quarantines[model.Name]
	if ok && now.Before(quarantineUntil) {
		return false
	}
	return true
}

func maxDuration(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
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
	sb.WriteString("1. 主对话：优先保持稳定主模型，不因瞬时抖动频繁轮换\n")
	sb.WriteString("2. 简单任务（cron/心跳）：优先\"低费用\"且\"快\"的模型\n")
	sb.WriteString("3. 专家任务：优先专家池（高智力/思维链）\n")
	sb.WriteString("4. 图片/文档理解：用支持\"多模态\"的模型\n")
	sb.WriteString("5. 你可以通过 `ai.switch_model` 工具切换模型\n")
	sb.WriteString("6. 如果当前模型失败，会自动 failover 到下一个可用模型\n")

	return sb.String()
}
