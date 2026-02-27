package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/kayz/coco/internal/ai"
	"github.com/kayz/coco/internal/config"
	"github.com/kayz/coco/internal/service"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var (
	onboardMode           string
	onboardNonInteractive bool
	onboardSetValues      []string
	onboardSkipService    bool
)

var onboardCmd = &cobra.Command{
	Use:   "onboard",
	Short: "Interactive setup for relay/keeper/both",
	Long: `Interactive setup for relay/keeper/both.

The wizard writes:
  - .coco.yaml              Runtime config (mode, relay, keeper, platform creds)
  - .coco/providers.yaml    AI provider config
  - .coco/models.yaml       AI model config

It is intentionally step-based so new onboarding sections can be added later
without breaking existing flows.`,
	RunE: runOnboard,
}

func init() {
	rootCmd.AddCommand(onboardCmd)

	onboardCmd.Flags().StringVar(&onboardMode, "mode", "", "Mode: relay, keeper, both")
	onboardCmd.Flags().BoolVar(&onboardNonInteractive, "non-interactive", false, "Fail if required values are missing instead of prompting")
	onboardCmd.Flags().StringArrayVar(&onboardSetValues, "set", nil, "Pre-fill answers as key=value (repeatable)")
	onboardCmd.Flags().BoolVar(&onboardSkipService, "skip-service", false, "Skip service installation prompts")
}

type onboardQuestion struct {
	Key       string
	Prompt    string
	Required  bool
	Default   func(*onboardState) string
	Validate  func(string, *onboardState) error
	Condition func(*onboardState) bool
}

type onboardStep struct {
	Name      string
	Questions []onboardQuestion
	Apply     func(*onboardState) error
}

type onboardState struct {
	cfg            *config.Config
	mode           string
	answers        map[string]string
	prefill        map[string]string
	reader         *bufio.Reader
	nonInteractive bool
}

type providerTemplate struct {
	Name           string
	Type           string
	DefaultBaseURL string
	PrimaryModel   modelTemplate
	FallbackModel  string
}

type modelTemplate struct {
	Code      string
	Intellect string
	Speed     string
	Cost      string
	Skills    []string
}

var providerTemplates = map[string]providerTemplate{
	"deepseek": {
		Name:           "deepseek",
		Type:           "deepseek",
		DefaultBaseURL: "https://api.deepseek.com/v1",
		PrimaryModel:   modelTemplate{Code: "deepseek-chat", Intellect: "excellent", Speed: "fast", Cost: "medium"},
		FallbackModel:  "deepseek-reasoner",
	},
	"qwen": {
		Name:           "qwen",
		Type:           "qwen",
		DefaultBaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1",
		PrimaryModel:   modelTemplate{Code: "qwen-plus", Intellect: "excellent", Speed: "fast", Cost: "medium"},
		FallbackModel:  "qwen-turbo",
	},
	"openai": {
		Name:           "openai",
		Type:           "openai",
		DefaultBaseURL: "https://api.openai.com/v1",
		PrimaryModel:   modelTemplate{Code: "gpt-4o", Intellect: "full", Speed: "fast", Cost: "high", Skills: []string{"multimodal", "thinking"}},
		FallbackModel:  "gpt-4o-mini",
	},
	"claude": {
		Name:           "claude",
		Type:           "claude",
		DefaultBaseURL: "https://api.anthropic.com/v1",
		PrimaryModel:   modelTemplate{Code: "claude-sonnet-4-20250514", Intellect: "full", Speed: "fast", Cost: "high", Skills: []string{"multimodal", "thinking"}},
		FallbackModel:  "claude-3-5-sonnet-20241022",
	},
	"kimi": {
		Name:           "kimi",
		Type:           "kimi",
		DefaultBaseURL: "https://api.moonshot.cn/v1",
		PrimaryModel:   modelTemplate{Code: "moonshot-v1-8k", Intellect: "good", Speed: "fast", Cost: "low"},
		FallbackModel:  "moonshot-v1-32k",
	},
}

type providersYAML struct {
	Providers []providerYAML `yaml:"providers"`
}

type providerYAML struct {
	Name    string `yaml:"name"`
	Type    string `yaml:"type"`
	BaseURL string `yaml:"base_url"`
	APIKey  string `yaml:"api_key"`
}

type modelsYAML struct {
	Models []modelYAML `yaml:"models"`
}

