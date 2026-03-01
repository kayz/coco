package cmd

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/kayz/coco/internal/agent"
	"github.com/kayz/coco/internal/ai"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var (
	modelBenchTimeout        int
	modelBenchDisableFailure bool
	modelBenchDisableFor     string
	modelBenchRoleFilters    []string

	modelToggleName   string
	modelToggleReason string
	modelToggleFor    string

	doctorModelsBench bool
)

type modelsFile struct {
	Models []*ai.ModelConfig `yaml:"models"`
}

type modelBenchResult struct {
	Model   *ai.ModelConfig
	Status  string
	Detail  string
	Latency time.Duration
}

var modelsCmd = &cobra.Command{
	Use:   "models",
	Short: "Model governance tools (status, bench, enable, disable)",
}

var modelStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show model status including off-shelf state",
	RunE: func(cmd *cobra.Command, args []string) error {
		reg, err := ai.LoadRegistry()
		if err != nil {
			return err
		}
		now := time.Now()
		fmt.Println("Model status:")
		for _, m := range reg.ListModels() {
			status := "enabled"
			note := ""
			if !m.IsEnabled() {
				status = "disabled"
				note = strings.TrimSpace(m.DisabledReason)
			} else if m.IsTemporarilyDisabled(now) {
				status = "off-shelf"
				note = fmt.Sprintf("until %s", strings.TrimSpace(m.DisabledUntil))
			}
			roles := "-"
			if len(m.Roles) > 0 {
				roles = strings.Join(m.Roles, ",")
			}
			if note != "" {
				fmt.Printf("- %s [%s] roles=%s (%s)\n", m.Name, status, roles, note)
			} else {
				fmt.Printf("- %s [%s] roles=%s\n", m.Name, status, roles)
			}
		}
		return nil
	},
}

var modelBenchCmd = &cobra.Command{
	Use:   "bench",
	Short: "Run a lightweight online health bench across models",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runModelsBench(modelBenchDisableFailure, modelBenchDisableFor, modelBenchRoleFilters)
	},
}

var modelDisableCmd = &cobra.Command{
	Use:   "disable",
	Short: "Disable/off-shelf a model",
	RunE: func(cmd *cobra.Command, args []string) error {
		if strings.TrimSpace(modelToggleName) == "" {
			return fmt.Errorf("--name is required")
		}
		return toggleModelEnabled(modelToggleName, false, modelToggleFor, modelToggleReason)
	},
}

var modelEnableCmd = &cobra.Command{
	Use:   "enable",
	Short: "Re-enable a model",
	RunE: func(cmd *cobra.Command, args []string) error {
		if strings.TrimSpace(modelToggleName) == "" {
			return fmt.Errorf("--name is required")
		}
		return toggleModelEnabled(modelToggleName, true, "", "")
	},
}

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Diagnostics commands",
}

