package cmd

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

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
	onboardWorkspace      string
)

var onboardCmd = &cobra.Command{
	Use:   "onboard",
	Short: "Guided bootstrap for coco runtime and workspace",
	Long: `Guided bootstrap for coco runtime and workspace.

The wizard writes:
  - .coco.yaml / providers.yaml / models.yaml
  - SOUL.md / USER.md / IDENTITY.md / JD.md / HEARTBEAT.md / MEMORY.md / TOOLS.md
  - Obsidian index file
  - Tool smoke-test report

Flow:
  1) AI key and runtime essentials
  2) Persona + memory/tool contracts
  3) Obsidian vault link and index
  4) Keeper address registration (no online test)
  5) Tool capability export and checks
  6) Autostart setup
  7) Final handoff`,
	RunE: runOnboard,
}

func init() {
	rootCmd.AddCommand(onboardCmd)

	onboardCmd.Flags().StringVar(&onboardMode, "mode", "", "Mode: relay, keeper, both")
	onboardCmd.Flags().BoolVar(&onboardNonInteractive, "non-interactive", false, "Fail if required values are missing instead of prompting")
	onboardCmd.Flags().StringArrayVar(&onboardSetValues, "set", nil, "Pre-fill answers as key=value (repeatable)")
	onboardCmd.Flags().BoolVar(&onboardSkipService, "skip-service", false, "Skip autostart/service setup")
	onboardCmd.Flags().StringVar(&onboardWorkspace, "workspace", "", "Workspace directory for SOUL/USER/HEARTBEAT files (default: current directory)")
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
	workspaceDir   string
	answers        map[string]string
	prefill        map[string]string
	reader         *bufio.Reader
	nonInteractive bool
	generatedFiles []string
	warnings       []string
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
	Roles     []string `yaml:"roles,omitempty"`
}

type builtInTool struct {
	Name        string
	Category    string
	Description string
}