type modelYAML struct {
	Name      string   `yaml:"name"`
	Code      string   `yaml:"code"`
	Provider  string   `yaml:"provider"`
	Intellect string   `yaml:"intellect"`
	Speed     string   `yaml:"speed"`
	Cost      string   `yaml:"cost"`
	Skills    []string `yaml:"skills"`
}

func runOnboard(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		cfg = config.DefaultConfig()
	}

	prefill, err := parseSetValues(onboardSetValues)
	if err != nil {
		return err
	}

	state := &onboardState{
		cfg:            cfg,
		answers:        make(map[string]string),
		prefill:        prefill,
		reader:         bufio.NewReader(os.Stdin),
		nonInteractive: onboardNonInteractive,
	}

	steps := []onboardStep{
		modeStep(),
		aiStep(),
		modeConfigStep(),
	}

	fmt.Println("=== coco onboard ===")

	for _, step := range steps {
		if err := runOnboardStep(state, step); err != nil {
			return fmt.Errorf("%s step failed: %w", step.Name, err)
		}
	}

	if err := state.cfg.Save(); err != nil {
		return fmt.Errorf("failed to save .coco.yaml: %w", err)
	}

	if err := writeAIRegistry(state); err != nil {
		return err
	}

	fmt.Println()
	fmt.Printf("Mode configured: %s\n", state.mode)
	fmt.Printf("Config saved: %s\n", config.ConfigPath())
	fmt.Printf("Providers:    %s\n", ai.ProvidersPath())
	fmt.Printf("Models:       %s\n", ai.ModelsPath())

	if !onboardSkipService {
		if err := maybeInstallService(state); err != nil {
			return err
		}
	}

	fmt.Println("Onboarding complete.")
	return nil
}

func modeStep() onboardStep {
	return onboardStep{
		Name: "mode",
		Questions: []onboardQuestion{
			{
				Key:      "mode",
				Prompt:   "Select mode (relay/keeper/both)",
				Required: true,
				Default: func(s *onboardState) string {
					if onboardMode != "" {
						return onboardMode
					}
					if s.cfg.Mode != "" {
						return s.cfg.Mode
					}
					return "relay"
				},
				Validate: validateModeValue,
			},
		},
		Apply: func(s *onboardState) error {
			mode, err := service.ValidateMode(s.answers["mode"])
			if err != nil {
				return err
			}
			s.mode = mode
			s.cfg.Mode = mode
			return nil
		},
	}
}

func aiStep() onboardStep {
	return onboardStep{
		Name: "ai",
		Questions: []onboardQuestion{
			{
				Key:      "ai.provider",
				Prompt:   "AI provider (deepseek/qwen/openai/claude/kimi/custom)",
				Required: true,
				Default: func(_ *onboardState) string {
					if v := onboardValue("ai.provider", nil); v != "" {
						return v
					}
					return "deepseek"
				},
				Validate: func(v string, _ *onboardState) error {
					switch strings.ToLower(strings.TrimSpace(v)) {
					case "deepseek", "qwen", "openai", "claude", "kimi", "custom":
						return nil
					default:
						return fmt.Errorf("unsupported provider %q", v)
					}
				},
			},
			{
				Key:      "ai.provider_name",
				Prompt:   "Custom provider name",
				Required: true,
				Default: func(s *onboardState) string {
					return onboardValue("ai.provider_name", s)
				},
				Condition: func(s *onboardState) bool {
					return strings.EqualFold(s.answers["ai.provider"], "custom")
				},
				Validate: func(v string, _ *onboardState) error {
					if strings.TrimSpace(v) == "" {
						return errors.New("provider name cannot be empty")
					}
					return nil
				},
			},
			{
				Key:      "ai.provider_type",
				Prompt:   "Custom provider type (e.g. openai/deepseek/claude)",
				Required: true,
				Default: func(s *onboardState) string {
					return onboardValue("ai.provider_type", s)
				},
				Condition: func(s *onboardState) bool {
					return strings.EqualFold(s.answers["ai.provider"], "custom")
				},
			},
			{
				Key:      "ai.api_key",
				Prompt:   "AI API key",
				Required: true,
				Default:  func(_ *onboardState) string { return "" },
			},
			{
				Key:      "ai.base_url",
				Prompt:   "AI base URL",
				Required: true,
				Default:  defaultAIBaseURL,
				Validate: func(v string, _ *onboardState) error {
					return validateHTTPURL(v, nil)
				},
			},
			{
				Key:      "ai.model.primary",
				Prompt:   "Primary model code",
				Required: true,
				Default:  defaultPrimaryModel,
			},
			{
				Key:      "ai.model.fallback",
				Prompt:   "Fallback model code (optional)",
				Required: false,
				Default:  defaultFallbackModel,
			},
		},
		Apply: func(s *onboardState) error { return nil },
	}
}

