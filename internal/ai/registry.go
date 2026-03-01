package ai

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

var (
	exeDirCache string
)

func getExecutableDir() string {
	if exeDirCache != "" {
		return exeDirCache
	}
	execPath, err := os.Executable()
	if err != nil {
		exeDirCache = "."
		return exeDirCache
	}
	execPath, err = filepath.EvalSymlinks(execPath)
	if err != nil {
		exeDirCache = "."
		return exeDirCache
	}
	exeDirCache = filepath.Dir(execPath)
	return exeDirCache
}

func ProvidersPath() string {
	exeDir := getExecutableDir()
	return filepath.Join(exeDir, ".coco", "providers.yaml")
}

func ModelsPath() string {
	exeDir := getExecutableDir()
	return filepath.Join(exeDir, ".coco", "models.yaml")
}

type ProviderConfig struct {
	Name    string   `yaml:"name"`
	Type    string   `yaml:"type"`
	BaseURL string   `yaml:"base_url"`
	APIKey  string   `yaml:"api_key"`
	APIKeys []string `yaml:"api_keys,omitempty"`
}

type ModelConfig struct {
	Name           string   `yaml:"name"`
	Code           string   `yaml:"code"`
	Provider       string   `yaml:"provider"`
	Intellect      string   `yaml:"intellect"`
	Speed          string   `yaml:"speed"`
	Cost           string   `yaml:"cost"`
	Skills         []string `yaml:"skills"`
	Roles          []string `yaml:"roles,omitempty"`
	Enabled        *bool    `yaml:"enabled,omitempty"`
	DisabledUntil  string   `yaml:"disabled_until,omitempty"`
	DisabledReason string   `yaml:"disabled_reason,omitempty"`
}

func (m *ModelConfig) IntellectText() string {
	switch m.Intellect {
	case "full":
		return "满分"
	case "excellent":
		return "优秀"
	case "good":
		return "良好"
	case "usable":
		return "可用"
	default:
		return m.Intellect
	}
}

func (m *ModelConfig) SpeedText() string {
	switch m.Speed {
	case "fast":
		return "快"
	case "medium":
		return "中"
	case "slow":
		return "慢"
	default:
		return m.Speed
	}
}

func (m *ModelConfig) CostText() string {
	switch m.Cost {
	case "expensive":
		return "贵"
	case "high":
		return "高"
	case "medium":
		return "中"
	case "low":
		return "低"
	case "free":
		return "免费"
	default:
		return m.Cost
	}
}

func (m *ModelConfig) SkillsText() string {
	if len(m.Skills) == 0 {
		return "无"
	}
	skillNames := make([]string, 0, len(m.Skills))
	for _, s := range m.Skills {
		switch s {
		case "thinking":
			skillNames = append(skillNames, "思维链")
		case "multimodal":
			skillNames = append(skillNames, "多模态")
		case "asr":
			skillNames = append(skillNames, "语音识别")
		case "t2i":
			skillNames = append(skillNames, "文生图")
		case "i2v":
			skillNames = append(skillNames, "图生视频")
		case "local":
			skillNames = append(skillNames, "本地运行")
		default:
			skillNames = append(skillNames, s)
		}
	}
	return strings.Join(skillNames, "、")
}

func (m *ModelConfig) IntellectRank() int {
	switch m.Intellect {
	case "full":
		return 4
	case "excellent":
		return 3
	case "good":
		return 2
	case "usable":
		return 1
	default:
		return 0
	}
}

func (m *ModelConfig) HasSkill(skill string) bool {
	if m == nil {
		return false
	}
	skill = strings.ToLower(strings.TrimSpace(skill))
	if skill == "" {
		return false
	}
	for _, s := range m.Skills {
		if strings.EqualFold(strings.TrimSpace(s), skill) {
			return true
		}
	}
	return false
}

func (m *ModelConfig) HasRole(role string) bool {
	if m == nil {
		return false
	}
	role = strings.ToLower(strings.TrimSpace(role))
	if role == "" {
		return false
	}
	for _, r := range m.Roles {
		if strings.EqualFold(strings.TrimSpace(r), role) {
			return true
		}
	}
	return false
}

func (m *ModelConfig) IsEnabled() bool {
	if m == nil {
		return false
	}
	if m.Enabled == nil {
		return true
	}
	return *m.Enabled
}

func (m *ModelConfig) IsTemporarilyDisabled(now time.Time) bool {
	if m == nil {
		return false
	}
	until := strings.TrimSpace(m.DisabledUntil)
	if until == "" {
		return false
	}
	t, err := time.Parse(time.RFC3339, until)
	if err != nil {
		return false
	}
	return now.Before(t)
}

func (m *ModelConfig) IsAvailable(now time.Time) bool {
	if !m.IsEnabled() {
		return false
	}
	if m.IsTemporarilyDisabled(now) {
		return false
	}
	return true
}

func (p *ProviderConfig) Keys() []string {
	if p == nil {
		return nil
	}
	keys := make([]string, 0, len(p.APIKeys)+1)
	for _, key := range p.APIKeys {
		key = strings.TrimSpace(key)
		if key != "" {
			keys = append(keys, key)
		}
	}
	if len(keys) == 0 {
		if key := strings.TrimSpace(p.APIKey); key != "" {
			keys = append(keys, key)
		}
	}
	return keys
}

type Registry struct {
	providers  map[string]*ProviderConfig
	models     map[string]*ModelConfig
	modelOrder []string
}

type providersFile struct {
	Providers []*ProviderConfig `yaml:"providers"`
}

type modelsFile struct {
	Models []*ModelConfig `yaml:"models"`
}

func LoadRegistry() (*Registry, error) {
	r := &Registry{
		providers:  make(map[string]*ProviderConfig),
		models:     make(map[string]*ModelConfig),
		modelOrder: make([]string, 0),
	}

	providersPath := ProvidersPath()
	providersData, err := os.ReadFile(providersPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read providers.yaml: %w", err)
	}

	var pf providersFile
	if err := yaml.Unmarshal(providersData, &pf); err != nil {
		return nil, fmt.Errorf("failed to parse providers.yaml: %w", err)
	}

	for _, p := range pf.Providers {
		r.providers[p.Name] = p
	}

	modelsPath := ModelsPath()
	modelsData, err := os.ReadFile(modelsPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read models.yaml: %w", err)
	}

	var mf modelsFile
	if err := yaml.Unmarshal(modelsData, &mf); err != nil {
		return nil, fmt.Errorf("failed to parse models.yaml: %w", err)
	}

	for _, m := range mf.Models {
		if _, exists := r.models[m.Name]; !exists {
			r.modelOrder = append(r.modelOrder, m.Name)
		}
		r.models[m.Name] = m
	}

	if len(r.models) == 0 {
		return nil, fmt.Errorf("no models found in models.yaml")
	}

	return r, nil
}

func (r *Registry) GetProvider(name string) (*ProviderConfig, bool) {
	p, ok := r.providers[name]
	return p, ok
}

func (r *Registry) GetModel(name string) (*ModelConfig, bool) {
	m, ok := r.models[name]
	return m, ok
}

func (r *Registry) ListModels() []*ModelConfig {
	models := make([]*ModelConfig, 0, len(r.modelOrder))
	for _, name := range r.modelOrder {
		if m, ok := r.models[name]; ok {
			models = append(models, m)
		}
	}
	return models
}

func (r *Registry) GetDefaultModel() *ModelConfig {
	for _, name := range r.modelOrder {
		if m, ok := r.models[name]; ok {
			return m
		}
	}
	return nil
}