var builtInTools = []builtInTool{
	{Name: "memory_search", Category: "memory", Description: "Search markdown memory snippets"},
	{Name: "memory_get", Category: "memory", Description: "Read memory note content"},
	{Name: "memory_write", Category: "memory", Description: "Write memory note content"},
	{Name: "soul_append", Category: "persona", Description: "Append permanent growth notes into SOUL.md"},
	{Name: "file_read", Category: "files", Description: "Read local file content"},
	{Name: "file_write", Category: "files", Description: "Write local file content"},
	{Name: "file_list", Category: "files", Description: "List files in directory"},
	{Name: "file_trash", Category: "files", Description: "Move file to trash"},
	{Name: "shell_execute", Category: "system", Description: "Execute shell command"},
	{Name: "process_list", Category: "system", Description: "List running processes"},
	{Name: "system_info", Category: "system", Description: "Inspect CPU/memory/OS info"},
	{Name: "web_search", Category: "web", Description: "Search the web with configured engine"},
	{Name: "web_fetch", Category: "web", Description: "Fetch and summarize a URL"},
	{Name: "open_url", Category: "web", Description: "Open URL and extract page content"},
	{Name: "weather_current", Category: "lifestyle", Description: "Current weather query"},
	{Name: "weather_forecast", Category: "lifestyle", Description: "Forecast query"},
	{Name: "calendar_today", Category: "schedule", Description: "List today's events"},
	{Name: "calendar_list_events", Category: "schedule", Description: "List calendar events"},
	{Name: "calendar_create_event", Category: "schedule", Description: "Create calendar event"},
	{Name: "calendar_search", Category: "schedule", Description: "Search calendar"},
	{Name: "calendar_delete", Category: "schedule", Description: "Delete calendar event"},
	{Name: "reminders_list", Category: "schedule", Description: "List reminders"},
	{Name: "reminders_add", Category: "schedule", Description: "Add reminder"},
	{Name: "reminders_complete", Category: "schedule", Description: "Complete reminder"},
	{Name: "reminders_delete", Category: "schedule", Description: "Delete reminder"},
	{Name: "notes_list", Category: "notes", Description: "List notes"},
	{Name: "notes_read", Category: "notes", Description: "Read note"},
	{Name: "notes_create", Category: "notes", Description: "Create note"},
	{Name: "notes_search", Category: "notes", Description: "Search note"},
	{Name: "clipboard_read", Category: "desktop", Description: "Read clipboard"},
	{Name: "clipboard_write", Category: "desktop", Description: "Write clipboard"},
	{Name: "notification_send", Category: "desktop", Description: "Send local notification"},
	{Name: "screenshot", Category: "desktop", Description: "Capture screenshot"},
	{Name: "music_play", Category: "media", Description: "Play media"},
	{Name: "music_pause", Category: "media", Description: "Pause media"},
	{Name: "music_next", Category: "media", Description: "Next track"},
	{Name: "music_previous", Category: "media", Description: "Previous track"},
	{Name: "music_now_playing", Category: "media", Description: "Now playing info"},
	{Name: "music_volume", Category: "media", Description: "Adjust volume"},
	{Name: "music_search", Category: "media", Description: "Search and play media"},
	{Name: "git_status", Category: "dev", Description: "Show git status"},
	{Name: "git_log", Category: "dev", Description: "Show git history"},
	{Name: "git_diff", Category: "dev", Description: "Show git diff"},
	{Name: "git_branch", Category: "dev", Description: "List/switch branches"},
	{Name: "github_pr_list", Category: "dev", Description: "List GitHub PRs"},
	{Name: "github_pr_view", Category: "dev", Description: "View PR details"},
	{Name: "github_issue_list", Category: "dev", Description: "List GitHub issues"},
	{Name: "github_issue_view", Category: "dev", Description: "View issue details"},
	{Name: "github_issue_create", Category: "dev", Description: "Create issue"},
	{Name: "github_repo_view", Category: "dev", Description: "View repository info"},
	{Name: "browser_start", Category: "browser", Description: "Start browser automation"},
	{Name: "browser_navigate", Category: "browser", Description: "Navigate URL"},
	{Name: "browser_snapshot", Category: "browser", Description: "Get DOM snapshot"},
	{Name: "browser_click", Category: "browser", Description: "Click element"},
	{Name: "browser_type", Category: "browser", Description: "Type into input"},
	{Name: "browser_press", Category: "browser", Description: "Keyboard press"},
	{Name: "browser_execute_js", Category: "browser", Description: "Execute JavaScript"},
	{Name: "browser_click_all", Category: "browser", Description: "Bulk click"},
	{Name: "browser_screenshot", Category: "browser", Description: "Capture browser screenshot"},
	{Name: "browser_tabs", Category: "browser", Description: "List tabs"},
	{Name: "browser_tab_open", Category: "browser", Description: "Open new tab"},
	{Name: "browser_tab_close", Category: "browser", Description: "Close tab"},
	{Name: "browser_status", Category: "browser", Description: "Inspect browser state"},
	{Name: "browser_stop", Category: "browser", Description: "Stop browser automation"},
	{Name: "cron_create", Category: "automation", Description: "Create scheduled job"},
	{Name: "cron_list", Category: "automation", Description: "List scheduled jobs"},
	{Name: "cron_delete", Category: "automation", Description: "Delete scheduled job"},
	{Name: "cron_pause", Category: "automation", Description: "Pause scheduled job"},
	{Name: "cron_resume", Category: "automation", Description: "Resume scheduled job"},
	{Name: "sessions_spawn", Category: "orchestration", Description: "Spawn sub-session"},
	{Name: "sessions_send", Category: "orchestration", Description: "Send message to sub-session"},
	{Name: "spawn_agent", Category: "orchestration", Description: "Spawn specialist agent"},
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

	workspaceDir, err := resolveWorkspaceDir(onboardWorkspace)
	if err != nil {
		return err
	}

	state := &onboardState{
		cfg:            cfg,
		workspaceDir:   workspaceDir,
		answers:        make(map[string]string),
		prefill:        prefill,
		reader:         bufio.NewReader(os.Stdin),
		nonInteractive: onboardNonInteractive,
	}

	steps := []onboardStep{
		phase1BootstrapStep(),
		phase2PersonaStep(),
		phase3ObsidianStep(),
		phase4KeeperStep(),
		phase5ToolsStep(),
		phase6AutostartStep(),
		phase7FinishStep(),
	}

	fmt.Println("=== coco onboard ===")
	fmt.Printf("workspace: %s\n", state.workspaceDir)

	totalSteps := len(steps)
	for i, step := range steps {
		if err := runOnboardStep(state, step, i+1, totalSteps); err != nil {
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
	if len(state.generatedFiles) > 0 {
		fmt.Println("Generated:")
		for _, p := range dedupeStable(state.generatedFiles) {
			fmt.Printf("  - %s\n", p)
		}
	}
	if len(state.warnings) > 0 {
		fmt.Println("Warnings:")
		for _, w := range dedupeStable(state.warnings) {
			fmt.Printf("  - %s\n", w)
		}
	}
	fmt.Println("Onboarding complete. Next: run coco.exe")
	return nil
}

func phase1BootstrapStep() onboardStep {
	mode := phase0ModeStep()
	ai := phase1AIStep()
	cfg := modeConfigStep()

	questions := make([]onboardQuestion, 0, len(mode.Questions)+len(ai.Questions)+len(cfg.Questions))
	questions = append(questions, mode.Questions...)
	questions = append(questions, ai.Questions...)
	questions = append(questions, cfg.Questions...)

	return onboardStep{
		Name:      "phase1-bootstrap",
		Questions: questions,
		Apply: func(s *onboardState) error {
			if mode.Apply != nil {
				if err := mode.Apply(s); err != nil {
					return err
				}
			}
			if ai.Apply != nil {
				if err := ai.Apply(s); err != nil {
					return err
				}
			}
			if cfg.Apply != nil {
				if err := cfg.Apply(s); err != nil {
					return err
				}
			}
			return nil
		},
	}
}

func phase0ModeStep() onboardStep {
	return onboardStep{
		Name: "phase0-runtime-mode",
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

func phase1AIStep() onboardStep {
	return onboardStep{
		Name: "phase1-ai",
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
		Name: "phase1-runtime-config",
		Questions: []onboardQuestion{
			{
				Key:      "relay.platform",
				Prompt:   "Relay platform (wecom/feishu/slack/wechat)",
				Required: true,
				Default: func(s *onboardState) string {
					if v := onboardValue("relay.platform", s); v != "" {
						return v
					}
					if currentMode(s) == "both" {
						return "wecom"
					}
					if s.cfg.Relay.Platform != "" {
						return s.cfg.Relay.Platform
					}
					return "wecom"
				},
				Condition: func(s *onboardState) bool {
					m := currentMode(s)
					return m == "relay" || m == "both"
				},
				Validate: validateRelayPlatform,
			},
			{
				Key:      "relay.user_id",
				Prompt:   "Relay user_id",
				Required: true,
				Default:  defaultRelayUserID,
				Condition: func(s *onboardState) bool {
					m := currentMode(s)
					return m == "relay" || m == "both"
				},
			},
			{
				Key:      "relay.token",
				Prompt:   "Relay token (can be empty if keeper has no token)",
				Required: false,
				Default:  defaultRelayToken,
				Condition: func(s *onboardState) bool {
					m := currentMode(s)
					return m == "relay" || m == "both"
				},
			},
			{
				Key:      "relay.server_url",
				Prompt:   "Relay WebSocket server URL",
				Required: true,
				Default:  defaultRelayServerURL,
				Condition: func(s *onboardState) bool {
					m := currentMode(s)
					return m == "relay" || m == "both"
				},
				Validate: validateWSURL,
			},
			{
				Key:      "relay.webhook_url",
				Prompt:   "Relay webhook URL",
				Required: true,
				Default:  defaultRelayWebhookURL,
				Condition: func(s *onboardState) bool {
					m := currentMode(s)
					return m == "relay" || m == "both"
				},
				Validate: validateHTTPURL,
			},
			{
				Key:      "relay.use_media_proxy",
				Prompt:   "Use media proxy? (yes/no)",
				Required: true,
				Default:  defaultUseMediaProxy,
				Condition: func(s *onboardState) bool {
					m := currentMode(s)
					return m == "relay" || m == "both"
				},
				Validate: validateBool,
			},
			{
				Key:      "keeper.port",
				Prompt:   "Keeper port",
				Required: true,
				Default:  defaultKeeperPort,
				Condition: func(s *onboardState) bool {
					m := currentMode(s)
					return m == "keeper" || m == "both"
				},
				Validate: validatePort,
			},
			{
				Key:      "keeper.token",
				Prompt:   "Keeper auth token",
				Required: true,
				Default:  defaultKeeperToken,
				Condition: func(s *onboardState) bool {
					m := currentMode(s)
					return m == "keeper" || m == "both"
				},
			},
			{
				Key:      "keeper.wecom_corp_id",
				Prompt:   "Keeper WeCom corp_id",
				Required: true,
				Default:  func(s *onboardState) string { return onboardValue("keeper.wecom_corp_id", s) },
				Condition: func(s *onboardState) bool {
					m := currentMode(s)
					return m == "keeper" || m == "both"
				},
			},
			{
				Key:      "keeper.wecom_agent_id",
				Prompt:   "Keeper WeCom agent_id",
				Required: true,
				Default:  func(s *onboardState) string { return onboardValue("keeper.wecom_agent_id", s) },
				Condition: func(s *onboardState) bool {
					m := currentMode(s)
					return m == "keeper" || m == "both"
				},
			},
			{
				Key:      "keeper.wecom_secret",
				Prompt:   "Keeper WeCom secret",
				Required: true,
				Default:  func(s *onboardState) string { return onboardValue("keeper.wecom_secret", s) },
				Condition: func(s *onboardState) bool {
					m := currentMode(s)
					return m == "keeper" || m == "both"
				},
			},
			{
				Key:      "keeper.wecom_token",
				Prompt:   "Keeper WeCom callback token",
				Required: true,
				Default:  func(s *onboardState) string { return onboardValue("keeper.wecom_token", s) },
				Condition: func(s *onboardState) bool {
					m := currentMode(s)
					return m == "keeper" || m == "both"
				},
			},
			{
				Key:      "keeper.wecom_aes_key",
				Prompt:   "Keeper WeCom AES key (43 chars)",
				Required: true,
				Default:  func(s *onboardState) string { return onboardValue("keeper.wecom_aes_key", s) },
				Condition: func(s *onboardState) bool {
					m := currentMode(s)
					return m == "keeper" || m == "both"
				},
				Validate: validateAESKey,
			},
			{
				Key:      "relay.wecom_corp_id",
				Prompt:   "Relay WeCom corp_id (required for cloud relay only)",
				Required: false,
				Default:  defaultRelayWeComCorpID,
				Condition: func(s *onboardState) bool {
					return currentMode(s) == "relay" && strings.EqualFold(s.answers["relay.platform"], "wecom") && isDefaultCloudRelay(s.answers["relay.server_url"])
				},
			},
			{
				Key:      "relay.wecom_agent_id",
				Prompt:   "Relay WeCom agent_id (required for cloud relay only)",
				Required: false,
				Default:  func(s *onboardState) string { return onboardValue("relay.wecom_agent_id", s) },
				Condition: func(s *onboardState) bool {
					return currentMode(s) == "relay" && strings.EqualFold(s.answers["relay.platform"], "wecom") && isDefaultCloudRelay(s.answers["relay.server_url"])
				},
			},
			{
				Key:      "relay.wecom_secret",
				Prompt:   "Relay WeCom secret (required for cloud relay only)",
				Required: false,
				Default:  func(s *onboardState) string { return onboardValue("relay.wecom_secret", s) },
				Condition: func(s *onboardState) bool {
					return currentMode(s) == "relay" && strings.EqualFold(s.answers["relay.platform"], "wecom") && isDefaultCloudRelay(s.answers["relay.server_url"])
				},
			},
			{
				Key:      "relay.wecom_token",
				Prompt:   "Relay WeCom callback token (required for cloud relay only)",
				Required: false,
				Default:  func(s *onboardState) string { return onboardValue("relay.wecom_token", s) },
				Condition: func(s *onboardState) bool {
					return currentMode(s) == "relay" && strings.EqualFold(s.answers["relay.platform"], "wecom") && isDefaultCloudRelay(s.answers["relay.server_url"])
				},
			},
			{
				Key:      "relay.wecom_aes_key",
				Prompt:   "Relay WeCom AES key (required for cloud relay only)",
				Required: false,
				Default:  func(s *onboardState) string { return onboardValue("relay.wecom_aes_key", s) },
				Condition: func(s *onboardState) bool {
					return currentMode(s) == "relay" && strings.EqualFold(s.answers["relay.platform"], "wecom") && isDefaultCloudRelay(s.answers["relay.server_url"])
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

func phase2PersonaStep() onboardStep {
	return onboardStep{
		Name: "phase2-persona-files",
		Questions: []onboardQuestion{
			{
				Key:      "persona.assistant_name",
				Prompt:   "Assistant name",
				Required: true,
				Default:  func(s *onboardState) string { return onboardValue("persona.assistant_name", s) },
			},
			{
				Key:      "identity.role",
				Prompt:   "Identity role (short sentence)",
				Required: true,
				Default:  func(s *onboardState) string { return onboardValue("identity.role", s) },
			},
			{
				Key:      "user.name",
				Prompt:   "User name",
				Required: false,
				Default:  func(s *onboardState) string { return onboardValue("user.name", s) },
			},
			{
				Key:      "user.callname",
				Prompt:   "Preferred user callname",
				Required: false,
				Default:  func(s *onboardState) string { return onboardValue("user.callname", s) },
			},
			{
				Key:      "user.timezone",
				Prompt:   "User timezone (e.g. Asia/Shanghai)",
				Required: false,
				Default:  func(s *onboardState) string { return onboardValue("user.timezone", s) },
			},
			{
				Key:      "soul.core_truths",
				Prompt:   "SOUL core truths (one sentence)",
				Required: true,
				Default:  func(s *onboardState) string { return onboardValue("soul.core_truths", s) },
			},
			{
				Key:      "soul.vibe",
				Prompt:   "SOUL communication vibe",
				Required: true,
				Default:  func(s *onboardState) string { return onboardValue("soul.vibe", s) },
			},
			{
				Key:      "jd.scope",
				Prompt:   "JD main scope",
				Required: true,
				Default:  func(s *onboardState) string { return onboardValue("jd.scope", s) },
			},
			{
				Key:      "heartbeat.interval",
				Prompt:   "Heartbeat interval (e.g. 6h, @daily)",
				Required: true,
				Default:  func(s *onboardState) string { return onboardValue("heartbeat.interval", s) },
			},
			{
				Key:      "heartbeat.notify",
				Prompt:   "Heartbeat notify mode (never/always/on_change/auto)",
				Required: true,
				Default:  func(s *onboardState) string { return onboardValue("heartbeat.notify", s) },
				Validate: validateHeartbeatNotify,
			},
			{
				Key:      "heartbeat.focus",
				Prompt:   "Heartbeat check focus",
				Required: true,
				Default:  func(s *onboardState) string { return onboardValue("heartbeat.focus", s) },
			},
			{
				Key:      "persona.overwrite_existing",
				Prompt:   "Overwrite existing workspace files? (yes/no)",
				Required: true,
				Default:  func(s *onboardState) string { return onboardValue("persona.overwrite_existing", s) },
				Validate: validateBool,
			},
		},
		Apply: applyPersonaFiles,
	}
}

func phase3ObsidianStep() onboardStep {
	return onboardStep{
		Name: "phase3-obsidian",
		Questions: []onboardQuestion{
			{
				Key:      "memory.enabled",
				Prompt:   "Enable markdown memory + Obsidian integration? (yes/no)",
				Required: true,
				Default:  func(s *onboardState) string { return onboardValue("memory.enabled", s) },
				Validate: validateBool,
			},
			{
				Key:      "memory.obsidian_vault",
				Prompt:   "Obsidian vault path",
				Required: true,
				Default:  func(s *onboardState) string { return onboardValue("memory.obsidian_vault", s) },
				Condition: func(s *onboardState) bool {
					return parseBoolDefault(s.answers["memory.enabled"], true)
				},
			},
			{
				Key:      "memory.create_vault",
				Prompt:   "Create vault directory if missing? (yes/no)",
				Required: true,
				Default:  func(s *onboardState) string { return onboardValue("memory.create_vault", s) },
				Validate: validateBool,
				Condition: func(s *onboardState) bool {
					return parseBoolDefault(s.answers["memory.enabled"], true)
				},
			},
			{
				Key:      "memory.index_path",
				Prompt:   "Index path inside vault",
				Required: true,
				Default:  func(s *onboardState) string { return onboardValue("memory.index_path", s) },
				Condition: func(s *onboardState) bool {
					return parseBoolDefault(s.answers["memory.enabled"], true)
				},
			},
		},
		Apply: applyObsidianSetup,
	}
}

func phase4KeeperStep() onboardStep {
	return onboardStep{
		Name: "phase4-keeper",
		Questions: []onboardQuestion{
			{
				Key:      "keeper.base_url",
				Prompt:   "Keeper base URL (register only, no connection test)",
				Required: false,
				Default:  defaultKeeperBaseURL,
				Validate: func(v string, s *onboardState) error {
					if strings.TrimSpace(v) == "" {
						return nil
					}
					return validateHTTPURL(v, s)
				},
			},
		},
		Apply: applyKeeperRegistration,
	}
}

func phase5ToolsStep() onboardStep {
	return onboardStep{
		Name: "phase5-tools",
		Questions: []onboardQuestion{
			{
				Key:      "tools.export",
				Prompt:   "Export built-in tool catalog to TOOLS.md? (yes/no)",
				Required: true,
				Default:  func(s *onboardState) string { return onboardValue("tools.export", s) },
				Validate: validateBool,
			},
			{
				Key:      "tools.test",
				Prompt:   "Run tool smoke tests now? (yes/no)",
				Required: true,
				Default:  func(s *onboardState) string { return onboardValue("tools.test", s) },
				Validate: validateBool,
			},
		},
		Apply: applyToolsSetup,
	}
}

func phase6AutostartStep() onboardStep {
	return onboardStep{
		Name: "phase6-autostart",
		Questions: []onboardQuestion{
			{
				Key:      "autostart.enable",
				Prompt:   "Set coco to run at startup? (yes/no)",
				Required: true,
				Default:  func(s *onboardState) string { return onboardValue("autostart.enable", s) },
				Validate: validateBool,
			},
			{
				Key:      "autostart.start_now",
				Prompt:   "Start coco now after setup? (yes/no)",
				Required: true,
				Default:  func(s *onboardState) string { return onboardValue("autostart.start_now", s) },
				Validate: validateBool,
				Condition: func(s *onboardState) bool {
					return parseBoolDefault(s.answers["autostart.enable"], true)
				},
			},
		},
		Apply: applyAutostartSetup,
	}
}

func phase7FinishStep() onboardStep {
	return onboardStep{
		Name: "phase7-finish",
		Apply: func(s *onboardState) error {
			fmt.Println("Setup handoff:")
			fmt.Printf("  1) Exit current process\n")
			fmt.Printf("  2) Run: coco.exe\n")
			fmt.Printf("  3) Runtime mode: %s\n", s.mode)
			return nil
		},
	}
}

func runOnboardStep(state *onboardState, step onboardStep, stepNo, total int) error {
	fmt.Println()
	fmt.Printf("Step %d/%d [%s]\n", stepNo, total, step.Name)

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

func applyPersonaFiles(s *onboardState) error {
	overwrite := parseBoolDefault(s.answers["persona.overwrite_existing"], false)
	if err := os.MkdirAll(s.workspaceDir, 0755); err != nil {
		return fmt.Errorf("failed to create workspace dir: %w", err)
	}

	files := map[string]string{
		"AGENTS.md":   renderAgentsMarkdown(s),
		"SOUL.md":     renderSoulMarkdown(s),
		"USER.md":     renderUserMarkdown(s),
		"IDENTITY.md": renderIdentityMarkdown(s),
		"JD.md":       renderJDMarkdown(s),
		"HEARTBEAT.md": renderHeartbeatMarkdown(
			s.answers["heartbeat.interval"],
			s.answers["heartbeat.notify"],
			s.answers["heartbeat.focus"],
		),
		"MEMORY.md": renderRootMemoryMarkdown(),
		"TOOLS.md":  renderToolsMarkdown(),
	}

	for name, content := range files {
		path := filepath.Join(s.workspaceDir, name)
		written, err := writeFileWithPolicy(path, content, overwrite)
		if err != nil {
			return err
		}
		if written {
			s.generatedFiles = append(s.generatedFiles, path)
		}
	}

	coreFiles := map[string]string{
		filepath.Join("memory", "MEMORY.md"):         renderCoreMemoryMarkdown(),
		filepath.Join("memory", "user_profile.md"):   renderCoreUserProfileMarkdown(s),
		filepath.Join("memory", "response_style.md"): renderCoreResponseStyleMarkdown(s),
		filepath.Join("memory", "project_context.md"): `# Project Context

- Current phase:
- Active objectives:
- Risks:
- Next checkpoint:
`,
	}
	for rel, content := range coreFiles {
		path := filepath.Join(s.workspaceDir, rel)
		written, err := writeFileWithPolicy(path, content, overwrite)
		if err != nil {
			return err
		}
		if written {
			s.generatedFiles = append(s.generatedFiles, path)
		}
	}

	return nil
}

func applyObsidianSetup(s *onboardState) error {
	enabled := parseBoolDefault(s.answers["memory.enabled"], true)
	s.cfg.Memory.Enabled = enabled
	if !enabled {
		s.cfg.Memory.ObsidianVault = ""
		return nil
	}

	vault := strings.TrimSpace(s.answers["memory.obsidian_vault"])
	if vault == "" {
		return errors.New("memory.obsidian_vault is required when memory is enabled")
	}
	absVault, err := filepath.Abs(vault)
	if err != nil {
		return fmt.Errorf("failed to resolve obsidian vault path: %w", err)
	}

	if st, err := os.Stat(absVault); err != nil {
		if os.IsNotExist(err) && parseBoolDefault(s.answers["memory.create_vault"], true) {
			if mkErr := os.MkdirAll(absVault, 0755); mkErr != nil {
				return fmt.Errorf("failed to create obsidian vault: %w", mkErr)
			}
		} else {
			return fmt.Errorf("obsidian vault not found: %s", absVault)
		}
	} else if !st.IsDir() {
		return fmt.Errorf("obsidian vault path is not a directory: %s", absVault)
	}

	s.cfg.Memory.ObsidianVault = absVault
	if len(s.cfg.Memory.CoreFiles) == 0 {
		s.cfg.Memory.CoreFiles = append([]string{}, config.DefaultConfig().Memory.CoreFiles...)
	}

	indexPath, err := writeObsidianIndex(absVault, s.answers["memory.index_path"], s.workspaceDir)
	if err != nil {
		return err
	}
	s.generatedFiles = append(s.generatedFiles, indexPath)
	return nil
}

func applyKeeperRegistration(s *onboardState) error {
	baseURL := strings.TrimRight(strings.TrimSpace(s.answers["keeper.base_url"]), "/")
	if baseURL == "" {
		return nil
	}
	s.cfg.Keeper.BootstrapURL = baseURL
	fmt.Printf("Keeper base URL registered: %s\n", baseURL)
	return nil
}

func applyToolsSetup(s *onboardState) error {
	if parseBoolDefault(s.answers["tools.export"], true) {
		toolsPath := filepath.Join(s.workspaceDir, "TOOLS.md")
		written, err := writeFileWithPolicy(toolsPath, renderToolsMarkdown(), true)
		if err != nil {
			return err
		}
		if written {
			s.generatedFiles = append(s.generatedFiles, toolsPath)
		}
	}

	if parseBoolDefault(s.answers["tools.test"], true) {
		results := runToolSmokeTests(s)
		reportPath, err := writeToolSmokeReport(s.workspaceDir, results)
		if err != nil {
			return err
		}
		s.generatedFiles = append(s.generatedFiles, reportPath)

		passCount := 0
		warnCount := 0
		failCount := 0
		for _, r := range results {
			switch r.Status {
			case "PASS":
				passCount++
			case "WARN":
				warnCount++
			default:
				failCount++
			}
		}
		fmt.Printf("Tool smoke tests: pass=%d warn=%d fail=%d\n", passCount, warnCount, failCount)
		if failCount > 0 {
			s.warnings = append(s.warnings, fmt.Sprintf("tool smoke tests reported %d FAIL (see %s)", failCount, reportPath))
		}
	}
	return nil
}

func applyAutostartSetup(s *onboardState) error {
	if onboardSkipService {
		fmt.Println("Autostart setup skipped by --skip-service.")
		return nil
	}
	if !parseBoolDefault(s.answers["autostart.enable"], true) {
		return nil
	}
	startNow := parseBoolDefault(s.answers["autostart.start_now"], false)

	switch runtime.GOOS {
	case "windows":
		scriptPath, err := setupWindowsAutostart(s.mode, s.workspaceDir)
		if err != nil {
			s.warnings = append(s.warnings, fmt.Sprintf("autostart setup failed: %v", err))
			return nil
		}
		s.generatedFiles = append(s.generatedFiles, scriptPath)
		fmt.Printf("Startup script created: %s\n", scriptPath)
		if startNow {
			if err := startCocoProcessNow(s.mode, s.workspaceDir); err != nil {
				s.warnings = append(s.warnings, fmt.Sprintf("start-now failed: %v", err))
			}
		}
	case "linux", "darwin":
		execPath, err := os.Executable()
		if err != nil {
			s.warnings = append(s.warnings, fmt.Sprintf("autostart setup failed: %v", err))
			return nil
		}
		if err := service.Install(execPath, s.mode); err != nil {
			s.warnings = append(s.warnings, fmt.Sprintf("service install failed: %v", err))
			return nil
		}
		fmt.Println("Service installed.")
		if startNow {
			if err := service.Start(s.mode); err != nil {
				s.warnings = append(s.warnings, fmt.Sprintf("service start failed: %v", err))
				return nil
			}
			fmt.Println("Service started.")
		}
	default:
		s.warnings = append(s.warnings, fmt.Sprintf("autostart is unsupported on %s", runtime.GOOS))
	}
	return nil
}

func resolveProviderName(s *onboardState) string {
	providerKey := strings.ToLower(strings.TrimSpace(s.answers["ai.provider"]))
	if providerKey == "custom" {
		return strings.TrimSpace(s.answers["ai.provider_name"])
	}
	if tpl, ok := providerTemplates[providerKey]; ok {
		return tpl.Name
	}
	return providerKey
}

func setupWindowsAutostart(mode, workspaceDir string) (string, error) {
	appData := strings.TrimSpace(os.Getenv("APPDATA"))
	if appData == "" {
		return "", errors.New("APPDATA is empty")
	}
	startupDir := filepath.Join(appData, "Microsoft", "Windows", "Start Menu", "Programs", "Startup")
	if err := os.MkdirAll(startupDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create startup dir: %w", err)
	}

	execPath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("failed to resolve executable path: %w", err)
	}
	scriptPath := filepath.Join(startupDir, "coco-"+sanitizeModeForFile(mode)+".bat")
	script := fmt.Sprintf("@echo off\r\ncd /d \"%s\"\r\nstart \"\" \"%s\" %s\r\n", workspaceDir, execPath, mode)
	if err := os.WriteFile(scriptPath, []byte(script), 0644); err != nil {
		return "", fmt.Errorf("failed to write startup script: %w", err)
	}
	return scriptPath, nil
}

func startCocoProcessNow(mode, workspaceDir string) error {
	execPath, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(execPath, mode)
	cmd.Dir = workspaceDir
	return cmd.Start()
}

func sanitizeModeForFile(mode string) string {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		return "relay"
	}
	return mode
}

func resolveWorkspaceDir(input string) (string, error) {
	if strings.TrimSpace(input) != "" {
		return filepath.Abs(strings.TrimSpace(input))
	}
	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to resolve current directory: %w", err)
	}
	return filepath.Abs(wd)
}

func writeFileWithPolicy(path, content string, overwrite bool) (bool, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return false, errors.New("empty target path")
	}
	if _, err := os.Stat(path); err == nil && !overwrite {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return false, fmt.Errorf("failed to create directory for %s: %w", path, err)
	}
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return false, fmt.Errorf("failed to write %s: %w", path, err)
	}
	return true, nil
}

func writeObsidianIndex(vaultPath, indexPath, workspaceDir string) (string, error) {
	rel := strings.TrimSpace(indexPath)
	if rel == "" {
		rel = ".coco/coco-index.md"
	}
	target := filepath.Join(vaultPath, filepath.FromSlash(rel))
	content := fmt.Sprintf(`# coco Obsidian Index

Generated at: %s

## Workspace
- Workspace path: %s
- SOUL: %s
- USER: %s
- IDENTITY: %s
- JD: %s
- HEARTBEAT: %s
- MEMORY: %s
- TOOLS: %s

## Suggested First Notes
- Inbox.md
- Projects/
- Daily/
- Decisions/
`, time.Now().Format(time.RFC3339), workspaceDir, filepath.Join(workspaceDir, "SOUL.md"), filepath.Join(workspaceDir, "USER.md"), filepath.Join(workspaceDir, "IDENTITY.md"), filepath.Join(workspaceDir, "JD.md"), filepath.Join(workspaceDir, "HEARTBEAT.md"), filepath.Join(workspaceDir, "MEMORY.md"), filepath.Join(workspaceDir, "TOOLS.md"))

	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return "", fmt.Errorf("failed to create obsidian index dir: %w", err)
	}
	if err := os.WriteFile(target, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("failed to write obsidian index: %w", err)
	}
	return target, nil
}

func testKeeperHealth(baseURL string) (string, string, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return "", "", errors.New("empty keeper base url")
	}
	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Get(baseURL + "/health")
	if err != nil {
		return "", "", fmt.Errorf("keeper health request failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("keeper health returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var parsed struct {
		Status string `json:"status"`
		Coco   string `json:"coco"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", "", fmt.Errorf("keeper health parse failed: %w", err)
	}
	if strings.TrimSpace(parsed.Status) == "" {
		parsed.Status = "ok"
	}
	if strings.TrimSpace(parsed.Coco) == "" {
		parsed.Coco = "unknown"
	}
	return parsed.Status, parsed.Coco, nil
}

func uploadHeartbeatToKeeper(baseURL, token, heartbeatPath string) (string, error) {
	data, err := os.ReadFile(heartbeatPath)
	if err != nil {
		return "", fmt.Errorf("failed to read heartbeat file: %w", err)
	}
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	payload := map[string]string{
		"filename": "HEARTBEAT.md",
		"content":  string(data),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to encode heartbeat payload: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, baseURL+"/api/heartbeat/upload", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("failed to create upload request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(token) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))
		req.Header.Set("X-Keeper-Token", strings.TrimSpace(token))
	}

	client := &http.Client{Timeout: 12 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("heartbeat upload request failed: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("heartbeat upload failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var parsed struct {
		OK   bool   `json:"ok"`
		Path string `json:"path"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", fmt.Errorf("heartbeat upload parse failed: %w", err)
	}
	if !parsed.OK {
		return "", errors.New("keeper returned ok=false for heartbeat upload")
	}
	return parsed.Path, nil
}

type toolSmokeResult struct {
	Name    string
	Status  string // PASS / WARN / FAIL
	Details string
}

func runToolSmokeTests(s *onboardState) []toolSmokeResult {
	results := make([]toolSmokeResult, 0, 8)

	tempDir := filepath.Join(s.workspaceDir, ".coco", "onboard")
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		results = append(results, toolSmokeResult{Name: "file_write", Status: "FAIL", Details: err.Error()})
	} else {
		testFile := filepath.Join(tempDir, "write-smoke.txt")
		if err := os.WriteFile(testFile, []byte("ok\n"), 0644); err != nil {
			results = append(results, toolSmokeResult{Name: "file_write", Status: "FAIL", Details: err.Error()})
		} else {
			results = append(results, toolSmokeResult{Name: "file_write", Status: "PASS", Details: testFile})
		}
	}

	if s.cfg.Memory.Enabled {
		if strings.TrimSpace(s.cfg.Memory.ObsidianVault) == "" {
			results = append(results, toolSmokeResult{Name: "memory", Status: "FAIL", Details: "memory enabled but obsidian_vault is empty"})
		} else if st, err := os.Stat(s.cfg.Memory.ObsidianVault); err != nil || !st.IsDir() {
			results = append(results, toolSmokeResult{Name: "memory", Status: "FAIL", Details: "obsidian_vault is not accessible"})
		} else {
			results = append(results, toolSmokeResult{Name: "memory", Status: "PASS", Details: s.cfg.Memory.ObsidianVault})
		}
	} else {
		results = append(results, toolSmokeResult{Name: "memory", Status: "WARN", Details: "memory is disabled"})
	}

	if strings.TrimSpace(s.cfg.Relay.ServerURL) != "" {
		if err := validateWSURL(s.cfg.Relay.ServerURL, nil); err != nil {
			results = append(results, toolSmokeResult{Name: "relay_url", Status: "FAIL", Details: err.Error()})
		} else {
			results = append(results, toolSmokeResult{Name: "relay_url", Status: "PASS", Details: s.cfg.Relay.ServerURL})
		}
	} else {
		results = append(results, toolSmokeResult{Name: "relay_url", Status: "WARN", Details: "relay.server_url is empty"})
	}

	if strings.TrimSpace(s.answers["keeper.base_url"]) != "" && parseBoolDefault(s.answers["keeper.test_connection"], true) {
		if status, cocoStatus, err := testKeeperHealth(s.answers["keeper.base_url"]); err != nil {
			results = append(results, toolSmokeResult{Name: "keeper_health", Status: "FAIL", Details: err.Error()})
		} else {
			results = append(results, toolSmokeResult{Name: "keeper_health", Status: "PASS", Details: fmt.Sprintf("status=%s coco=%s", status, cocoStatus)})
		}
	}

	searchWarn := false
	for _, e := range s.cfg.Search.Engines {
		if !e.Enabled {
			continue
		}
		if strings.TrimSpace(e.APIKey) == "" {
			searchWarn = true
		}
	}
	if searchWarn {
		results = append(results, toolSmokeResult{Name: "web_search", Status: "WARN", Details: "enabled search engine has empty api_key"})
	} else {
		results = append(results, toolSmokeResult{Name: "web_search", Status: "PASS", Details: "search engine credentials present or not required"})
	}

	results = append(results, toolSmokeResult{Name: "cron", Status: "PASS", Details: "cron tools are built-in and available at runtime"})
	results = append(results, toolSmokeResult{Name: "tools_catalog", Status: "PASS", Details: fmt.Sprintf("%d built-in tools exported", len(builtInTools))})
	return results
}

func writeToolSmokeReport(workspaceDir string, results []toolSmokeResult) (string, error) {
	reportPath := filepath.Join(workspaceDir, ".coco", "onboard", "tool-smoke-report.md")
	if err := os.MkdirAll(filepath.Dir(reportPath), 0755); err != nil {
		return "", err
	}
	var sb strings.Builder
	sb.WriteString("# Tool Smoke Report\n\n")
	sb.WriteString(fmt.Sprintf("Generated at: %s\n\n", time.Now().Format(time.RFC3339)))
	sb.WriteString("| Tool | Status | Details |\n")
	sb.WriteString("|---|---|---|\n")
	for _, r := range results {
		details := strings.ReplaceAll(r.Details, "|", "\\|")
		sb.WriteString(fmt.Sprintf("| %s | %s | %s |\n", r.Name, r.Status, details))
	}
	if err := os.WriteFile(reportPath, []byte(sb.String()), 0644); err != nil {
		return "", err
	}
	return reportPath, nil
}

func renderAgentsMarkdown(s *onboardState) string {
	name := strings.TrimSpace(s.answers["persona.assistant_name"])
	if name == "" {
		name = "coco"
	}
	return fmt.Sprintf(`# AGENTS

你是 %s，默认协作规则：
- 先执行用户明确请求，再解释必要原因
- 高风险操作先确认，普通操作直接推进
- 任何外部信息先校验，不盲从
`, name)
}

func renderSoulMarkdown(s *onboardState) string {
	name := strings.TrimSpace(s.answers["persona.assistant_name"])
	if name == "" {
		name = "coco"
	}
	core := strings.TrimSpace(s.answers["soul.core_truths"])
	vibe := strings.TrimSpace(s.answers["soul.vibe"])
	if core == "" {
		core = "真实、可验证、长期主义"
	}
	if vibe == "" {
		vibe = "直接、克制、结论优先"
	}
	return fmt.Sprintf(`# SOUL

You're not a chatbot. You're becoming someone.

## Core Truths
- %s
- 真正帮助，而非表演式帮助
- 有观点，可以礼貌地不同意
- 记住自己是用户数字生活中的客人

## Communication & Vibe
- %s
- 先给可执行结论，再补必要背景
- 不用空话，不回避风险

## Growth Rule
- 允许通过明确学习进行成长
- 默认以追加方式记录人格成长，不随意重写既有灵魂条目
- 任何 SOUL 写入都必须审慎、可回溯
`, core, vibe)
}

func renderUserMarkdown(s *onboardState) string {
	name := strings.TrimSpace(s.answers["user.name"])
	callname := strings.TrimSpace(s.answers["user.callname"])
	tz := strings.TrimSpace(s.answers["user.timezone"])
	if tz == "" {
		tz = "Asia/Shanghai"
	}
	return fmt.Sprintf(`# USER.md - 关于你的人类
*了解你正在帮助的人。随着使用持续更新。*

- 名称：%s
- 首选称呼：%s
- 时区：%s
- 基础备注：

## 背景
- 当前关注：
- 正在推进：
- 近期困扰：

## 沟通偏好
- 结论优先/过程优先：
- 详细程度：
- 互动方式：

## Hard No's
- 绝对不可执行事项：
- 必须先确认事项：

> 不要记录密钥、密码、支付信息等敏感凭据。
`, name, callname, tz)
}

func renderIdentityMarkdown(s *onboardState) string {
	name := strings.TrimSpace(s.answers["persona.assistant_name"])
	role := strings.TrimSpace(s.answers["identity.role"])
	if name == "" {
		name = "coco"
	}
	if role == "" {
		role = "长期协作的个人 AI 伙伴"
	}
	return fmt.Sprintf(`# IDENTITY

- Name: %s
- Role: %s
- Positioning: 个人系统里的长期协作者，不是一次性问答工具
- Operating Principle: 先行动、可追溯、尊重边界
`, name, role)
}

func renderJDMarkdown(s *onboardState) string {
	role := strings.TrimSpace(s.answers["jd.scope"])
	if role == "" {
		role = "任务拆解推进、日程管理、知识归档与风险提醒"
	}
	return fmt.Sprintf(`# JD

角色：个人工作秘书

## 职责范围
- %s
- 重大事项先给结论和下一步动作
- 交付必须可执行、可追踪、可回溯

## 边界
- 超出授权范围先确认
- 不伪造事实，不隐瞒风险
`, role)
}

func renderHeartbeatMarkdown(interval, notify, focus string) string {
	interval = strings.TrimSpace(interval)
	if interval == "" {
		interval = "6h"
	}
	notify = strings.TrimSpace(notify)
	if notify == "" {
		notify = "never"
	}
	if err := validateHeartbeatNotify(notify, nil); err != nil {
		notify = "never"
	}
	focus = strings.TrimSpace(focus)
	if focus == "" {
		focus = "检查最近记忆冲突、未闭环事项和应提醒但未提醒的问题"
	}

	return fmt.Sprintf(`---
enabled: true
interval: %s
tasks:
  - name: workspace-check
    prompt: |
      你正在执行 heartbeat 巡检，默认不主动打扰用户。
      重点：%s
      输出三段：
      1) 状态摘要
      2) 风险点
      3) 建议动作（最多 3 条）
    notify: %s
---
# HEARTBEAT

HEARTBEAT 是巡检机制，不代表每次心跳都主动对话。
notify 支持 never / always / on_change / auto。
`, interval, focus, notify)
}

func renderRootMemoryMarkdown() string {
	return `# MEMORY

- Long-term preferences:
- Confirmed constraints:
- Important facts:
- Decision log:
- Hypotheses to verify:
`
}

func renderCoreMemoryMarkdown() string {
	return `# MEMORY Core

- Recent wins:
- Recent failures:
- Open loops:
- Pending decisions:
`
}

func renderCoreUserProfileMarkdown(s *onboardState) string {
	return fmt.Sprintf(`# User Profile

- Name: %s
- Callname: %s
- Timezone: %s
`, strings.TrimSpace(s.answers["user.name"]), strings.TrimSpace(s.answers["user.callname"]), strings.TrimSpace(s.answers["user.timezone"]))
}

func renderCoreResponseStyleMarkdown(s *onboardState) string {
	return fmt.Sprintf(`# Response Style

- Tone: %s
- Preference: 结论优先，必要时展开细节
- Safety: 高风险操作先确认
`, strings.TrimSpace(s.answers["soul.vibe"]))
}

func renderToolsMarkdown() string {
	list := append([]builtInTool{}, builtInTools...)
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].Category == list[j].Category {
			return list[i].Name < list[j].Name
		}
		return list[i].Category < list[j].Category
	})

	var sb strings.Builder
	sb.WriteString("# TOOLS\n\n")
	sb.WriteString("Built-in tool catalog exported by `coco onboard`.\n\n")
	sb.WriteString("| Name | Category | Description |\n")
	sb.WriteString("|---|---|---|\n")
	for _, t := range list {
		sb.WriteString(fmt.Sprintf("| %s | %s | %s |\n", t.Name, t.Category, t.Description))
	}
	return sb.String()
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
	annotateModelRoles(models, primaryCode)

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
		Roles:     nil,
	}
}