func modeConfigStep() onboardStep {
	return onboardStep{
		Name: "mode-config",
		Questions: []onboardQuestion{
			{
				Key:      "relay.platform",
				Prompt:   "Relay platform (wecom/feishu/slack/wechat)",
				Required: true,
				Default: func(s *onboardState) string {
					if v := onboardValue("relay.platform", s); v != "" {
						return v
					}
					if s.mode == "both" {
						return "wecom"
					}
					if s.cfg.Relay.Platform != "" {
						return s.cfg.Relay.Platform
					}
					return "wecom"
				},
				Condition: func(s *onboardState) bool {
					return s.mode == "relay" || s.mode == "both"
				},
				Validate: validateRelayPlatform,
			},
			{
				Key:      "relay.user_id",
				Prompt:   "Relay user_id",
				Required: true,
				Default:  defaultRelayUserID,
				Condition: func(s *onboardState) bool {
					return s.mode == "relay" || s.mode == "both"
				},
			},
			{
				Key:      "relay.token",
				Prompt:   "Relay token (can be empty if keeper has no token)",
				Required: false,
				Default:  defaultRelayToken,
				Condition: func(s *onboardState) bool {
					return s.mode == "relay" || s.mode == "both"
				},
			},
			{
				Key:      "relay.server_url",
				Prompt:   "Relay WebSocket server URL",
				Required: true,
				Default:  defaultRelayServerURL,
				Condition: func(s *onboardState) bool {
					return s.mode == "relay" || s.mode == "both"
				},
				Validate: validateWSURL,
			},
			{
				Key:      "relay.webhook_url",
				Prompt:   "Relay webhook URL",
				Required: true,
				Default:  defaultRelayWebhookURL,
				Condition: func(s *onboardState) bool {
					return s.mode == "relay" || s.mode == "both"
				},
				Validate: validateHTTPURL,
			},
			{
				Key:      "relay.use_media_proxy",
				Prompt:   "Use media proxy? (yes/no)",
				Required: true,
				Default:  defaultUseMediaProxy,
				Condition: func(s *onboardState) bool {
					return s.mode == "relay" || s.mode == "both"
				},
				Validate: validateBool,
			},
			{
				Key:      "keeper.port",
				Prompt:   "Keeper port",
				Required: true,
				Default:  defaultKeeperPort,
				Condition: func(s *onboardState) bool {
					return s.mode == "keeper" || s.mode == "both"
				},
				Validate: validatePort,
			},
			{
				Key:      "keeper.token",
				Prompt:   "Keeper auth token",
				Required: true,
				Default:  defaultKeeperToken,
				Condition: func(s *onboardState) bool {
					return s.mode == "keeper" || s.mode == "both"
				},
			},
			{
				Key:      "keeper.wecom_corp_id",
				Prompt:   "Keeper WeCom corp_id",
				Required: true,
				Default:  func(s *onboardState) string { return onboardValue("keeper.wecom_corp_id", s) },
				Condition: func(s *onboardState) bool {
					return s.mode == "keeper" || s.mode == "both"
				},
			},
			{
				Key:      "keeper.wecom_agent_id",
				Prompt:   "Keeper WeCom agent_id",
				Required: true,
				Default:  func(s *onboardState) string { return onboardValue("keeper.wecom_agent_id", s) },
				Condition: func(s *onboardState) bool {
					return s.mode == "keeper" || s.mode == "both"
				},
			},
			{
				Key:      "keeper.wecom_secret",
				Prompt:   "Keeper WeCom secret",
				Required: true,
				Default:  func(s *onboardState) string { return onboardValue("keeper.wecom_secret", s) },
				Condition: func(s *onboardState) bool {
					return s.mode == "keeper" || s.mode == "both"
				},
			},
			{
				Key:      "keeper.wecom_token",
				Prompt:   "Keeper WeCom callback token",
				Required: true,
				Default:  func(s *onboardState) string { return onboardValue("keeper.wecom_token", s) },
				Condition: func(s *onboardState) bool {
					return s.mode == "keeper" || s.mode == "both"
				},
			},
			{
				Key:      "keeper.wecom_aes_key",
				Prompt:   "Keeper WeCom AES key (43 chars)",
				Required: true,
				Default:  func(s *onboardState) string { return onboardValue("keeper.wecom_aes_key", s) },
				Condition: func(s *onboardState) bool {
					return s.mode == "keeper" || s.mode == "both"
				},
				Validate: validateAESKey,
			},
			{
				Key:      "relay.wecom_corp_id",
				Prompt:   "Relay WeCom corp_id (required for cloud relay only)",
				Required: false,
				Default:  defaultRelayWeComCorpID,
				Condition: func(s *onboardState) bool {
					return s.mode == "relay" && strings.EqualFold(s.answers["relay.platform"], "wecom") && isDefaultCloudRelay(s.answers["relay.server_url"])
				},
			},
			{
				Key:      "relay.wecom_agent_id",
				Prompt:   "Relay WeCom agent_id (required for cloud relay only)",
				Required: false,
				Default:  func(s *onboardState) string { return onboardValue("relay.wecom_agent_id", s) },
				Condition: func(s *onboardState) bool {
					return s.mode == "relay" && strings.EqualFold(s.answers["relay.platform"], "wecom") && isDefaultCloudRelay(s.answers["relay.server_url"])
				},
			},
			{
				Key:      "relay.wecom_secret",
				Prompt:   "Relay WeCom secret (required for cloud relay only)",
				Required: false,
				Default:  func(s *onboardState) string { return onboardValue("relay.wecom_secret", s) },
				Condition: func(s *onboardState) bool {
					return s.mode == "relay" && strings.EqualFold(s.answers["relay.platform"], "wecom") && isDefaultCloudRelay(s.answers["relay.server_url"])
				},
			},
			{
				Key:      "relay.wecom_token",
				Prompt:   "Relay WeCom callback token (required for cloud relay only)",
				Required: false,
				Default:  func(s *onboardState) string { return onboardValue("relay.wecom_token", s) },
				Condition: func(s *onboardState) bool {
					return s.mode == "relay" && strings.EqualFold(s.answers["relay.platform"], "wecom") && isDefaultCloudRelay(s.answers["relay.server_url"])
				},
			},
			{
				Key:      "relay.wecom_aes_key",
				Prompt:   "Relay WeCom AES key (required for cloud relay only)",
				Required: false,
				Default:  func(s *onboardState) string { return onboardValue("relay.wecom_aes_key", s) },
				Condition: func(s *onboardState) bool {
					return s.mode == "relay" && strings.EqualFold(s.answers["relay.platform"], "wecom") && isDefaultCloudRelay(s.answers["relay.server_url"])
				},
				Validate: func(v string, s *onboardState) error {
					v = strings.TrimSpace(v)
					if v == "" {
						return nil
					}
					return validateAESKey(v, s)
				},
			},
		},
		Apply: applyModeConfig,
	}
}