var doctorModelsCmd = &cobra.Command{
	Use:   "models",
	Short: "Check model/provider configuration and optional online bench",
	RunE: func(cmd *cobra.Command, args []string) error {
		reg, err := ai.LoadRegistry()
		if err != nil {
			return err
		}
		fmt.Println("Doctor: models")
		models := reg.ListModels()
		if len(models) == 0 {
			return fmt.Errorf("no models found")
		}
		warn := 0
		for _, m := range models {
			provider, ok := reg.GetProvider(m.Provider)
			if !ok {
				warn++
				fmt.Printf("- WARN %s: provider %s not found\n", m.Name, m.Provider)
				continue
			}
			if len(provider.Keys()) == 0 {
				warn++
				fmt.Printf("- WARN %s: provider %s has empty api key\n", m.Name, m.Provider)
				continue
			}
			if !m.IsEnabled() {
				fmt.Printf("- INFO %s: disabled (%s)\n", m.Name, strings.TrimSpace(m.DisabledReason))
				continue
			}
			if m.IsTemporarilyDisabled(time.Now()) {
				fmt.Printf("- INFO %s: temporary off-shelf until %s\n", m.Name, strings.TrimSpace(m.DisabledUntil))
				continue
			}
			fmt.Printf("- OK   %s: provider=%s roles=%s\n", m.Name, m.Provider, strings.Join(defaultIfEmptySlice(m.Roles, "none"), ","))
		}
		if warn > 0 {
			fmt.Printf("Doctor summary: warnings=%d\n", warn)
		} else {
			fmt.Println("Doctor summary: no config issues found")
		}
		if doctorModelsBench {
			return runModelsBench(false, "", nil)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(modelsCmd)
	rootCmd.AddCommand(doctorCmd)
	doctorCmd.AddCommand(doctorModelsCmd)

	modelsCmd.AddCommand(modelStatusCmd)
	modelsCmd.AddCommand(modelBenchCmd)
	modelsCmd.AddCommand(modelDisableCmd)
	modelsCmd.AddCommand(modelEnableCmd)

	modelBenchCmd.Flags().IntVar(&modelBenchTimeout, "timeout", 12, "Per-model bench timeout in seconds")
	modelBenchCmd.Flags().BoolVar(&modelBenchDisableFailure, "disable-failures", false, "Disable failed models after bench")
	modelBenchCmd.Flags().StringVar(&modelBenchDisableFor, "disable-for", "24h", "Temporary off-shelf duration when --disable-failures is enabled (empty means permanently disabled)")
	modelBenchCmd.Flags().StringSliceVar(&modelBenchRoleFilters, "role", nil, "Only bench models with given role (repeatable): primary|cron|expert")

	modelDisableCmd.Flags().StringVar(&modelToggleName, "name", "", "Model name")
	modelDisableCmd.Flags().StringVar(&modelToggleReason, "reason", "manual disable", "Disable reason")
	modelDisableCmd.Flags().StringVar(&modelToggleFor, "for", "", "Temporary off-shelf duration, e.g. 12h/24h/7d")

	modelEnableCmd.Flags().StringVar(&modelToggleName, "name", "", "Model name")

	doctorModelsCmd.Flags().BoolVar(&doctorModelsBench, "bench", false, "Run online model bench after config doctor")
}

func runModelsBench(disableFailures bool, disableFor string, roleFilters []string) error {
	reg, err := ai.LoadRegistry()
	if err != nil {
		return err
	}

	targets := filterModelsByRoles(reg.ListModels(), roleFilters)
	if len(targets) == 0 {
		return fmt.Errorf("no models match current filters")
	}

	results := make([]modelBenchResult, 0, len(targets))
	for _, model := range targets {
		results = append(results, benchOneModel(reg, model))
	}

	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Status == results[j].Status {
			return results[i].Model.Name < results[j].Model.Name
		}
		return results[i].Status < results[j].Status
	})

	okCount := 0
	failNames := make([]string, 0)
	fmt.Println("Model bench result:")
	for _, r := range results {
		if r.Status == "PASS" {
			okCount++
		} else if r.Model != nil {
			failNames = append(failNames, r.Model.Name)
		}
		lat := ""
		if r.Latency > 0 {
			lat = fmt.Sprintf(" (%s)", r.Latency.Truncate(time.Millisecond))
		}
		fmt.Printf("- %s: %s%s - %s\n", r.Model.Name, r.Status, lat, r.Detail)
	}
	fmt.Printf("Summary: pass=%d fail=%d\n", okCount, len(results)-okCount)

	if disableFailures && len(failNames) > 0 {
		fmt.Printf("Applying temporary off-shelf to failed models: %s\n", strings.Join(failNames, ", "))
		for _, name := range failNames {
			if err := toggleModelEnabled(name, false, disableFor, "auto bench failure"); err != nil {
				fmt.Printf("- WARN failed to disable %s: %v\n", name, err)
			}
		}
	}

	return nil
}

func benchOneModel(reg *ai.Registry, model *ai.ModelConfig) modelBenchResult {
	result := modelBenchResult{Model: model, Status: "FAIL", Detail: "unknown"}
	if model == nil {
		result.Detail = "nil model"
		return result
	}
	now := time.Now()
	if !model.IsAvailable(now) {
		result.Status = "SKIP"
		if !model.IsEnabled() {
			result.Detail = "disabled"
		} else {
			result.Detail = "temporarily off-shelf"
		}
		return result
	}
	providerCfg, ok := reg.GetProvider(model.Provider)
	if !ok {
		result.Detail = "provider not found"
		return result
	}
	keys := providerCfg.Keys()
	if len(keys) == 0 {
		result.Detail = "provider has no api key"
		return result
	}

	p, err := createBenchProvider(providerCfg, model.Code, keys[0])
	if err != nil {
		result.Detail = err.Error()
		return result
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(modelBenchTimeout)*time.Second)
	defer cancel()
	start := time.Now()
	resp, err := p.Chat(ctx, agent.ChatRequest{
		Messages: []agent.Message{
			{Role: "user", Content: "ping"},
		},
		SystemPrompt: "Reply with one short line.",
		MaxTokens:    64,
	})
	result.Latency = time.Since(start)
	if err != nil {
		result.Detail = err.Error()
		return result
	}
	content := strings.TrimSpace(resp.Content)
	if content == "" {
		result.Detail = "empty response"
		return result
	}
	result.Status = "PASS"
	result.Detail = "ok"
	return result
}