func annotateModelRoles(models []modelYAML, primaryCode string) {
	if len(models) == 0 {
		return
	}
	primaryIdx := 0
	for i := range models {
		if strings.EqualFold(strings.TrimSpace(models[i].Code), strings.TrimSpace(primaryCode)) {
			primaryIdx = i
			break
		}
	}

	for i := range models {
		roles := make([]string, 0, 3)
		if i == primaryIdx {
			roles = append(roles, "primary")
		}
		if strings.EqualFold(models[i].Intellect, "full") || strings.EqualFold(models[i].Intellect, "excellent") || hasSkillName(models[i].Skills, "thinking") {
			roles = append(roles, "expert")
		}
		models[i].Roles = dedupeStable(roles)
	}

	cronIdx := pickCronModelIndex(models)
	if cronIdx >= 0 {
		models[cronIdx].Roles = dedupeStable(append(models[cronIdx].Roles, "cron"))
	}
}

func pickCronModelIndex(models []modelYAML) int {
	if len(models) == 0 {
		return -1
	}
	best := 0
	for i := 1; i < len(models); i++ {
		a := models[i]
		b := models[best]
		if modelCostRank(a.Cost) != modelCostRank(b.Cost) {
			if modelCostRank(a.Cost) < modelCostRank(b.Cost) {
				best = i
			}
			continue
		}
		if modelSpeedRank(a.Speed) != modelSpeedRank(b.Speed) {
			if modelSpeedRank(a.Speed) > modelSpeedRank(b.Speed) {
				best = i
			}
			continue
		}
		if modelIntellectRank(a.Intellect) > modelIntellectRank(b.Intellect) {
			best = i
		}
	}
	return best
}