func runOnboardStep(state *onboardState, step onboardStep) error {
	fmt.Println()
	fmt.Printf("[%s]\n", step.Name)

	for _, q := range step.Questions {
		if q.Condition != nil && !q.Condition(state) {
			continue
		}
		v, err := askQuestion(state, q)
		if err != nil {
			return err
		}
		state.answers[q.Key] = v
	}

	if step.Apply != nil {
		return step.Apply(state)
	}
	return nil
}

func askQuestion(state *onboardState, q onboardQuestion) (string, error) {
	if v, ok := state.prefill[q.Key]; ok {
		v = strings.TrimSpace(v)
		if v == "" && q.Default != nil {
			v = strings.TrimSpace(q.Default(state))
		}
		if q.Required && v == "" {
			return "", fmt.Errorf("%s is required", q.Key)
		}
		if q.Validate != nil {
			if err := q.Validate(v, state); err != nil {
				return "", fmt.Errorf("%s: %w", q.Key, err)
			}
		}
		return v, nil
	}

	defaultValue := ""
	if q.Default != nil {
		defaultValue = strings.TrimSpace(q.Default(state))
	}

	if state.nonInteractive {
		v := defaultValue
		if q.Required && v == "" {
			return "", fmt.Errorf("%s is required (provide with --set %s=...)", q.Key, q.Key)
		}
		if q.Validate != nil {
			if err := q.Validate(v, state); err != nil {
				return "", fmt.Errorf("%s: %w", q.Key, err)
			}
		}
		return v, nil
	}

	for {
		if defaultValue != "" {
			fmt.Printf("%s [%s]: ", q.Prompt, defaultValue)
		} else {
			fmt.Printf("%s: ", q.Prompt)
		}

		line, err := state.reader.ReadString('\n')
		if err != nil {
			return "", err
		}
		v := strings.TrimSpace(line)
		if v == "" {
			v = defaultValue
		}

		if q.Required && strings.TrimSpace(v) == "" {
			fmt.Println("Value is required.")
			continue
		}
		if q.Validate != nil {
			if err := q.Validate(v, state); err != nil {
				fmt.Printf("Invalid value: %v\n", err)
				continue
			}
		}
		return v, nil
	}
}