func createBenchProvider(cfg *ai.ProviderConfig, modelCode, apiKey string) (agent.Provider, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.Type)) {
	case "deepseek":
		return agent.NewDeepSeekProvider(agent.DeepSeekConfig{
			APIKey:  apiKey,
			BaseURL: cfg.BaseURL,
			Model:   modelCode,
		})
	case "qwen", "qianwen", "tongyi":
		return agent.NewQwenProvider(agent.QwenConfig{
			APIKey:  apiKey,
			BaseURL: cfg.BaseURL,
			Model:   modelCode,
		})
	case "kimi", "moonshot":
		return agent.NewKimiProvider(agent.KimiConfig{
			APIKey:  apiKey,
			BaseURL: cfg.BaseURL,
			Model:   modelCode,
		})
	case "claude", "anthropic":
		return agent.NewClaudeProvider(agent.ClaudeConfig{
			APIKey:  apiKey,
			BaseURL: cfg.BaseURL,
			Model:   modelCode,
		})
	default:
		baseURL := strings.TrimSpace(cfg.BaseURL)
		if baseURL == "" {
			baseURL = "https://api.openai.com/v1"
		}
		return agent.NewOpenAICompatProvider(agent.OpenAICompatConfig{
			ProviderName: strings.TrimSpace(cfg.Type),
			APIKey:       apiKey,
			BaseURL:      baseURL,
			Model:        modelCode,
			DefaultURL:   baseURL,
			DefaultModel: modelCode,
		})
	}
}

func filterModelsByRoles(models []*ai.ModelConfig, roles []string) []*ai.ModelConfig {
	if len(roles) == 0 {
		return models
	}
	roleSet := make(map[string]struct{}, len(roles))
	for _, role := range roles {
		role = strings.ToLower(strings.TrimSpace(role))
		if role != "" {
			roleSet[role] = struct{}{}
		}
	}
	filtered := make([]*ai.ModelConfig, 0, len(models))
	for _, model := range models {
		if model == nil {
			continue
		}
		for role := range roleSet {
			if model.HasRole(role) {
				filtered = append(filtered, model)
				break
			}
		}
	}
	return filtered
}

func toggleModelEnabled(modelName string, enabled bool, disableFor string, reason string) error {
	filePath := ai.ModelsPath()
	data, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}
	var mf modelsFile
	if err := yaml.Unmarshal(data, &mf); err != nil {
		return err
	}

	found := false
	for _, m := range mf.Models {
		if m == nil || !strings.EqualFold(strings.TrimSpace(m.Name), strings.TrimSpace(modelName)) {
			continue
		}
		found = true
		m.Enabled = boolPtr(enabled)
		if enabled {
			m.DisabledUntil = ""
			m.DisabledReason = ""
		} else {
			m.DisabledReason = strings.TrimSpace(reason)
			d := strings.TrimSpace(disableFor)
			if d != "" {
				dur, err := time.ParseDuration(d)
				if err != nil {
					return fmt.Errorf("invalid --for duration %q: %w", d, err)
				}
				m.DisabledUntil = time.Now().Add(dur).Format(time.RFC3339)
			} else {
				m.DisabledUntil = ""
			}
		}
		break
	}
	if !found {
		return fmt.Errorf("model %s not found", modelName)
	}

	out, err := yaml.Marshal(&mf)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filePath, out, 0644); err != nil {
		return err
	}
	if enabled {
		fmt.Printf("Model enabled: %s\n", modelName)
	} else {
		fmt.Printf("Model disabled/off-shelf: %s\n", modelName)
	}
	return nil
}

func defaultIfEmptySlice(in []string, fallback string) []string {
	if len(in) == 0 {
		return []string{fallback}
	}
	return in
}

func boolPtr(v bool) *bool {
	return &v
}