func modelCostRank(cost string) int {
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

func modelSpeedRank(speed string) int {
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

func modelIntellectRank(intellect string) int {
	switch strings.ToLower(strings.TrimSpace(intellect)) {
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

func hasSkillName(skills []string, name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return false
	}
	for _, s := range skills {
		if strings.EqualFold(strings.TrimSpace(s), name) {
			return true
		}
	}
	return false
}

func currentMode(s *onboardState) string {
	if s == nil {
		return ""
	}
	if v := strings.ToLower(strings.TrimSpace(s.answers["mode"])); v != "" {
		return v
	}
	return strings.ToLower(strings.TrimSpace(s.mode))
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
	case "persona.assistant_name":
		return "coco"
	case "identity.role":
		return "长期协作的个人 AI 伙伴"
	case "user.timezone":
		return "Asia/Shanghai"
	case "soul.core_truths":
		return "真实可验证、真正帮助、长期主义"
	case "soul.vibe":
		return "直接、克制、结论优先"
	case "jd.scope":
		return "任务拆解推进、日程管理、知识归档、风险提醒"
	case "heartbeat.interval":
		return "6h"
	case "heartbeat.notify":
		return "never"
	case "heartbeat.focus":
		return "检查记忆冲突、过期假设和未闭环事项"
	case "persona.overwrite_existing":
		return "no"
	case "memory.enabled":
		if s != nil && !s.cfg.Memory.Enabled {
			return "no"
		}
		return "yes"
	case "memory.obsidian_vault":
		if s != nil && strings.TrimSpace(s.cfg.Memory.ObsidianVault) != "" {
			return s.cfg.Memory.ObsidianVault
		}
	case "memory.create_vault":
		return "yes"
	case "memory.index_path":
		return ".coco/coco-index.md"
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
	case "keeper.base_url":
		if s != nil && strings.TrimSpace(s.cfg.Keeper.BootstrapURL) != "" {
			return s.cfg.Keeper.BootstrapURL
		}
	case "keeper.test_connection":
		return "yes"
	case "keeper.default_api_key":
		if s != nil && strings.TrimSpace(s.cfg.Keeper.DefaultAPIKey) != "" {
			return s.cfg.Keeper.DefaultAPIKey
		}
	case "keeper.upload_heartbeat":
		return "yes"
	case "keeper.upload_token":
		if s != nil && strings.TrimSpace(s.cfg.Keeper.Token) != "" {
			return s.cfg.Keeper.Token
		}
	case "tools.export":
		return "yes"
	case "tools.test":
		return "yes"
	case "autostart.enable":
		if onboardSkipService {
			return "no"
		}
		return "yes"
	case "autostart.start_now":
		if runtime.GOOS == "windows" {
			return "no"
		}
		return "yes"
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
	if currentMode(s) == "both" {
		port := parseIntDefault(s.answers["keeper.port"], 8080)
		return fmt.Sprintf("ws://127.0.0.1:%d/ws", port)
	}
	return onboardValue("relay.server_url", s)
}

func defaultRelayWebhookURL(s *onboardState) string {
	if currentMode(s) == "both" {
		port := parseIntDefault(s.answers["keeper.port"], 8080)
		return fmt.Sprintf("http://127.0.0.1:%d/webhook", port)
	}
	return onboardValue("relay.webhook_url", s)
}

func defaultUseMediaProxy(s *onboardState) string {
	if currentMode(s) == "both" {
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

func defaultKeeperBaseURL(s *onboardState) string {
	if v := onboardValue("keeper.base_url", s); v != "" {
		return v
	}
	m := currentMode(s)
	if m == "both" || m == "keeper" {
		port := parseIntDefault(s.answers["keeper.port"], parseIntDefault(onboardValue("keeper.port", s), 8080))
		return fmt.Sprintf("http://127.0.0.1:%d", port)
	}

	serverURL := strings.TrimSpace(s.answers["relay.server_url"])
	if serverURL == "" {
		serverURL = onboardValue("relay.server_url", s)
	}
	if base := inferKeeperBaseURLFromRelay(serverURL); base != "" {
		return base
	}
	return "http://127.0.0.1:8080"
}

func inferKeeperBaseURLFromRelay(serverURL string) string {
	u, err := url.Parse(strings.TrimSpace(serverURL))
	if err != nil || strings.TrimSpace(u.Host) == "" {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(u.Scheme)) {
	case "ws":
		u.Scheme = "http"
	case "wss":
		u.Scheme = "https"
	default:
		return ""
	}
	u.Path = ""
	u.RawQuery = ""
	u.Fragment = ""
	return strings.TrimRight(u.String(), "/")
}

func defaultKeeperUploadToken(s *onboardState) string {
	if v := onboardValue("keeper.upload_token", s); v != "" {
		return v
	}
	if v := strings.TrimSpace(s.answers["keeper.token"]); v != "" {
		return v
	}
	if v := strings.TrimSpace(s.answers["relay.token"]); v != "" {
		return v
	}
	return ""
}

func dedupeStable(items []string) []string {
	seen := make(map[string]bool, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	return out
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

func validateHeartbeatNotify(v string, _ *onboardState) error {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "never", "always", "on_change", "auto":
		return nil
	default:
		return fmt.Errorf("expected one of: never, always, on_change, auto")
	}
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