func applyModeConfig(s *onboardState) error {
	switch s.mode {
	case "relay":
		applyRelayConfig(s.cfg, s.answers)
	case "keeper":
		applyKeeperConfig(s.cfg, s.answers)
	case "both":
		applyKeeperConfig(s.cfg, s.answers)
		applyRelayConfig(s.cfg, s.answers)
	}

	if s.mode == "both" {
		// Keep both mode self-contained by default.
		s.cfg.Relay.Token = strings.TrimSpace(s.answers["relay.token"])
		if s.cfg.Relay.Token == "" {
			s.cfg.Relay.Token = strings.TrimSpace(s.answers["keeper.token"])
		}
	}

	if s.mode == "relay" && strings.EqualFold(strings.TrimSpace(s.answers["relay.platform"]), "wecom") && isDefaultCloudRelay(s.answers["relay.server_url"]) {
		// Cloud relay for WeCom needs local WeCom credentials.
		if strings.TrimSpace(s.answers["relay.wecom_corp_id"]) == "" ||
			strings.TrimSpace(s.answers["relay.wecom_agent_id"]) == "" ||
			strings.TrimSpace(s.answers["relay.wecom_secret"]) == "" ||
			strings.TrimSpace(s.answers["relay.wecom_token"]) == "" ||
			strings.TrimSpace(s.answers["relay.wecom_aes_key"]) == "" {
			return errors.New("cloud relay with wecom requires relay.wecom_* fields")
		}
		s.cfg.Platforms.WeCom.CorpID = strings.TrimSpace(s.answers["relay.wecom_corp_id"])
		s.cfg.Platforms.WeCom.AgentID = strings.TrimSpace(s.answers["relay.wecom_agent_id"])
		s.cfg.Platforms.WeCom.Secret = strings.TrimSpace(s.answers["relay.wecom_secret"])
		s.cfg.Platforms.WeCom.Token = strings.TrimSpace(s.answers["relay.wecom_token"])
		s.cfg.Platforms.WeCom.AESKey = strings.TrimSpace(s.answers["relay.wecom_aes_key"])
	}

	// In both mode, reuse keeper WeCom credentials for relay convenience.
	if s.mode == "both" {
		s.cfg.Platforms.WeCom.CorpID = s.cfg.Keeper.WeComCorpID
		s.cfg.Platforms.WeCom.AgentID = s.cfg.Keeper.WeComAgentID
		s.cfg.Platforms.WeCom.Secret = s.cfg.Keeper.WeComSecret
		s.cfg.Platforms.WeCom.Token = s.cfg.Keeper.WeComToken
		s.cfg.Platforms.WeCom.AESKey = s.cfg.Keeper.WeComAESKey
	}

	return nil
}

func applyRelayConfig(cfg *config.Config, answers map[string]string) {
	cfg.Mode = "relay"
	cfg.Relay.Platform = strings.TrimSpace(answers["relay.platform"])
	cfg.Relay.UserID = strings.TrimSpace(answers["relay.user_id"])
	cfg.Relay.Token = strings.TrimSpace(answers["relay.token"])
	cfg.Relay.ServerURL = strings.TrimSpace(answers["relay.server_url"])
	cfg.Relay.WebhookURL = strings.TrimSpace(answers["relay.webhook_url"])
	cfg.Relay.UseMediaProxy = parseBoolDefault(answers["relay.use_media_proxy"], cfg.Relay.UseMediaProxy)
}

func applyKeeperConfig(cfg *config.Config, answers map[string]string) {
	cfg.Mode = "keeper"
	cfg.Keeper.Port = parseIntDefault(answers["keeper.port"], 8080)
	cfg.Keeper.Token = strings.TrimSpace(answers["keeper.token"])
	cfg.Keeper.WeComCorpID = strings.TrimSpace(answers["keeper.wecom_corp_id"])
	cfg.Keeper.WeComAgentID = strings.TrimSpace(answers["keeper.wecom_agent_id"])
	cfg.Keeper.WeComSecret = strings.TrimSpace(answers["keeper.wecom_secret"])
	cfg.Keeper.WeComToken = strings.TrimSpace(answers["keeper.wecom_token"])
	cfg.Keeper.WeComAESKey = strings.TrimSpace(answers["keeper.wecom_aes_key"])
}

func writeAIRegistry(s *onboardState) error {
	providerKey := strings.ToLower(strings.TrimSpace(s.answers["ai.provider"]))
	template := providerTemplates[providerKey]

	providerName := template.Name
	providerType := template.Type
	if providerKey == "custom" {
		providerName = strings.TrimSpace(s.answers["ai.provider_name"])
		providerType = strings.TrimSpace(s.answers["ai.provider_type"])
	}

	baseURL := strings.TrimSpace(s.answers["ai.base_url"])
	apiKey := strings.TrimSpace(s.answers["ai.api_key"])
	primaryCode := strings.TrimSpace(s.answers["ai.model.primary"])
	fallbackCode := strings.TrimSpace(s.answers["ai.model.fallback"])

	providersPath := ai.ProvidersPath()
	if err := os.MkdirAll(filepath.Dir(providersPath), 0755); err != nil {
		return fmt.Errorf("failed to create providers directory: %w", err)
	}

	pf := providersYAML{
		Providers: []providerYAML{
			{
				Name:    providerName,
				Type:    providerType,
				BaseURL: baseURL,
				APIKey:  apiKey,
			},
		},
	}
	pd, err := yaml.Marshal(pf)
	if err != nil {
		return fmt.Errorf("failed to marshal providers.yaml: %w", err)
	}
	if err := os.WriteFile(providersPath, pd, 0600); err != nil {
		return fmt.Errorf("failed to write providers.yaml: %w", err)
	}

	primaryModel := buildModelYAML(primaryCode, providerName, template)
	models := []modelYAML{primaryModel}
	if fallbackCode != "" && fallbackCode != primaryCode {
		models = append(models, buildModelYAML(fallbackCode, providerName, template))
	}

	mf := modelsYAML{Models: models}
	md, err := yaml.Marshal(mf)
	if err != nil {
		return fmt.Errorf("failed to marshal models.yaml: %w", err)
	}
	if err := os.WriteFile(ai.ModelsPath(), md, 0644); err != nil {
		return fmt.Errorf("failed to write models.yaml: %w", err)
	}

	return nil
}

func buildModelYAML(code, providerName string, template providerTemplate) modelYAML {
	code = strings.TrimSpace(code)
	mt := modelTemplate{
		Code:      code,
		Intellect: "excellent",
		Speed:     "fast",
		Cost:      "medium",
		Skills:    []string{},
	}

	if strings.EqualFold(code, template.PrimaryModel.Code) {
		mt = template.PrimaryModel
	}

	if template.FallbackModel != "" && strings.EqualFold(code, template.FallbackModel) {
		mt.Code = template.FallbackModel
	}

	return modelYAML{
		Name:      code,
		Code:      mt.Code,
		Provider:  providerName,
		Intellect: mt.Intellect,
		Speed:     mt.Speed,
		Cost:      mt.Cost,
		Skills:    mt.Skills,
	}
}

func maybeInstallService(s *onboardState) error {
	_, err := service.ServiceID(s.mode)
	if err != nil {
		fmt.Printf("Service setup skipped: %v\n", err)
		return nil
	}

	install, err := askYesNo(s, "service.install", "Install as a long-running service? (yes/no)", true)
	if err != nil {
		return err
	}
	if !install {
		return nil
	}

	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	if err := service.Install(execPath, s.mode); err != nil {
		return fmt.Errorf("service install failed: %w", err)
	}
	fmt.Println("Service installed.")

	startNow, err := askYesNo(s, "service.start", "Start service now? (yes/no)", true)
	if err != nil {
		return err
	}
	if !startNow {
		return nil
	}

	if err := service.Start(s.mode); err != nil {
		return fmt.Errorf("service start failed: %w", err)
	}
	fmt.Println("Service started.")

	return nil
}

func askYesNo(s *onboardState, key, prompt string, defaultYes bool) (bool, error) {
	q := onboardQuestion{
		Key:      key,
		Prompt:   prompt,
		Required: true,
		Default: func(_ *onboardState) string {
			if defaultYes {
				return "yes"
			}
			return "no"
		},
		Validate: validateBool,
	}
	v, err := askQuestion(s, q)
	if err != nil {
		return false, err
	}
	return parseBoolDefault(v, defaultYes), nil
}

func parseSetValues(raw []string) (map[string]string, error) {
	out := make(map[string]string, len(raw))
	for _, item := range raw {
		parts := strings.SplitN(item, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid --set value %q, expected key=value", item)
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		if key == "" {
			return nil, fmt.Errorf("invalid --set value %q, empty key", item)
		}
		out[key] = val
	}
	return out, nil
}

func onboardValue(key string, s *onboardState) string {
	if s != nil {
		if v, ok := s.prefill[key]; ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}

	switch key {
	case "ai.provider":
		return "deepseek"
	case "keeper.port":
		if s != nil && s.cfg.Keeper.Port != 0 {
			return strconv.Itoa(s.cfg.Keeper.Port)
		}
		return "8080"
	case "keeper.token":
		if s != nil && s.cfg.Keeper.Token != "" {
			return s.cfg.Keeper.Token
		}
	case "keeper.wecom_corp_id":
		if s != nil && s.cfg.Keeper.WeComCorpID != "" {
			return s.cfg.Keeper.WeComCorpID
		}
	case "keeper.wecom_agent_id":
		if s != nil && s.cfg.Keeper.WeComAgentID != "" {
			return s.cfg.Keeper.WeComAgentID
		}
	case "keeper.wecom_secret":
		if s != nil && s.cfg.Keeper.WeComSecret != "" {
			return s.cfg.Keeper.WeComSecret
		}
	case "keeper.wecom_token":
		if s != nil && s.cfg.Keeper.WeComToken != "" {
			return s.cfg.Keeper.WeComToken
		}
	case "keeper.wecom_aes_key":
		if s != nil && s.cfg.Keeper.WeComAESKey != "" {
			return s.cfg.Keeper.WeComAESKey
		}
	case "relay.platform":
		if s != nil && s.cfg.Relay.Platform != "" {
			return s.cfg.Relay.Platform
		}
	case "relay.user_id":
		if s != nil && s.cfg.Relay.UserID != "" {
			return s.cfg.Relay.UserID
		}
	case "relay.token":
		if s != nil && s.cfg.Relay.Token != "" {
			return s.cfg.Relay.Token
		}
	case "relay.server_url":
		if s != nil && s.cfg.Relay.ServerURL != "" {
			return s.cfg.Relay.ServerURL
		}
		return config.DefaultConfig().Relay.ServerURL
	case "relay.webhook_url":
		if s != nil && s.cfg.Relay.WebhookURL != "" {
			return s.cfg.Relay.WebhookURL
		}
		return config.DefaultConfig().Relay.WebhookURL
	case "relay.use_media_proxy":
		if s != nil {
			if s.cfg.Relay.UseMediaProxy {
				return "yes"
			}
			return "no"
		}
	}
	return ""
}

func defaultAIBaseURL(s *onboardState) string {
	provider := strings.ToLower(strings.TrimSpace(s.answers["ai.provider"]))
	if provider == "custom" {
		return "https://api.example.com/v1"
	}
	if tpl, ok := providerTemplates[provider]; ok {
		return tpl.DefaultBaseURL
	}
	return "https://api.deepseek.com/v1"
}

func defaultPrimaryModel(s *onboardState) string {
	provider := strings.ToLower(strings.TrimSpace(s.answers["ai.provider"]))
	if provider == "custom" {
		return "custom-model"
	}
	if tpl, ok := providerTemplates[provider]; ok {
		return tpl.PrimaryModel.Code
	}
	return "deepseek-chat"
}

func defaultFallbackModel(s *onboardState) string {
	provider := strings.ToLower(strings.TrimSpace(s.answers["ai.provider"]))
	if provider == "custom" {
		return ""
	}
	if tpl, ok := providerTemplates[provider]; ok {
		return tpl.FallbackModel
	}
	return ""
}

func defaultRelayUserID(s *onboardState) string {
	if v := onboardValue("relay.user_id", s); v != "" {
		return v
	}
	if strings.EqualFold(strings.TrimSpace(s.answers["relay.platform"]), "wecom") {
		if corp := strings.TrimSpace(s.answers["keeper.wecom_corp_id"]); corp != "" {
			return "wecom-" + corp
		}
		if corp := onboardValue("relay.wecom_corp_id", s); corp != "" {
			return "wecom-" + corp
		}
	}
	return "coco-user"
}

func defaultRelayToken(s *onboardState) string {
	if v := onboardValue("relay.token", s); v != "" {
		return v
	}
	if keeperToken := strings.TrimSpace(s.answers["keeper.token"]); keeperToken != "" {
		return keeperToken
	}
	return ""
}

func defaultRelayServerURL(s *onboardState) string {
	if s.mode == "both" {
		port := parseIntDefault(s.answers["keeper.port"], 8080)
		return fmt.Sprintf("ws://127.0.0.1:%d/ws", port)
	}
	return onboardValue("relay.server_url", s)
}

func defaultRelayWebhookURL(s *onboardState) string {
	if s.mode == "both" {
		port := parseIntDefault(s.answers["keeper.port"], 8080)
		return fmt.Sprintf("http://127.0.0.1:%d/webhook", port)
	}
	return onboardValue("relay.webhook_url", s)
}

func defaultUseMediaProxy(s *onboardState) string {
	if s.mode == "both" {
		return "no"
	}
	serverURL := strings.TrimSpace(s.answers["relay.server_url"])
	if serverURL == "" {
		serverURL = onboardValue("relay.server_url", s)
	}
	if isDefaultCloudRelay(serverURL) {
		return "yes"
	}
	if onboardValue("relay.use_media_proxy", s) == "yes" {
		return "yes"
	}
	return "no"
}

func defaultKeeperPort(s *onboardState) string {
	return onboardValue("keeper.port", s)
}

func defaultKeeperToken(s *onboardState) string {
	return onboardValue("keeper.token", s)
}

func defaultRelayWeComCorpID(s *onboardState) string {
	if v := onboardValue("relay.wecom_corp_id", s); v != "" {
		return v
	}
	return onboardValue("keeper.wecom_corp_id", s)
}

func validateModeValue(v string, _ *onboardState) error {
	_, err := service.ValidateMode(v)
	return err
}

func validateRelayPlatform(v string, _ *onboardState) error {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "wecom", "feishu", "slack", "wechat":
		return nil
	default:
		return fmt.Errorf("unsupported relay platform %q", v)
	}
}

func validateWSURL(v string, _ *onboardState) error {
	u, err := url.Parse(strings.TrimSpace(v))
	if err != nil {
		return err
	}
	if u.Scheme != "ws" && u.Scheme != "wss" {
		return fmt.Errorf("scheme must be ws or wss")
	}
	if u.Host == "" {
		return fmt.Errorf("host is required")
	}
	return nil
}

func validateHTTPURL(v string, _ *onboardState) error {
	u, err := url.Parse(strings.TrimSpace(v))
	if err != nil {
		return err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("scheme must be http or https")
	}
	if u.Host == "" {
		return fmt.Errorf("host is required")
	}
	return nil
}

func validatePort(v string, _ *onboardState) error {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		return fmt.Errorf("must be a number")
	}
	if n < 1 || n > 65535 {
		return fmt.Errorf("must be between 1 and 65535")
	}
	return nil
}

func validateAESKey(v string, _ *onboardState) error {
	if len(strings.TrimSpace(v)) != 43 {
		return fmt.Errorf("must be 43 characters")
	}
	return nil
}

func validateBool(v string, _ *onboardState) error {
	v = strings.ToLower(strings.TrimSpace(v))
	switch v {
	case "y", "yes", "n", "no", "true", "false", "1", "0":
		return nil
	default:
		return fmt.Errorf("expected yes/no")
	}
}

func parseBoolDefault(v string, defaultValue bool) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "y", "yes", "true", "1":
		return true
	case "n", "no", "false", "0":
		return false
	default:
		return defaultValue
	}
}

func parseIntDefault(v string, defaultValue int) int {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		return defaultValue
	}
	return n
}

func isDefaultCloudRelay(serverURL string) bool {
	u := strings.TrimSpace(serverURL)
	return u == "" || strings.EqualFold(u, "wss://keeper.kayz.com/ws")
}
