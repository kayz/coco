package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/kayz/coco/internal/ai"
	"github.com/kayz/coco/internal/config"
	cronpkg "github.com/kayz/coco/internal/cron"
	"github.com/kayz/coco/internal/logger"
	"github.com/kayz/coco/internal/persist"
	"github.com/kayz/coco/internal/promptbuild"
	"github.com/kayz/coco/internal/router"
	"github.com/kayz/coco/internal/search"
	"github.com/kayz/coco/internal/security"
	"github.com/kayz/coco/internal/skills"
)

var (
	exeDirCache string
)

// getExecutableDir returns the directory where the executable is located
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

// Agent processes messages using AI providers and tools
type Agent struct {
	modelRouter           *ai.ModelRouter
	registry              *ai.Registry
	providerCache         map[string]Provider
	providerMu            sync.RWMutex
	memory                *ConversationMemory
	ragMemory             *RAGMemory
	markdownMemory        *MarkdownMemory
	sessions              *SessionStore
	autoApprove           bool
	customInstructions    string
	cronScheduler         *cronpkg.Scheduler
	currentMsg            router.Message // set during HandleMessage for cron_create context
	cronCreatedCount      int            // tracks cron_create calls per HandleMessage turn
	securityMu            sync.RWMutex
	pathChecker           *security.PathChecker
	disableFileTools      bool
	blockedCommands       []string
	requireConfirmCmds    []string
	allowFrom             []string
	requireMentionInGroup bool
	configPath            string
	configMtime           time.Time
	persistStore          *persist.Store
	firstMessageSent      map[string]bool
	firstMessageMu        sync.RWMutex
	latestReport          *persist.DailyReport
	searchRegistry        *search.Registry
	searchManager         *search.Manager
}

// Config holds agent configuration
type Config struct {
	AutoApprove           bool     // Skip all confirmation prompts (default: false)
	CustomInstructions    string   // Additional instructions appended to system prompt (optional)
	AllowedPaths          []string // Restrict file/shell operations to these directories (empty = no restriction)
	BlockedCommands       []string // Block command patterns for shell execution
	RequireConfirmation   []string // Shell command patterns requiring confirmation unless auto approve
	AllowFrom             []string // Optional sender whitelist (userID/username/platform:userID)
	RequireMentionInGroup bool     // Ignore group messages unless explicitly mentioned
	DisableFileTools      bool     // Completely disable all file operation tools
	Embedding             config.EmbeddingConfig
	Memory                config.MemoryConfig
}

// loadPromptFile reads a prompt file from the project root
func loadPromptFile(filename string) string {
	execPath, err := os.Executable()
	if err != nil {
		return ""
	}

	dir := filepath.Dir(execPath)
	paths := []string{
		filepath.Join(dir, filename),
		filepath.Join(".", filename),
		filename,
	}

	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err == nil {
			return strings.TrimSpace(string(data))
		}
	}

	return ""
}

type workspacePromptFile struct {
	name     string
	required bool
}

var workspacePromptOrder = []workspacePromptFile{
	{name: "AGENTS.md", required: true},
	{name: "SOUL.md", required: true},
	{name: "PROFILE.md", required: false},
	{name: "MEMORY.md", required: false},
	{name: "HEARTBEAT.md", required: false},
}

func getWorkspaceDir() string {
	if env := strings.TrimSpace(os.Getenv("COCO_WORKSPACE_DIR")); env != "" {
		return env
	}
	if exeDir := getExecutableDir(); exeDir != "" {
		return exeDir
	}
	return "."
}

func stripYAMLFrontmatter(content string) string {
	content = strings.TrimSpace(content)
	if !strings.HasPrefix(content, "---") {
		return content
	}
	parts := strings.SplitN(content, "---", 3)
	if len(parts) < 3 {
		return content
	}
	return strings.TrimSpace(parts[2])
}

func loadWorkspacePromptBundle() string {
	workspaceDir := getWorkspaceDir()
	var sections []string

	for _, file := range workspacePromptOrder {
		path := filepath.Join(workspaceDir, file.name)
		data, err := os.ReadFile(path)
		if err != nil {
			if file.required {
				return ""
			}
			continue
		}

		content := stripYAMLFrontmatter(string(data))
		if strings.TrimSpace(content) == "" {
			if file.required {
				return ""
			}
			continue
		}
		sections = append(sections, fmt.Sprintf("# %s\n\n%s", file.name, content))
	}

	return strings.TrimSpace(strings.Join(sections, "\n\n"))
}

// ConversationKey generates a unique key for a conversation
func ConversationKey(platform, channelID, userID string) string {
	return platform + ":" + channelID + ":" + userID
}

func (a *Agent) chatWithModel(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	model := a.modelRouter.GetCurrentModel()
	if model == nil {
		return ChatResponse{}, fmt.Errorf("no current model")
	}

	provider, err := a.getProviderForModel(model)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("failed to get provider for model %s: %w", model.Name, err)
	}

	logger.Debug("[AGENT] Using model: %s (provider: %s)", model.Name, model.Provider)

	resp, err := provider.Chat(ctx, req)
	if err == nil {
		a.modelRouter.RecordSuccess(model)
		return resp, nil
	}

	logger.Warn("[AGENT] Model %s failed: %v", model.Name, err)
	a.modelRouter.RecordFailure(model)

	newModel, failoverErr := a.modelRouter.Failover()
	if failoverErr != nil {
		return ChatResponse{}, fmt.Errorf("model %s failed, and failover failed: %w", model.Name, err)
	}

	logger.Info("[AGENT] Failover to model: %s", newModel.Name)

	newProvider, err := a.getProviderForModel(newModel)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("failed to get provider for failover model %s: %w", newModel.Name, err)
	}

	resp, err = newProvider.Chat(ctx, req)
	if err == nil {
		a.modelRouter.RecordSuccess(newModel)
		return resp, nil
	}

	logger.Warn("[AGENT] Failover model %s also failed: %v", newModel.Name, err)
	a.modelRouter.RecordFailure(newModel)

	return ChatResponse{}, fmt.Errorf("all models failed, last error: %w", err)
}

func (a *Agent) getProviderForModel(model *ai.ModelConfig) (Provider, error) {
	key := model.Provider + ":" + model.Code

	a.providerMu.RLock()
	if provider, ok := a.providerCache[key]; ok {
		a.providerMu.RUnlock()
		return provider, nil
	}
	a.providerMu.RUnlock()

	a.providerMu.Lock()
	defer a.providerMu.Unlock()

	if provider, ok := a.providerCache[key]; ok {
		return provider, nil
	}

	providerConfig, ok := a.registry.GetProvider(model.Provider)
	if !ok {
		return nil, fmt.Errorf("provider not found: %s", model.Provider)
	}

	provider, err := a.createProvider(providerConfig, model.Code)
	if err != nil {
		return nil, fmt.Errorf("failed to create provider %s: %w", model.Provider, err)
	}

	a.providerCache[key] = provider
	return provider, nil
}

func (a *Agent) createProvider(cfg *ai.ProviderConfig, modelCode string) (Provider, error) {
	switch cfg.Type {
	case "deepseek":
		return NewDeepSeekProvider(DeepSeekConfig{
			APIKey:  cfg.APIKey,
			BaseURL: cfg.BaseURL,
			Model:   modelCode,
		})
	case "kimi", "moonshot":
		return NewKimiProvider(KimiConfig{
			APIKey:  cfg.APIKey,
			BaseURL: cfg.BaseURL,
			Model:   modelCode,
		})
	case "qwen", "qianwen", "tongyi":
		return NewQwenProvider(QwenConfig{
			APIKey:  cfg.APIKey,
			BaseURL: cfg.BaseURL,
			Model:   modelCode,
		})
	case "claude", "anthropic", "":
		return NewClaudeProvider(ClaudeConfig{
			APIKey:  cfg.APIKey,
			BaseURL: cfg.BaseURL,
			Model:   modelCode,
		})
	default:
		return a.createOpenAICompatProvider(cfg, modelCode)
	}
}

func (a *Agent) createOpenAICompatProvider(cfg *ai.ProviderConfig, modelCode string) (Provider, error) {
	defaults := map[string]struct {
		baseURL string
		model   string
	}{
		"minimax":     {"https://api.minimax.chat/v1", "MiniMax-Text-01"},
		"doubao":      {"https://ark.cn-beijing.volces.com/api/v3", "doubao-pro-32k"},
		"zhipu":       {"https://open.bigmodel.cn/api/paas/v4", "glm-4-flash"},
		"openai":      {"https://api.openai.com/v1", "gpt-4o"},
		"gemini":      {"https://generativelanguage.googleapis.com/v1beta/openai", "gemini-2.0-flash"},
		"yi":          {"https://api.lingyiwanwu.com/v1", "yi-large"},
		"stepfun":     {"https://api.stepfun.com/v1", "step-2-16k"},
		"siliconflow": {"https://api.siliconflow.cn/v1", "Qwen/Qwen2.5-72B-Instruct"},
		"grok":        {"https://api.x.ai/v1", "grok-2-latest"},
		"baichuan":    {"https://api.baichuan-ai.com/v1", "Baichuan4"},
		"spark":       {"https://spark-api-open.xf-yun.com/v1", "generalv3.5"},
		"hunyuan":     {"https://api.hunyuan.cloud.tencent.com/v1", "hunyuan-turbos-latest"},
	}

	aliases := map[string]string{
		"glm":         "zhipu",
		"chatglm":     "zhipu",
		"gpt":         "openai",
		"chatgpt":     "openai",
		"lingyiwanwu": "yi",
		"wanwu":       "yi",
		"google":      "gemini",
		"xai":         "grok",
		"bytedance":   "doubao",
		"volcengine":  "doubao",
		"iflytek":     "spark",
		"xunfei":      "spark",
		"tencent":     "hunyuan",
		"hungyuan":    "hunyuan",
	}

	name := cfg.Type
	if canonical, ok := aliases[name]; ok {
		name = canonical
	}

	defaultURL := ""
	defaultModel := ""
	if d, ok := defaults[name]; ok {
		defaultURL = d.baseURL
		defaultModel = d.model
	}

	if defaultURL == "" {
		return nil, fmt.Errorf("unknown provider: %s", cfg.Type)
	}

	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = defaultURL
	}

	model := modelCode
	if model == "" {
		model = defaultModel
	}

	return NewOpenAICompatProvider(OpenAICompatConfig{
		ProviderName: name,
		APIKey:       cfg.APIKey,
		BaseURL:      baseURL,
		Model:        model,
		DefaultURL:   defaultURL,
		DefaultModel: defaultModel,
	})
}

func (a *Agent) currentModelName() string {
	model := a.modelRouter.GetCurrentModel()
	if model == nil {
		return "unknown"
	}
	return model.Name
}

// New creates a new Agent with the specified provider
func New(cfg Config) (*Agent, error) {
	registry, err := ai.LoadRegistry()
	if err != nil {
		return nil, fmt.Errorf("failed to load registry: %w", err)
	}

	configCfg, err := config.Load()
	if err != nil {
		configCfg = config.DefaultConfig()
	}

	cooldownDuration := 5 * time.Minute
	if configCfg.ModelCooldown != "" {
		if d, err := time.ParseDuration(configCfg.ModelCooldown); err == nil {
			cooldownDuration = d
		}
	}

	modelRouter := ai.NewModelRouter(registry, cooldownDuration)

	exeDir := getExecutableDir()
	if exeDir == "" {
		exeDir = "."
	}

	dbPath := filepath.Join(exeDir, ".coco.db")
	persistStore, err := persist.NewStore(dbPath)
	if err != nil {
		return nil, err
	}

	memory := NewMemory(persistStore, 200)

	searchRegistry := search.NewRegistry()
	searchManager, err := search.NewManager(configCfg.Search, searchRegistry)
	if err != nil {
		log.Printf("[AGENT] Failed to initialize search manager: %v", err)
	}

	effectiveEmbedding := configCfg.Embedding
	if cfg.Embedding.Enabled || cfg.Embedding.APIKey != "" || cfg.Embedding.Provider != "" || cfg.Embedding.Model != "" || cfg.Embedding.BaseURL != "" {
		effectiveEmbedding = cfg.Embedding
	}

	var ragMemory *RAGMemory
	if effectiveEmbedding.Enabled {
		ragMemory, err = NewRAGMemory(effectiveEmbedding)
		if err != nil {
			log.Printf("[AGENT] Failed to initialize RAG memory: %v", err)
		}
	} else {
		ragMemory = &RAGMemory{enabled: false}
	}

	markdownMemory := NewMarkdownMemory(configCfg.Memory)
	if err := markdownMemory.EnableSemanticSearch(effectiveEmbedding); err != nil {
		log.Printf("[AGENT] Markdown semantic search disabled: %v", err)
	}
	markdownMemory.StartWatcher(10 * time.Second)

	agent := &Agent{
		modelRouter:        modelRouter,
		registry:           registry,
		providerCache:      make(map[string]Provider),
		memory:             memory,
		ragMemory:          ragMemory,
		markdownMemory:     markdownMemory,
		sessions:           NewSessionStore(),
		autoApprove:        cfg.AutoApprove,
		customInstructions: cfg.CustomInstructions,
		configPath:         config.ConfigPath(),
		persistStore:       persistStore,
		firstMessageSent:   make(map[string]bool),
		searchRegistry:     searchRegistry,
		searchManager:      searchManager,
	}
	agent.applySecurityConfig(
		cfg.AllowedPaths,
		cfg.DisableFileTools,
		cfg.BlockedCommands,
		cfg.RequireConfirmation,
		cfg.AllowFrom,
		cfg.RequireMentionInGroup,
	)
	agent.refreshRuntimeSecurityConfig()

	agent.initializeDailyReport()

	return agent, nil
}

type runtimeSecuritySnapshot struct {
	pathChecker           *security.PathChecker
	disableFileTools      bool
	blockedCommands       []string
	requireConfirmCmds    []string
	allowFrom             []string
	requireMentionInGroup bool
}

func normalizeAllowFrom(values []string) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0, len(values))
	for _, v := range values {
		key := strings.ToLower(strings.TrimSpace(v))
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, key)
	}
	return out
}

func (a *Agent) applySecurityConfig(allowedPaths []string, disableFileTools bool, blockedCommands []string, requireConfirmation []string, allowFrom []string, requireMentionInGroup bool) {
	blocked := security.NormalizeCommandPatterns(blockedCommands, security.DefaultBlockedCommandPatterns)
	requireConfirm := security.NormalizeCommandPatterns(requireConfirmation, nil)
	normalizedAllowFrom := normalizeAllowFrom(allowFrom)

	a.securityMu.Lock()
	defer a.securityMu.Unlock()

	a.pathChecker = security.NewPathChecker(allowedPaths)
	a.disableFileTools = disableFileTools
	a.blockedCommands = blocked
	a.requireConfirmCmds = requireConfirm
	a.allowFrom = normalizedAllowFrom
	a.requireMentionInGroup = requireMentionInGroup
}

func (a *Agent) refreshRuntimeSecurityConfig() {
	if strings.TrimSpace(a.configPath) == "" {
		return
	}

	info, err := os.Stat(a.configPath)
	if err != nil {
		return
	}

	a.securityMu.RLock()
	unchanged := !info.ModTime().After(a.configMtime)
	a.securityMu.RUnlock()
	if unchanged {
		return
	}

	cfg, err := config.LoadFromPath(a.configPath)
	if err != nil {
		logger.Warn("[Agent] Failed to reload runtime config: %v", err)
		return
	}

	a.applySecurityConfig(
		cfg.Security.AllowedPaths,
		cfg.Security.DisableFileTools,
		cfg.Security.BlockedCommands,
		cfg.Security.RequireConfirmation,
		cfg.Security.AllowFrom,
		cfg.Security.RequireMentionInGroup,
	)
	a.applyModelRouterConfig(cfg.ModelCooldown)
	a.applySearchConfig(cfg.Search)

	a.securityMu.Lock()
	a.configMtime = info.ModTime()
	a.securityMu.Unlock()
	logger.Info("[Agent] Reloaded runtime config from %s", a.configPath)
}

func (a *Agent) applySearchConfig(searchCfg config.SearchConfig) {
	if a.searchRegistry == nil {
		a.searchRegistry = search.NewRegistry()
	}
	manager, err := search.NewManager(searchCfg, a.searchRegistry)
	if err != nil {
		logger.Warn("[Agent] Failed to reload search config: %v", err)
		return
	}
	a.searchManager = manager
}

func (a *Agent) applyModelRouterConfig(modelCooldown string) {
	registry, err := ai.LoadRegistry()
	if err != nil {
		logger.Warn("[Agent] Failed to reload model registry: %v", err)
		return
	}

	cooldownDuration := 5 * time.Minute
	if modelCooldown != "" {
		if d, err := time.ParseDuration(modelCooldown); err == nil {
			cooldownDuration = d
		}
	}

	currentModelName := ""
	if a.modelRouter != nil {
		if current := a.modelRouter.GetCurrentModel(); current != nil {
			currentModelName = current.Name
		}
	}

	router := ai.NewModelRouter(registry, cooldownDuration)
	if currentModelName != "" {
		_ = router.SwitchToModel(currentModelName, true)
	}

	a.registry = registry
	a.modelRouter = router

	a.providerMu.Lock()
	a.providerCache = make(map[string]Provider)
	a.providerMu.Unlock()
}

func (a *Agent) securitySnapshot() runtimeSecuritySnapshot {
	a.securityMu.RLock()
	defer a.securityMu.RUnlock()

	snapshot := runtimeSecuritySnapshot{
		pathChecker:           a.pathChecker,
		disableFileTools:      a.disableFileTools,
		requireMentionInGroup: a.requireMentionInGroup,
	}
	if len(a.blockedCommands) > 0 {
		snapshot.blockedCommands = append([]string(nil), a.blockedCommands...)
	}
	if len(a.requireConfirmCmds) > 0 {
		snapshot.requireConfirmCmds = append([]string(nil), a.requireConfirmCmds...)
	}
	if len(a.allowFrom) > 0 {
		snapshot.allowFrom = append([]string(nil), a.allowFrom...)
	}
	return snapshot
}

func (a *Agent) validateShellCommand(command string) string {
	snapshot := a.securitySnapshot()

	if matched, ok := security.MatchCommandPattern(command, snapshot.blockedCommands); ok {
		logger.Warn("[Agent] Shell command blocked by policy: %s", matched)
		return fmt.Sprintf("ACCESS DENIED: command blocked by security policy (matched %q). Do NOT retry.", matched)
	}

	if !a.autoApprove {
		if matched, ok := security.MatchCommandPattern(command, snapshot.requireConfirmCmds); ok {
			logger.Info("[Agent] Shell command requires confirmation: %s", matched)
			return fmt.Sprintf("CONFIRMATION REQUIRED: command matches security.require_confirmation pattern %q. Re-run with --yes or adjust config before retrying.", matched)
		}
	}

	return ""
}

func (a *Agent) enforceMessageSecurityPolicy(msg router.Message) (string, bool) {
	snapshot := a.securitySnapshot()

	if len(snapshot.allowFrom) > 0 && !isSenderAllowed(msg, snapshot.allowFrom) {
		logger.Warn("[Agent] Message rejected by allow_from policy: %s/%s", msg.Platform, msg.UserID)
		return "ACCESS DENIED: sender is not in security.allow_from whitelist.", true
	}

	if snapshot.requireMentionInGroup && isGroupConversation(msg) && !isMessageExplicitlyMentioned(msg) {
		logger.Info("[Agent] Group message ignored because mention is required: %s/%s", msg.Platform, msg.ChannelID)
		return "", true
	}

	return "", false
}

func isSenderAllowed(msg router.Message, allowFrom []string) bool {
	if len(allowFrom) == 0 {
		return true
	}

	candidates := []string{
		strings.ToLower(strings.TrimSpace(msg.UserID)),
		strings.ToLower(strings.TrimSpace(msg.Username)),
		strings.ToLower(strings.TrimSpace(msg.Platform + ":" + msg.UserID)),
		strings.ToLower(strings.TrimSpace(msg.Platform + ":" + msg.Username)),
	}

	allowed := make(map[string]struct{}, len(allowFrom))
	for _, v := range allowFrom {
		allowed[strings.ToLower(strings.TrimSpace(v))] = struct{}{}
	}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if _, ok := allowed[candidate]; ok {
			return true
		}
	}
	return false
}

func isGroupConversation(msg router.Message) bool {
	meta := msg.Metadata
	if len(meta) == 0 {
		return false
	}

	chatType := strings.ToLower(strings.TrimSpace(meta["chat_type"]))
	switch chatType {
	case "private", "p2p", "im", "dm":
		return false
	case "group", "supergroup":
		return true
	}

	channelType := strings.ToLower(strings.TrimSpace(meta["channel_type"]))
	switch channelType {
	case "im", "dm":
		return false
	case "group", "group_dm", "mpim", "channel":
		return true
	}

	if strings.TrimSpace(meta["guild_id"]) != "" {
		return true
	}
	if strings.TrimSpace(meta["group_id"]) != "" {
		return true
	}
	if strings.TrimSpace(meta["conversation_type"]) == "2" {
		return true
	}

	return false
}

func isMessageExplicitlyMentioned(msg router.Message) bool {
	meta := msg.Metadata
	for _, key := range []string{"mentioned", "is_mentioned", "bot_mentioned", "is_in_at_list"} {
		value := strings.ToLower(strings.TrimSpace(meta[key]))
		if value == "true" || value == "1" || value == "yes" {
			return true
		}
	}

	text := strings.TrimSpace(msg.Text)
	return strings.Contains(text, "@")
}

// initializeDailyReport initializes the daily report functionality
func (a *Agent) initializeDailyReport() {
	yesterday := persist.GetYesterdayDate()
	report, err := a.persistStore.GetDailyReport(yesterday, "default")

	if err != nil || report == nil {
		a.generateDailyReport(yesterday)
	}

	latest, _ := a.persistStore.GetLatestDailyReport("default")
	a.latestReport = latest
}

// generateDailyReport generates a daily report for a specific date
func (a *Agent) generateDailyReport(date string) {
	report := &persist.DailyReport{
		Date:      date,
		UserID:    "default",
		Summary:   "系统启动时自动生成的日报",
		Content:   fmt.Sprintf("日报自动生成于 %s", time.Now().Format(time.RFC3339)),
		Tasks:     []persist.TaskItem{},
		Calendars: []persist.CalendarItem{},
	}

	if err := a.persistStore.SaveDailyReport(report); err != nil {
		log.Printf("[AGENT] Failed to save daily report: %v", err)
	}
}

// isFirstMessage checks if this is the first message from a user
func (a *Agent) isFirstMessage(key string) bool {
	a.firstMessageMu.RLock()
	_, sent := a.firstMessageSent[key]
	a.firstMessageMu.RUnlock()

	if !sent {
		a.firstMessageMu.Lock()
		a.firstMessageSent[key] = true
		a.firstMessageMu.Unlock()
		return true
	}
	return false
}

// getReportNotification gets the report notification message
func (a *Agent) getReportNotification() string {
	if a.latestReport == nil {
		return ""
	}

	notification := fmt.Sprintf("📋 今日日报 (%s)\n", a.latestReport.Date)
	if a.latestReport.Summary != "" {
		notification += fmt.Sprintf("摘要: %s\n\n", a.latestReport.Summary)
	}

	if len(a.latestReport.Tasks) > 0 {
		notification += "📌 当前任务:\n"
		for _, task := range a.latestReport.Tasks {
			status := "⭕"
			if task.Status == "completed" {
				status = "✅"
			} else if task.Status == "in_progress" {
				status = "🔄"
			}
			notification += fmt.Sprintf("  %s %s\n", status, task.Title)
		}
		notification += "\n"
	}

	if len(a.latestReport.Calendars) > 0 {
		notification += "📅 日历事件:\n"
		for _, cal := range a.latestReport.Calendars {
			notification += fmt.Sprintf("  - %s (%s)\n", cal.Title, cal.StartTime)
		}
	}

	return notification
}

// handleBuiltinCommand handles special commands without calling AI
func (a *Agent) handleBuiltinCommand(msg router.Message) (router.Response, bool) {
	text := strings.TrimSpace(msg.Text)
	textLower := strings.ToLower(text)
	convKey := ConversationKey(msg.Platform, msg.ChannelID, msg.UserID)

	// Exact match commands
	switch textLower {
	case "/whoami", "whoami", "我是谁", "我的id":
		return router.Response{
			Text: fmt.Sprintf("用户信息:\n- 用户ID: %s\n- 用户名: %s\n- 平台: %s\n- 频道ID: %s",
				msg.UserID, msg.Username, msg.Platform, msg.ChannelID),
		}, true

	case "/help", "help", "帮助", "/commands":
		return router.Response{
			Text: `可用命令:

会话管理:
  /new, /reset    开始新对话，清除历史
  /status         查看当前会话状态

思考模式:
  /think off      关闭深度思考
  /think low      简单思考
  /think medium   中等思考（默认）
  /think high     深度思考

显示设置:
  /verbose on     显示详细执行过程
  /verbose off    隐藏执行过程

其他:
  /whoami         查看用户信息
  /model          查看当前模型
  /tools          列出可用工具
  /help           显示帮助

直接用自然语言和我对话即可！`,
		}, true

	case "/new", "/reset", "/clear", "新对话", "清除历史":
		a.memory.Clear(convKey)
		a.sessions.Clear(convKey)
		return router.Response{
			Text: "已开始新对话，历史记录和会话设置已重置。",
		}, true

	case "/status", "状态":
		history := a.memory.GetHistory(convKey)
		settings := a.sessions.Get(convKey)
		return router.Response{
			Text: fmt.Sprintf(`会话状态:
- 平台: %s
- 用户: %s
- 历史消息: %d 条
- 思考模式: %s
- 详细模式: %v
- AI 模型: %s`,
				msg.Platform, msg.Username, len(history),
				settings.ThinkingLevel, settings.Verbose, a.currentModelName()),
		}, true

	case "/model", "模型":
		return router.Response{
			Text: fmt.Sprintf("当前模型: %s", a.currentModelName()),
		}, true

	case "/tools", "工具", "工具列表":
		toolsText := `可用工具:

📁 文件操作:
  file_send, file_list, file_read, file_write, file_trash, file_list_old

📅 日历 (macOS):
  calendar_today, calendar_list_events, calendar_create_event
  calendar_search, calendar_delete

✅ 提醒事项 (macOS):
  reminders_list, reminders_add, reminders_complete, reminders_delete

📝 备忘录 (macOS):
  notes_list, notes_read, notes_create, notes_search

🌤 天气:
  weather_current, weather_forecast

🌐 网页:
  web_search, web_fetch, open_url

📋 剪贴板:
  clipboard_read, clipboard_write

🔔 通知:
  notification_send

📸 截图:
  screenshot

🎵 音乐 (macOS):
  music_play, music_pause, music_next, music_previous
  music_now_playing, music_volume, music_search

💻 系统:
  system_info, shell_execute, process_list

⏰ 定时任务:
  cron_create, cron_list, cron_delete, cron_pause, cron_resume` + formatSkillsSection()
		return router.Response{Text: toolsText}, true

	case "/verbose on", "详细模式开":
		a.sessions.SetVerbose(convKey, true)
		return router.Response{Text: "详细模式已开启"}, true

	case "/verbose off", "详细模式关":
		a.sessions.SetVerbose(convKey, false)
		return router.Response{Text: "详细模式已关闭"}, true

	case "/think off", "思考关":
		a.sessions.SetThinkingLevel(convKey, ThinkOff)
		return router.Response{Text: "思考模式已关闭"}, true

	case "/think low", "简单思考":
		a.sessions.SetThinkingLevel(convKey, ThinkLow)
		return router.Response{Text: "思考模式: 简单"}, true

	case "/think medium", "中等思考":
		a.sessions.SetThinkingLevel(convKey, ThinkMedium)
		return router.Response{Text: "思考模式: 中等"}, true

	case "/think high", "深度思考":
		a.sessions.SetThinkingLevel(convKey, ThinkHigh)
		return router.Response{Text: "思考模式: 深度"}, true
	}

	return router.Response{}, false
}

// SetCronScheduler sets the cron scheduler for the agent
func (a *Agent) SetCronScheduler(s *cronpkg.Scheduler) {
	a.cronScheduler = s
	a.setupDailyReportJob()
}

// setupDailyReportJob sets up the daily report cron job
func (a *Agent) setupDailyReportJob() {
	if a.cronScheduler == nil {
		return
	}

	jobs := a.cronScheduler.ListJobs()
	for _, job := range jobs {
		if job.Name == "每日日报生成" {
			log.Printf("[AGENT] Daily report job already exists")
			return
		}
	}

	prompt := `请生成今日日报，包括：
1. 对昨天的对话内容进行整理和总结
2. 分析当前的任务状态
3. 检查日历事件
4. 生成今日任务清单
5. 调整定时任务（如有需要）

请使用中文回复。`

	_, err := a.cronScheduler.AddJobWithPrompt(
		"每日日报生成",
		"0 3 * * *", // 每天凌晨3点
		prompt,
		"local",
		"daily-report",
		"default",
	)

	if err != nil {
		log.Printf("[AGENT] Failed to create daily report job: %v", err)
	} else {
		log.Printf("[AGENT] Daily report job created successfully")
	}
}

// ExecuteTool implements the cron.ToolExecutor interface
func (a *Agent) ExecuteTool(ctx context.Context, toolName string, arguments map[string]any) (any, error) {
	result := callToolDirect(ctx, toolName, arguments)
	return result, nil
}

// ExecutePrompt runs a full AI conversation with tools and returns the text response.
// Used by cron scheduler for prompt-based jobs.
func (a *Agent) ExecutePrompt(ctx context.Context, platform, channelID, userID, prompt string) (string, error) {
	msg := router.Message{
		Platform:  platform,
		ChannelID: channelID,
		UserID:    userID,
		Username:  "cron",
		Text:      prompt,
	}
	resp, err := a.HandleMessage(ctx, msg)
	if err != nil {
		return "", err
	}
	return resp.Text, nil
}

type orchestrationPlan struct {
	NeedClarification  bool     `json:"need_clarification"`
	ClarifyingQuestion string   `json:"clarifying_question"`
	MemoryQueries      []string `json:"memory_queries"`
	FinalInstruction   string   `json:"final_instruction"`
	TaskComplexity     string   `json:"task_complexity"` // simple | normal | complex
}

func isTwoStageOrchestrationEnabled() bool {
	raw := strings.TrimSpace(os.Getenv("COCO_AGENT_ORCHESTRATION_ENABLE"))
	if raw == "" {
		return true
	}
	raw = strings.ToLower(raw)
	return raw == "1" || raw == "true" || raw == "yes" || raw == "on"
}

func (a *Agent) planOrchestration(ctx context.Context, userInput string, memoryRecall string) (*orchestrationPlan, error) {
	if a.modelRouter == nil {
		return nil, fmt.Errorf("model router not initialized")
	}

	plannerModel := a.selectPlannerModel()
	restore := a.switchModelTemporarily(plannerModel)
	defer restore()

	systemPrompt := `You are a response orchestration planner.
Output STRICT JSON only with keys:
- need_clarification (boolean)
- clarifying_question (string)
- memory_queries (array of strings, max 3)
- final_instruction (string, concise)
- task_complexity (simple|normal|complex)

Rules:
1. Ask clarification only when critical information is missing and cannot be inferred.
2. memory_queries should target retrieval intent, not full sentences.
3. final_instruction must describe how the final model should answer.
4. Never include markdown or extra commentary.`

	recall := strings.TrimSpace(memoryRecall)
	if len(recall) > 2200 {
		recall = recall[:2200] + "\n...[truncated]"
	}
	userPrompt := fmt.Sprintf("User input:\n%s\n\nKnown memory snippet:\n%s", strings.TrimSpace(userInput), recall)

	resp, err := a.chatWithModel(ctx, ChatRequest{
		Messages: []Message{
			{Role: "user", Content: userPrompt},
		},
		SystemPrompt: systemPrompt,
		Tools:        nil,
		MaxTokens:    600,
	})
	if err != nil {
		return nil, err
	}

	jsonPayload := extractJSONObject(strings.TrimSpace(resp.Content))
	if jsonPayload == "" {
		return nil, fmt.Errorf("planner returned non-json content")
	}

	var plan orchestrationPlan
	if err := json.Unmarshal([]byte(jsonPayload), &plan); err != nil {
		return nil, fmt.Errorf("invalid planner json: %w", err)
	}

	plan.ClarifyingQuestion = strings.TrimSpace(plan.ClarifyingQuestion)
	plan.FinalInstruction = strings.TrimSpace(plan.FinalInstruction)
	plan.TaskComplexity = normalizeTaskComplexity(plan.TaskComplexity)
	plan.MemoryQueries = normalizeMemoryQueries(plan.MemoryQueries, 3)

	return &plan, nil
}

func (a *Agent) appendPlannerMemoryRecall(ctx context.Context, queries []string, memoryRecallForPromptBuild *strings.Builder, markdownMemoriesSection *string) {
	if a.markdownMemory == nil || !a.markdownMemory.IsEnabled() || len(queries) == 0 {
		return
	}

	seenPath := map[string]bool{}
	var lines []string
	for _, q := range queries {
		hits, err := a.markdownMemory.Search(ctx, q, 3)
		if err != nil {
			logger.Warn("[Agent] planner memory search failed for %q: %v", q, err)
			continue
		}
		for _, h := range hits {
			if seenPath[h.Path] {
				continue
			}
			seenPath[h.Path] = true
			lines = append(lines, fmt.Sprintf("- [%s] %s (updated: %s)\n  %s",
				h.Source, h.Path, h.ModifiedAt.Format("2006-01-02 15:04"), h.Content))
			if len(lines) >= 8 {
				break
			}
		}
		if len(lines) >= 8 {
			break
		}
	}

	if len(lines) == 0 {
		return
	}

	section := "\n\n## Planner Memory Recall\n" + strings.Join(lines, "\n")
	if markdownMemoriesSection != nil {
		*markdownMemoriesSection += section
	}
	if memoryRecallForPromptBuild != nil {
		if memoryRecallForPromptBuild.Len() > 0 {
			memoryRecallForPromptBuild.WriteString("\n\n")
		}
		memoryRecallForPromptBuild.WriteString(strings.TrimSpace(section))
	}
}

func normalizeMemoryQueries(in []string, max int) []string {
	if max <= 0 {
		max = 3
	}
	seen := map[string]bool{}
	out := make([]string, 0, max)
	for _, q := range in {
		q = strings.TrimSpace(q)
		if len([]rune(q)) < 2 {
			continue
		}
		key := strings.ToLower(q)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, q)
		if len(out) >= max {
			break
		}
	}
	return out
}

func normalizeTaskComplexity(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	switch v {
	case "simple", "normal", "complex":
		return v
	default:
		return "normal"
	}
}

func extractJSONObject(content string) string {
	start := strings.Index(content, "{")
	end := strings.LastIndex(content, "}")
	if start < 0 || end <= start {
		return ""
	}
	return strings.TrimSpace(content[start : end+1])
}

func hasSkill(m *ai.ModelConfig, skill string) bool {
	if m == nil {
		return false
	}
	for _, s := range m.Skills {
		if strings.EqualFold(strings.TrimSpace(s), skill) {
			return true
		}
	}
	return false
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

func (a *Agent) selectPlannerModel() *ai.ModelConfig {
	models := a.modelRouter.ListModels()
	if len(models) == 0 {
		return nil
	}
	best := models[0]
	bestScore := -1
	for _, m := range models {
		score := speedRank(m.Speed)*100 + m.IntellectRank()*10
		if hasSkill(m, "thinking") {
			score += 2
		}
		if score > bestScore {
			best = m
			bestScore = score
		}
	}
	return best
}

func (a *Agent) selectFinalModel(complexity string) *ai.ModelConfig {
	models := a.modelRouter.ListModels()
	if len(models) == 0 {
		return nil
	}

	best := models[0]
	bestScore := -1
	for _, m := range models {
		score := m.IntellectRank() * 100
		if hasSkill(m, "thinking") {
			score += 25
		}
		switch complexity {
		case "simple":
			score += speedRank(m.Speed) * 8
		case "complex":
			score += m.IntellectRank() * 10
		default:
			score += speedRank(m.Speed) * 3
		}
		if score > bestScore {
			best = m
			bestScore = score
		}
	}
	return best
}

func (a *Agent) switchModelTemporarily(target *ai.ModelConfig) func() {
	if target == nil || a.modelRouter == nil {
		return func() {}
	}

	current := a.modelRouter.GetCurrentModel()
	if current != nil && current.Name == target.Name {
		return func() {}
	}

	var previous string
	if current != nil {
		previous = current.Name
	}

	if err := a.modelRouter.SwitchToModel(target.Name, true); err != nil {
		logger.Warn("[Agent] failed to switch to model %s: %v", target.Name, err)
		return func() {}
	}
	logger.Debug("[Agent] temporary model switch: %s", target.Name)

	return func() {
		if previous == "" {
			return
		}
		if err := a.modelRouter.SwitchToModel(previous, true); err != nil {
			logger.Warn("[Agent] failed to restore model %s: %v", previous, err)
		}
	}
}

func (a *Agent) persistTurnAndLongMemory(ctx context.Context, convKey string, msg router.Message, assistantText string) {
	a.memory.AddExchange(convKey,
		Message{Role: "user", Content: msg.Text},
		Message{Role: "assistant", Content: assistantText},
	)

	if a.ragMemory != nil && a.ragMemory.IsEnabled() {
		conversationText := fmt.Sprintf("User: %s\nAssistant: %s", msg.Text, assistantText)
		err := a.ragMemory.AddMemory(ctx, MemoryItem{
			ID:      fmt.Sprintf("conv-%s-%d", convKey, time.Now().Unix()),
			Type:    "conversation",
			Content: conversationText,
			Metadata: map[string]string{
				"platform":  msg.Platform,
				"channel":   msg.ChannelID,
				"user":      msg.UserID,
				"timestamp": time.Now().Format(time.RFC3339),
			},
		})
		if err != nil {
			logger.Warn("[Agent] Failed to save conversation to RAG memory: %v", err)
		} else {
			logger.Debug("[Agent] Conversation saved to RAG memory")
		}

		history := a.memory.GetHistory(convKey)
		if len(history) > 0 && len(history)%4 == 0 {
			a.learnUserPreferences(ctx, convKey, msg)
		}
	}
}

// HandleMessage processes a message and returns a response
func (a *Agent) HandleMessage(ctx context.Context, msg router.Message) (router.Response, error) {
	a.refreshRuntimeSecurityConfig()
	a.currentMsg = msg
	a.cronCreatedCount = 0
	logger.Info("[Agent] Processing message from %s: %s (model: %s)", msg.Username, msg.Text, a.currentModelName())

	if denial, drop := a.enforceMessageSecurityPolicy(msg); drop {
		if denial == "" {
			return router.Response{}, nil
		}
		return router.Response{Text: denial}, nil
	}

	// Handle built-in commands
	if resp, handled := a.handleBuiltinCommand(msg); handled {
		return resp, nil
	}

	// Generate conversation key
	convKey := ConversationKey(msg.Platform, msg.ChannelID, msg.UserID)

	// Build the tools list
	tools := a.buildToolsList()

	// Get conversation history
	history := a.memory.GetHistory(convKey)
	if thresholdChars, keepRecent := contextCompactionSettings(); thresholdChars > 0 {
		if compacted, compactedOK := compactHistoryForPrompt(history, thresholdChars, keepRecent); compactedOK {
			logger.Info("[Agent] Context compaction applied: %d -> %d messages", len(history), len(compacted))
			history = compacted
		}
	}
	logger.Trace("[Agent] Conversation key: %s, history messages: %d", convKey, len(history))

	// Create messages with history
	messages := make([]Message, 0, len(history)+1)
	messages = append(messages, history...)

	// Add current message
	messages = append(messages, Message{
		Role:    "user",
		Content: msg.Text,
	})

	// Get system info for context
	exeDir := getExecutableDir()
	if exeDir == "" {
		exeDir = "."
	}

	// Get session settings
	settings := a.sessions.Get(convKey)
	thinkingPrompt := ThinkingPrompt(settings.ThinkingLevel)

	// Auto-approval mode notice
	autoApprovalNotice := ""
	if a.autoApprove {
		autoApprovalNotice = `

## 🚀 AUTO-APPROVAL MODE ENABLED
**You are running in auto-approval mode. DO NOT ask for permission before taking actions.**
- Execute file writes, deletions, and modifications immediately without confirmation
- Run shell commands directly without asking first
- Create, modify, or delete files as requested without hesitation
- The user has explicitly disabled all safety prompts with --yes flag
- Only skip actions if they are IMPOSSIBLE or DANGEROUS (e.g., rm -rf /, destructive operations)
- For normal operations (file writes, reads, modifications), proceed immediately`
	}

	// Load prompt files
	aboutMe := loadPromptFile("ABOUTME.md")
	systemContent := loadPromptFile("SYSTEM.md")
	workspacePromptBundle := loadWorkspacePromptBundle()

	// Fallback to default if files not found
	if aboutMe == "" {
		aboutMe = "You are coco, a helpful AI assistant running on the user's computer."
	}

	// Retrieve relevant memories from markdown + RAG if enabled
	var markdownMemoriesSection string
	var memoriesSection string
	var preferencesSection string
	var memoryRecallForPromptBuild strings.Builder
	if a.markdownMemory != nil && a.markdownMemory.IsEnabled() {
		markdownMemories, err := a.markdownMemory.Search(ctx, msg.Text, 6)
		if err != nil {
			logger.Warn("[Agent] Failed to search markdown memories: %v", err)
		} else if len(markdownMemories) > 0 {
			markdownMemoriesSection = "\n\n## Markdown Memories\nHere are recent and relevant notes from local markdown memory:\n"
			for i, mem := range markdownMemories {
				modified := mem.ModifiedAt.Format("2006-01-02 15:04")
				markdownMemoriesSection += fmt.Sprintf("%d. [%s] %s (updated: %s)\n%s\n\n",
					i+1, mem.Source, mem.Path, modified, mem.Content)
			}
			memoryRecallForPromptBuild.WriteString("## Markdown Memories\n")
			memoryRecallForPromptBuild.WriteString(strings.TrimSpace(markdownMemoriesSection))
			logger.Debug("[Agent] Retrieved %d markdown memories", len(markdownMemories))
		}
	}

	if a.ragMemory != nil && a.ragMemory.IsEnabled() {
		memories, err := a.ragMemory.SearchMemories(ctx, msg.Text, 5)
		if err == nil && len(memories) > 0 {
			memoriesSection = "\n\n## Relevant Memories\nHere are some relevant memories from previous conversations that might help you respond:\n"
			for i, mem := range memories {
				memoriesSection += fmt.Sprintf("%d. [%s] %s\n", i+1, mem.Type, mem.Content)
			}
			if memoryRecallForPromptBuild.Len() > 0 {
				memoryRecallForPromptBuild.WriteString("\n\n")
			}
			memoryRecallForPromptBuild.WriteString(strings.TrimSpace(memoriesSection))
			logger.Debug("[Agent] Retrieved %d relevant memories", len(memories))
		}

		// Retrieve user preferences
		preferences, err := a.ragMemory.SearchMemories(ctx, "user preferences communication style tone format", 3)
		if err == nil && len(preferences) > 0 {
			preferencesSection = "\n\n## User Preferences\nHere are some known preferences about this user that you should follow:\n"
			for i, pref := range preferences {
				if pref.Type == "preference" {
					preferencesSection += fmt.Sprintf("%d. %s\n", i+1, pref.Content)
				}
			}
			if strings.TrimSpace(preferencesSection) != "" {
				if memoryRecallForPromptBuild.Len() > 0 {
					memoryRecallForPromptBuild.WriteString("\n\n")
				}
				memoryRecallForPromptBuild.WriteString(strings.TrimSpace(preferencesSection))
			}
			logger.Debug("[Agent] Retrieved %d user preferences", len(preferences))
		}
	}

	plannerInstruction := ""
	taskComplexity := "normal"
	if isTwoStageOrchestrationEnabled() {
		plan, err := a.planOrchestration(ctx, msg.Text, strings.TrimSpace(memoryRecallForPromptBuild.String()))
		if err != nil {
			logger.Warn("[Agent] orchestration planner failed, fallback single-stage: %v", err)
		} else if plan != nil {
			taskComplexity = normalizeTaskComplexity(plan.TaskComplexity)
			plannerInstruction = strings.TrimSpace(plan.FinalInstruction)
			if len(plan.MemoryQueries) > 0 {
				a.appendPlannerMemoryRecall(ctx, plan.MemoryQueries, &memoryRecallForPromptBuild, &markdownMemoriesSection)
			}

			if plan.NeedClarification && strings.TrimSpace(plan.ClarifyingQuestion) != "" {
				clarify := strings.TrimSpace(plan.ClarifyingQuestion)
				a.persistTurnAndLongMemory(ctx, convKey, msg, clarify)
				a.isFirstMessage(convKey)
				return router.Response{Text: clarify}, nil
			}
		}
	}

	// System prompt with actual paths
	var systemPrompt string
	if systemContent != "" {
		systemPrompt = fmt.Sprintf(aboutMe+"%s\n\n"+systemContent,
			autoApprovalNotice, runtime.GOOS, runtime.GOARCH, exeDir, msg.Username, time.Now().Format("2006-01-02"))
	} else {
		systemPrompt = fmt.Sprintf(`You are coco, a helpful AI assistant running on the user's computer.%s

## System Environment
- Operating System: %s
- Architecture: %s
- Executable Directory: %s
- User: %s

## Available Tools

### File Operations
- file_send: Send/transfer a file to the user via messaging platform
- file_list: List directory contents (use ~ for executable directory)
- file_read: Read file contents
- file_write: Write content to a file (creates parent directories if needed)
- file_trash: Move files to trash (for delete operations)
- file_list_old: Find old files not modified for N days

### User Schedules & Reminders
- Use cron_create with tag="user-schedule" to create user's personal schedules, reminders, and calendar events
- Set the 'prompt' parameter to describe what you should remind the user about
- Use cron_list with tag="user-schedule" to list only user's schedules
- For assistant's background tasks (daily reports, etc.), use tag="assistant-task"

### Notes (macOS)
- notes_list: List notes
- notes_read: Read note content
- notes_create: Create new note
- notes_search: Search notes

### Weather
- weather_current: Current weather
- weather_forecast: Weather forecast

### Web
- web_search: Search the web using configured search engines (Metaso, Tavily, or custom engines)
- web_fetch: Fetch URL content
- open_url: Open URL in browser

### Clipboard
- clipboard_read: Read clipboard
- clipboard_write: Write to clipboard

### System
- system_info: System information
- shell_execute: Execute shell command
- process_list: List processes
- notification_send: Send notification
- screenshot: Capture screen

### Music (macOS)
- music_play/pause/next/previous: Playback control
- music_now_playing: Current track info
- music_volume: Set volume
- music_search: Search and play

### Scheduled Tasks (Cron)
- cron_create: Create ONE scheduled task with 'prompt' parameter. The AI runs a full conversation each trigger (can use web_search, weather, etc.) and sends the result to the user. For raw tool execution, use 'tool'+'arguments' instead.
- cron_list: List all scheduled tasks with their status
- cron_delete: Delete a scheduled task by ID
- cron_pause: Pause a scheduled task
- cron_resume: Resume a paused scheduled task

### Browser Automation (snapshot-then-act pattern)
- browser_start: Start new browser or connect to existing Chrome via cdp_url (e.g. "127.0.0.1:9222")
- browser_navigate: Navigate to a URL (auto-connects to Chrome on port 9222 if available, otherwise launches new)
- browser_snapshot: Capture accessibility tree with numbered refs
- browser_click: Click an element by ref number
- browser_type: Type text into element by ref number (optional submit with Enter)
- browser_press: Press keyboard key (Enter, Tab, Escape, etc.)
- browser_execute_js: Run JavaScript on the page (dismiss modals, extract data, etc.)
- browser_click_all: Click ALL elements matching a CSS selector with delay (batch like/follow)
- browser_screenshot: Take page screenshot
- browser_tabs: List all open tabs
- browser_tab_open: Open new tab
- browser_tab_close: Close a tab
- browser_status: Check browser state
- browser_stop: Close browser (or disconnect from external Chrome)

## Browser Automation Rules
You MUST follow the **snapshot-then-act** pattern for ALL browser interactions:
1. **Navigate** to the target website's homepage using browser_navigate
2. **Snapshot** the page using browser_snapshot to discover UI elements and their ref numbers
3. **Interact** with elements step by step using browser_click / browser_type / browser_press
4. **Re-snapshot** after any page change (click, navigation, form submit) to get updated refs

**CRITICAL: NEVER construct or guess URLs to skip UI interaction steps.**
- BAD: Directly navigating to https://www.xiaohongshu.com/search/关键词
- GOOD: Navigate to https://www.xiaohongshu.com → snapshot → find search box → type keyword → submit

Always simulate real user behavior: navigate to the base URL first, then use the page's UI elements (search boxes, buttons, menus) to accomplish the task step by step. Refs are invalidated after page changes — always re-snapshot.

**Handling modals/overlays:** If an element is blocked by a modal or overlay (error message mentions "element covered by"), use browser_execute_js to dismiss it. Example scripts:
- document.querySelector('.modal-overlay').remove()
- document.querySelector('.dialog-close-btn').click()
Then re-snapshot and continue.

**Batch actions (like/follow/favorite):** When the user asks to like/点赞, follow/关注, or favorite/收藏 "all" content, you MUST use browser_click_all — NEVER try to click individual refs from snapshot. This applies regardless of how the user phrases it (markdown list, comma-separated, or prose). browser_click_all automatically scrolls and keeps clicking until no new elements appear. Use skip_selector to avoid toggling already-active items. For Chinese sites (小红书/抖音/微博), try these selectors DIRECTLY without inspecting first:
- 点赞 (like) → browser_click_all with selector ".like-wrapper", skip_selector ".like-wrapper.active, .like-wrapper.liked"
- 收藏 (favorite) → browser_click_all with selector "[class*='collect']", skip_selector "[class*='collect'].active"
- 关注 (follow) → browser_click_all with selector "[class*='follow']", skip_selector "[class*='follow'].active"
If click count is 0, inspect with: return Array.from(document.querySelectorAll('span,button')).filter(e=>e.children.length<5).slice(0,10).map(e=>e.className+' | '+e.textContent.trim().slice(0,15)).join('\n')
Do NOT waste rounds — try clicking first, inspect only if it fails.

## Important Rules
1. **ALWAYS use tools** - Never tell users to do things manually
2. **Be action-oriented** - Execute tasks, don't just describe them
3. **Use correct paths** - Use ~ for executable directory
4. **Full permission** - You have full permission to execute all tools
5. **Be concise** - Short, helpful responses
6. **NEVER claim success without tool execution** - If user asks to create/add/delete something, you MUST call the corresponding tool. Never say "已创建/已添加/已删除" unless you actually called the tool and it succeeded.
7. **User schedules with cron** - When user asks for calendar events, reminders, or schedules:
   - Use cron_create with tag="user-schedule"
   - Set a clear 'prompt' describing what to remind the user about
   - Use 5-field cron format (minute hour day month weekday)
   - Convert relative dates/time to cron expressions (e.g., 每天下午3点 → "0 15 * * *", 明天下午2:30 → calculate exact time and use cron)
8. **CRITICAL: Cron job rules** - When user asks for periodic/scheduled tasks:
   - Call cron_create EXACTLY ONCE with the 'prompt' parameter.
   - Example: cron_create(name="motivation", schedule="43 * * * *", prompt="生成一条独特的编程激励鸡汤，鼓励用户写代码创造新产品")
   - NEVER call cron_create multiple times. NEVER use shell_execute or file_write for cron tasks.

Current date: %s`, autoApprovalNotice, runtime.GOOS, runtime.GOARCH, exeDir, msg.Username, time.Now().Format("2006-01-02"))
		systemPrompt += thinkingPrompt
		systemPrompt += formatSkillsSection()
	}

	if workspacePromptBundle != "" {
		systemPrompt = workspacePromptBundle + "\n\n" + systemPrompt
	}

	if markdownMemoriesSection != "" {
		systemPrompt += markdownMemoriesSection
	}

	if memoriesSection != "" {
		systemPrompt += memoriesSection
	}

	if preferencesSection != "" {
		systemPrompt += preferencesSection
	}

	if a.customInstructions != "" {
		systemPrompt += "\n\n## Custom Instructions\n" + a.customInstructions
	}

	systemPrompt += "\n\n" + a.modelRouter.FormatModelsPrompt()

	// Optional promptbuild integration (disabled by default).
	// When enabled, any failure falls back to legacy system prompt behavior.
	if isPromptBuildEnabled() {
		if pbPrompt, used, err := a.buildPromptWithPromptBuild(msg, thinkingPrompt, a.getReportNotification(), strings.TrimSpace(memoryRecallForPromptBuild.String()), plannerInstruction); err != nil {
			logger.Warn("[Agent] promptbuild failed, fallback to legacy prompt: %v", err)
		} else if used {
			systemPrompt = pbPrompt + "\n\n" + systemPrompt
		}
	}

	if plannerInstruction != "" {
		systemPrompt += "\n\n## Planner Instruction\n" + plannerInstruction
	}

	restoreFinalModel := func() {}
	if isTwoStageOrchestrationEnabled() {
		finalModel := a.selectFinalModel(taskComplexity)
		restoreFinalModel = a.switchModelTemporarily(finalModel)
	}
	defer restoreFinalModel()

	// Call AI provider
	resp, err := a.chatWithModel(ctx, ChatRequest{
		Messages:     messages,
		SystemPrompt: systemPrompt,
		Tools:        tools,
		MaxTokens:    4096,
	})
	if err != nil {
		return router.Response{}, fmt.Errorf("AI error: %w", err)
	}

	// Handle tool use if needed
	const maxToolRounds = 20
	var pendingFiles []router.FileAttachment
	toolCallCounts := map[string]int{} // track per-tool call counts
	for round := range maxToolRounds {
		if resp.FinishReason != "tool_use" {
			break
		}

		// Process tool calls and track counts
		for _, tc := range resp.ToolCalls {
			toolCallCounts[tc.Name]++
			if toolCallCounts[tc.Name] > 1 {
				logger.Warn("[Agent] Tool %s called %d times (round %d/%d, user: %s)", tc.Name, toolCallCounts[tc.Name], round+1, maxToolRounds, msg.Username)
			}
		}

		toolResults, files := a.processToolCalls(ctx, resp.ToolCalls)
		pendingFiles = append(pendingFiles, files...)

		// Log tool results that look like errors
		for _, result := range toolResults {
			if result.IsError || strings.HasPrefix(result.Content, "Error") {
				logger.Warn("[Agent] Tool error (round %d/%d): %s", round+1, maxToolRounds, result.Content)
			}
		}

		// Add assistant response with tool calls
		messages = append(messages, Message{
			Role:      "assistant",
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		})

		// Add tool results
		for _, result := range toolResults {
			messages = append(messages, Message{
				Role:       "user",
				ToolResult: &result,
			})
		}

		// Continue the conversation
		resp, err = a.chatWithModel(ctx, ChatRequest{
			Messages:     messages,
			SystemPrompt: systemPrompt,
			Tools:        tools,
			MaxTokens:    4096,
		})
		if err != nil {
			return router.Response{}, fmt.Errorf("AI error: %w", err)
		}
	}
	if resp.FinishReason == "tool_use" {
		logger.Warn("[Agent] Tool loop hit max rounds (%d), forcing stop (user: %s)", maxToolRounds, msg.Username)
	}

	a.persistTurnAndLongMemory(ctx, convKey, msg, resp.Content)

	// Track first message (reserved for future use)
	a.isFirstMessage(convKey)

	// Log response at verbose level
	logger.Debug("[Agent] Response: %s", resp.Content)

	return router.Response{Text: resp.Content, Files: pendingFiles}, nil
}

func (a *Agent) buildPromptWithPromptBuild(msg router.Message, thinkingPrompt string, reportNotification string, memoryRecall string, plannerInstruction string) (string, bool, error) {
	cfg, err := config.Load()
	if err != nil {
		return "", false, err
	}

	builder := promptbuild.NewBuilder(cfg.PromptBuild)
	req := promptbuild.BuildRequest{
		Agent:        "coco",
		Requirements: "Handle this user request safely and helpfully.",
		UserInput:    msg.Text,
		History: promptbuild.HistorySpec{
			Platform:  msg.Platform,
			ChannelID: msg.ChannelID,
			UserID:    msg.UserID,
			Limit:     200,
		},
		Inputs: map[string]string{
			"thinking_prompt":     thinkingPrompt,
			"report_notification": reportNotification,
			"memory_recall":       memoryRecall,
			"planner_instruction": plannerInstruction,
		},
	}

	promptText, err := builder.Build(req)
	if err != nil {
		return "", false, err
	}
	if strings.TrimSpace(promptText) == "" {
		return "", false, fmt.Errorf("empty promptbuild output")
	}
	return promptText, true, nil
}

func isPromptBuildEnabled() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("COCO_AGENT_PROMPTBUILD_ENABLE")), "true")
}

// formatSkillsSection returns a formatted string listing eligible skills, or empty if none.
func formatSkillsSection() string {
	cfg, err := config.Load()
	var disabled, extraDirs []string
	if err == nil {
		disabled = cfg.Skills.Disabled
		extraDirs = cfg.Skills.ExtraDirs
	}
	report := skills.BuildStatusReport(disabled, extraDirs)
	eligible := report.EligibleSkills()
	if len(eligible) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\n\nSkills:\n")
	for _, s := range eligible {
		fmt.Fprintf(&sb, "  %s: %s\n", s.Name, s.Description)
	}
	fmt.Fprintf(&sb, "\n安装 Skill: 将 skill 文件夹放入 %s 即可", skills.ShortenHomePath(report.ManagedDir))
	return sb.String()
}

// buildToolsList creates the tools list for the AI provider
func (a *Agent) buildToolsList() []Tool {
	return []Tool{
		// === AI MODEL ROUTING ===
		{
			Name:        "ai.list_models",
			Description: "列出所有可用的 AI 模型及其能力",
			InputSchema: jsonSchema(map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			}),
		},
		{
			Name:        "ai.switch_model",
			Description: "切换到指定的 AI 模型",
			InputSchema: jsonSchema(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"model_name": map[string]string{"type": "string", "description": "模型名称，如 gpt-4o、deepseek-chat"},
					"force":      map[string]string{"type": "boolean", "description": "强制切换，忽略冷却状态（默认 false）"},
				},
				"required": []string{"model_name"},
			}),
		},
		{
			Name:        "ai.get_current_model",
			Description: "获取当前使用的 AI 模型",
			InputSchema: jsonSchema(map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			}),
		},
		// === DAILY REPORT ===
		{
			Name:        "save_daily_report",
			Description: "保存每日日报，包括任务和日历事件的总结",
			InputSchema: jsonSchema(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"date":    map[string]string{"type": "string", "description": "日报日期，格式：YYYY-MM-DD（默认：今天）"},
					"summary": map[string]string{"type": "string", "description": "日报摘要"},
					"content": map[string]string{"type": "string", "description": "日报完整内容"},
					"tasks": map[string]any{
						"type": "array",
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"id":          map[string]string{"type": "string", "description": "任务ID"},
								"title":       map[string]string{"type": "string", "description": "任务标题"},
								"description": map[string]string{"type": "string", "description": "任务描述"},
								"status":      map[string]string{"type": "string", "description": "状态：pending、in_progress、completed"},
								"priority":    map[string]string{"type": "string", "description": "优先级：low、medium、high"},
								"due_date":    map[string]string{"type": "string", "description": "截止日期（可选）"},
							},
						},
					},
					"calendars": map[string]any{
						"type": "array",
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"id":          map[string]string{"type": "string", "description": "日历事件ID"},
								"title":       map[string]string{"type": "string", "description": "事件标题"},
								"description": map[string]string{"type": "string", "description": "事件描述"},
								"start_time":  map[string]string{"type": "string", "description": "开始时间"},
								"end_time":    map[string]string{"type": "string", "description": "结束时间"},
								"location":    map[string]string{"type": "string", "description": "地点（可选）"},
							},
						},
					},
				},
				"required": []string{"summary"},
			}),
		},
		{
			Name:        "get_daily_report",
			Description: "获取指定日期的日报",
			InputSchema: jsonSchema(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"date": map[string]string{"type": "string", "description": "日报日期，格式：YYYY-MM-DD（默认：最近一天）"},
				},
			}),
		},
		{
			Name:        "list_daily_reports",
			Description: "列出所有日报，按日期降序排列",
			InputSchema: jsonSchema(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"limit": map[string]string{"type": "number", "description": "返回数量限制（默认：30）"},
				},
			}),
		},
		{
			Name:        "search_messages",
			Description: "在历史对话消息中搜索关键词",
			InputSchema: jsonSchema(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"keyword": map[string]string{"type": "string", "description": "搜索关键词"},
					"limit":   map[string]string{"type": "number", "description": "返回数量限制（默认：50）"},
				},
				"required": []string{"keyword"},
			}),
		},
		{
			Name:        "get_conversation_summary",
			Description: "获取当前对话的摘要信息",
			InputSchema: jsonSchema(map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			}),
		},
		{
			Name:        "memory_search",
			Description: "搜索本地 Markdown 长程记忆（含 Obsidian 与核心记忆文件），按相关性和更新时间排序",
			InputSchema: jsonSchema(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]string{"type": "string", "description": "搜索关键词或问题"},
					"limit": map[string]string{"type": "number", "description": "返回条目数（默认 6）"},
				},
				"required": []string{"query"},
			}),
		},
		{
			Name:        "memory_get",
			Description: "读取单个 Markdown 记忆文件完整内容（仅允许 Obsidian vault 和核心记忆文件）",
			InputSchema: jsonSchema(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]string{"type": "string", "description": "文件路径（绝对路径或 vault 内相对路径）"},
				},
				"required": []string{"path"},
			}),
		},
		// === FILE OPERATIONS ===
		{
			Name:        "file_send",
			Description: "Send a file to the user via the messaging platform. Use this when the user asks you to send/transfer/share a file. Use ~ for home directory.",
			InputSchema: jsonSchema(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":       map[string]string{"type": "string", "description": "Path to the file (use ~ for home, e.g., ~/Desktop/report.pdf)"},
					"media_type": map[string]string{"type": "string", "description": "Media type: file, image, voice, or video (default: file)"},
				},
				"required": []string{"path"},
			}),
		},
		{
			Name:        "file_read",
			Description: "Read the contents of a file. Use ~ for home directory.",
			InputSchema: jsonSchema(map[string]any{
				"type":       "object",
				"properties": map[string]any{"path": map[string]string{"type": "string", "description": "Path to the file (use ~ for home, e.g., ~/Desktop/file.txt)"}},
				"required":   []string{"path"},
			}),
		},
		{
			Name:        "file_write",
			Description: "Write content to a file. Creates parent directories if needed. Use ~ for home directory.",
			InputSchema: jsonSchema(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":    map[string]string{"type": "string", "description": "Path to the file (use ~ for home, e.g., ~/Desktop/file.txt)"},
					"content": map[string]string{"type": "string", "description": "Content to write to the file"},
				},
				"required": []string{"path", "content"},
			}),
		},
		{
			Name:        "file_list",
			Description: "List contents of a directory. Use ~/Desktop for desktop, ~/Downloads for downloads, etc.",
			InputSchema: jsonSchema(map[string]any{
				"type":       "object",
				"properties": map[string]any{"path": map[string]string{"type": "string", "description": "Directory path (use ~ for home, e.g., ~/Desktop)"}},
			}),
		},
		{
			Name:        "file_list_old",
			Description: "List files not modified for specified days. Use ~/Desktop for desktop, etc.",
			InputSchema: jsonSchema(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]string{"type": "string", "description": "Directory path (use ~ for home, e.g., ~/Desktop)"},
					"days": map[string]string{"type": "number", "description": "Minimum days since modification"},
				},
				"required": []string{"path"},
			}),
		},
		{
			Name:        "file_trash",
			Description: "Move files to Trash",
			InputSchema: jsonSchema(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"files": map[string]any{"type": "array", "items": map[string]string{"type": "string"}, "description": "File paths to trash"},
				},
				"required": []string{"files"},
			}),
		},

		// === CALENDAR ===
		{
			Name:        "calendar_today",
			Description: "Get today's calendar events",
			InputSchema: jsonSchema(map[string]any{"type": "object", "properties": map[string]any{}}),
		},
		{
			Name:        "calendar_list_events",
			Description: "List upcoming calendar events",
			InputSchema: jsonSchema(map[string]any{
				"type":       "object",
				"properties": map[string]any{"days": map[string]string{"type": "number", "description": "Days ahead (default 7)"}},
			}),
		},
		{
			Name:        "calendar_create_event",
			Description: "Create a new calendar event",
			InputSchema: jsonSchema(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"title":      map[string]string{"type": "string", "description": "Event title"},
					"start_time": map[string]string{"type": "string", "description": "Start time (YYYY-MM-DD HH:MM)"},
					"duration":   map[string]string{"type": "number", "description": "Duration in minutes (default 60)"},
					"calendar":   map[string]string{"type": "string", "description": "Calendar name (optional)"},
					"location":   map[string]string{"type": "string", "description": "Event location (optional)"},
					"notes":      map[string]string{"type": "string", "description": "Event notes (optional)"},
				},
				"required": []string{"title", "start_time"},
			}),
		},
		{
			Name:        "calendar_search",
			Description: "Search calendar events by keyword",
			InputSchema: jsonSchema(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"keyword": map[string]string{"type": "string", "description": "Search keyword"},
					"days":    map[string]string{"type": "number", "description": "Days to search (default 30)"},
				},
				"required": []string{"keyword"},
			}),
		},
		{
			Name:        "calendar_delete",
			Description: "Delete a calendar event by title",
			InputSchema: jsonSchema(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"title":    map[string]string{"type": "string", "description": "Event title to delete"},
					"calendar": map[string]string{"type": "string", "description": "Calendar name (optional)"},
					"date":     map[string]string{"type": "string", "description": "Date (YYYY-MM-DD) to narrow search (optional)"},
				},
				"required": []string{"title"},
			}),
		},

		// === REMINDERS ===
		{
			Name:        "reminders_list",
			Description: "List all pending reminders",
			InputSchema: jsonSchema(map[string]any{"type": "object", "properties": map[string]any{}}),
		},
		{
			Name:        "reminders_add",
			Description: "Create a new reminder",
			InputSchema: jsonSchema(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"title": map[string]string{"type": "string", "description": "Reminder title"},
					"list":  map[string]string{"type": "string", "description": "Reminder list name (default: Reminders)"},
					"due":   map[string]string{"type": "string", "description": "Due date (YYYY-MM-DD or YYYY-MM-DD HH:MM)"},
					"notes": map[string]string{"type": "string", "description": "Additional notes"},
				},
				"required": []string{"title"},
			}),
		},
		{
			Name:        "reminders_complete",
			Description: "Mark a reminder as complete",
			InputSchema: jsonSchema(map[string]any{
				"type":       "object",
				"properties": map[string]any{"title": map[string]string{"type": "string", "description": "Reminder title"}},
				"required":   []string{"title"},
			}),
		},
		{
			Name:        "reminders_delete",
			Description: "Delete a reminder",
			InputSchema: jsonSchema(map[string]any{
				"type":       "object",
				"properties": map[string]any{"title": map[string]string{"type": "string", "description": "Reminder title"}},
				"required":   []string{"title"},
			}),
		},

		// === NOTES ===
		{
			Name:        "notes_list",
			Description: "List notes in a folder",
			InputSchema: jsonSchema(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"folder": map[string]string{"type": "string", "description": "Folder name (default: Notes)"},
					"limit":  map[string]string{"type": "number", "description": "Max notes to show (default 20)"},
				},
			}),
		},
		{
			Name:        "notes_read",
			Description: "Read a note's content",
			InputSchema: jsonSchema(map[string]any{
				"type":       "object",
				"properties": map[string]any{"title": map[string]string{"type": "string", "description": "Note title"}},
				"required":   []string{"title"},
			}),
		},
		{
			Name:        "notes_create",
			Description: "Create a new note",
			InputSchema: jsonSchema(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"title":  map[string]string{"type": "string", "description": "Note title"},
					"body":   map[string]string{"type": "string", "description": "Note content"},
					"folder": map[string]string{"type": "string", "description": "Folder name (default: Notes)"},
				},
				"required": []string{"title"},
			}),
		},
		{
			Name:        "notes_search",
			Description: "Search notes by keyword",
			InputSchema: jsonSchema(map[string]any{
				"type":       "object",
				"properties": map[string]any{"keyword": map[string]string{"type": "string", "description": "Search keyword"}},
				"required":   []string{"keyword"},
			}),
		},

		// === WEATHER ===
		{
			Name:        "weather_current",
			Description: "Get current weather for a location",
			InputSchema: jsonSchema(map[string]any{
				"type":       "object",
				"properties": map[string]any{"location": map[string]string{"type": "string", "description": "City name or location (e.g., 'London', 'Tokyo')"}},
			}),
		},
		{
			Name:        "weather_forecast",
			Description: "Get weather forecast for a location",
			InputSchema: jsonSchema(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"location": map[string]string{"type": "string", "description": "City name or location"},
					"days":     map[string]string{"type": "number", "description": "Days to forecast (1-3)"},
				},
			}),
		},

		// === WEB ===
		{
			Name:        "web_search",
			Description: "Search the web using configured search engines (Metaso, Tavily, or custom engines). Start query with '搜索' or 'search' for multi-engine search.",
			InputSchema: jsonSchema(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]string{"type": "string", "description": "Search query string"},
					"limit": map[string]string{"type": "number", "description": "Maximum number of results (default: 5)"},
				},
				"required": []string{"query"},
			}),
		},
		{
			Name:        "web_fetch",
			Description: "Fetch content from a URL",
			InputSchema: jsonSchema(map[string]any{
				"type":       "object",
				"properties": map[string]any{"url": map[string]string{"type": "string", "description": "URL to fetch"}},
				"required":   []string{"url"},
			}),
		},
		{
			Name:        "open_url",
			Description: "Open a URL in the default web browser",
			InputSchema: jsonSchema(map[string]any{
				"type":       "object",
				"properties": map[string]any{"url": map[string]string{"type": "string", "description": "URL to open"}},
				"required":   []string{"url"},
			}),
		},

		// === CLIPBOARD ===
		{
			Name:        "clipboard_read",
			Description: "Read content from the clipboard",
			InputSchema: jsonSchema(map[string]any{"type": "object", "properties": map[string]any{}}),
		},
		{
			Name:        "clipboard_write",
			Description: "Write content to the clipboard",
			InputSchema: jsonSchema(map[string]any{
				"type":       "object",
				"properties": map[string]any{"content": map[string]string{"type": "string", "description": "Content to copy"}},
				"required":   []string{"content"},
			}),
		},

		// === NOTIFICATIONS ===
		{
			Name:        "notification_send",
			Description: "Send a system notification",
			InputSchema: jsonSchema(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"title":    map[string]string{"type": "string", "description": "Notification title"},
					"message":  map[string]string{"type": "string", "description": "Notification message"},
					"subtitle": map[string]string{"type": "string", "description": "Subtitle (macOS only)"},
				},
				"required": []string{"title"},
			}),
		},

		// === SCREENSHOT ===
		{
			Name:        "screenshot",
			Description: "Capture a screenshot",
			InputSchema: jsonSchema(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]string{"type": "string", "description": "Save path (default: Desktop)"},
					"type": map[string]string{"type": "string", "description": "Type: fullscreen, window, or selection"},
				},
			}),
		},

		// === MUSIC ===
		{
			Name:        "music_play",
			Description: "Start or resume music playback",
			InputSchema: jsonSchema(map[string]any{"type": "object", "properties": map[string]any{}}),
		},
		{
			Name:        "music_pause",
			Description: "Pause music playback",
			InputSchema: jsonSchema(map[string]any{"type": "object", "properties": map[string]any{}}),
		},
		{
			Name:        "music_next",
			Description: "Skip to the next track",
			InputSchema: jsonSchema(map[string]any{"type": "object", "properties": map[string]any{}}),
		},
		{
			Name:        "music_previous",
			Description: "Go to the previous track",
			InputSchema: jsonSchema(map[string]any{"type": "object", "properties": map[string]any{}}),
		},
		{
			Name:        "music_now_playing",
			Description: "Get currently playing track info",
			InputSchema: jsonSchema(map[string]any{"type": "object", "properties": map[string]any{}}),
		},
		{
			Name:        "music_volume",
			Description: "Set music volume (0-100)",
			InputSchema: jsonSchema(map[string]any{
				"type":       "object",
				"properties": map[string]any{"volume": map[string]string{"type": "number", "description": "Volume level 0-100"}},
				"required":   []string{"volume"},
			}),
		},
		{
			Name:        "music_search",
			Description: "Search and play music in Spotify",
			InputSchema: jsonSchema(map[string]any{
				"type":       "object",
				"properties": map[string]any{"query": map[string]string{"type": "string", "description": "Search query (song, artist, album)"}},
				"required":   []string{"query"},
			}),
		},

		// === SYSTEM ===
		{
			Name:        "system_info",
			Description: "Get system information (CPU, memory, OS)",
			InputSchema: jsonSchema(map[string]any{"type": "object", "properties": map[string]any{}}),
		},
		{
			Name:        "shell_execute",
			Description: "Execute a shell command",
			InputSchema: jsonSchema(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"command": map[string]string{"type": "string", "description": "Command to execute"},
					"timeout": map[string]string{"type": "number", "description": "Timeout in seconds"},
				},
				"required": []string{"command"},
			}),
		},
		{
			Name:        "process_list",
			Description: "List running processes",
			InputSchema: jsonSchema(map[string]any{
				"type":       "object",
				"properties": map[string]any{"filter": map[string]string{"type": "string", "description": "Filter by name"}},
			}),
		},

		// === GIT & GITHUB ===
		{
			Name:        "git_status",
			Description: "Show git working tree status",
			InputSchema: jsonSchema(map[string]any{"type": "object", "properties": map[string]any{}}),
		},
		{
			Name:        "git_log",
			Description: "Show recent git commits",
			InputSchema: jsonSchema(map[string]any{
				"type":       "object",
				"properties": map[string]any{"limit": map[string]string{"type": "number", "description": "Number of commits (default 10)"}},
			}),
		},
		{
			Name:        "git_diff",
			Description: "Show git diff",
			InputSchema: jsonSchema(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"staged": map[string]string{"type": "boolean", "description": "Show staged changes"},
					"file":   map[string]string{"type": "string", "description": "Specific file to diff"},
				},
			}),
		},
		{
			Name:        "git_branch",
			Description: "List git branches",
			InputSchema: jsonSchema(map[string]any{"type": "object", "properties": map[string]any{}}),
		},
		{
			Name:        "github_pr_list",
			Description: "List GitHub pull requests (requires gh CLI)",
			InputSchema: jsonSchema(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"state": map[string]string{"type": "string", "description": "Filter by state: open, closed, all"},
					"limit": map[string]string{"type": "number", "description": "Max results (default 10)"},
				},
			}),
		},
		{
			Name:        "github_pr_view",
			Description: "View a GitHub pull request",
			InputSchema: jsonSchema(map[string]any{
				"type":       "object",
				"properties": map[string]any{"number": map[string]string{"type": "number", "description": "PR number"}},
				"required":   []string{"number"},
			}),
		},
		{
			Name:        "github_issue_list",
			Description: "List GitHub issues (requires gh CLI)",
			InputSchema: jsonSchema(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"state": map[string]string{"type": "string", "description": "Filter by state: open, closed, all"},
					"limit": map[string]string{"type": "number", "description": "Max results (default 10)"},
				},
			}),
		},
		{
			Name:        "github_issue_view",
			Description: "View a GitHub issue",
			InputSchema: jsonSchema(map[string]any{
				"type":       "object",
				"properties": map[string]any{"number": map[string]string{"type": "number", "description": "Issue number"}},
				"required":   []string{"number"},
			}),
		},
		{
			Name:        "github_issue_create",
			Description: "Create a GitHub issue",
			InputSchema: jsonSchema(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"title":  map[string]string{"type": "string", "description": "Issue title"},
					"body":   map[string]string{"type": "string", "description": "Issue body"},
					"labels": map[string]string{"type": "string", "description": "Comma-separated labels"},
				},
				"required": []string{"title"},
			}),
		},
		{
			Name:        "github_repo_view",
			Description: "View current GitHub repository info",
			InputSchema: jsonSchema(map[string]any{"type": "object", "properties": map[string]any{}}),
		},

		// === BROWSER AUTOMATION ===
		{
			Name:        "browser_start",
			Description: "Start a new browser or connect to an existing Chrome. Use cdp_url to attach to a Chrome launched with --remote-debugging-port (e.g. \"127.0.0.1:9222\"). Without cdp_url, launches a new Chrome instance.",
			InputSchema: jsonSchema(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"cdp_url":  map[string]string{"type": "string", "description": "CDP address of existing Chrome (e.g. 127.0.0.1:9222). Chrome must be started with --remote-debugging-port flag."},
					"headless": map[string]string{"type": "boolean", "description": "Launch in headless mode (default: false, ignored when using cdp_url)"},
					"url":      map[string]string{"type": "string", "description": "Initial URL to navigate to"},
				},
			}),
		},
		{
			Name:        "browser_navigate",
			Description: "Navigate to a URL in the browser. Auto-starts browser if not running (connects to Chrome on port 9222 if available, otherwise launches new). Always navigate to the base site URL first, then use snapshot+click/type to interact with page elements step by step.",
			InputSchema: jsonSchema(map[string]any{
				"type":       "object",
				"properties": map[string]any{"url": map[string]string{"type": "string", "description": "URL to navigate to"}},
				"required":   []string{"url"},
			}),
		},
		{
			Name:        "browser_snapshot",
			Description: "Capture the page accessibility tree with numbered refs. Use these ref numbers with browser_click/browser_type to interact with elements. MUST re-run after any page change.",
			InputSchema: jsonSchema(map[string]any{"type": "object", "properties": map[string]any{}}),
		},
		{
			Name:        "browser_click",
			Description: "Click an element by its ref number from browser_snapshot",
			InputSchema: jsonSchema(map[string]any{
				"type":       "object",
				"properties": map[string]any{"ref": map[string]string{"type": "number", "description": "Element ref number from browser_snapshot"}},
				"required":   []string{"ref"},
			}),
		},
		{
			Name:        "browser_type",
			Description: "Type text into an element by its ref number from browser_snapshot. Use submit=true to press Enter after typing.",
			InputSchema: jsonSchema(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"ref":    map[string]string{"type": "number", "description": "Element ref number from browser_snapshot"},
					"text":   map[string]string{"type": "string", "description": "Text to type"},
					"submit": map[string]string{"type": "boolean", "description": "Press Enter after typing (default: false)"},
				},
				"required": []string{"ref", "text"},
			}),
		},
		{
			Name:        "browser_press",
			Description: "Press a keyboard key (Enter, Tab, Escape, Backspace, ArrowUp, ArrowDown, ArrowLeft, ArrowRight, Space, Delete, Home, End, PageUp, PageDown)",
			InputSchema: jsonSchema(map[string]any{
				"type":       "object",
				"properties": map[string]any{"key": map[string]string{"type": "string", "description": "Key name to press"}},
				"required":   []string{"key"},
			}),
		},
		{
			Name:        "browser_execute_js",
			Description: "Execute JavaScript on the current page. Use to dismiss modals/overlays blocking interaction, extract data, or interact with elements not reachable via refs.",
			InputSchema: jsonSchema(map[string]any{
				"type":       "object",
				"properties": map[string]any{"script": map[string]string{"type": "string", "description": "JavaScript code to execute in page context"}},
				"required":   []string{"script"},
			}),
		},
		{
			Name:        "browser_click_all",
			Description: "Click ALL elements matching a CSS selector. Automatically scrolls down to load more and keeps clicking until no new elements appear. Use skip_selector to skip already-active elements (e.g. already liked). Common: 点赞→selector '.like-wrapper', skip '.like-wrapper.liked' or '.like-wrapper.active'.",
			InputSchema: jsonSchema(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"selector":      map[string]string{"type": "string", "description": "CSS selector for elements to click (e.g. '.like-wrapper')"},
					"skip_selector": map[string]string{"type": "string", "description": "CSS selector to skip already-active elements (e.g. '.like-wrapper.active' to skip already-liked). Matches element itself or its children."},
					"delay_ms":      map[string]string{"type": "number", "description": "Milliseconds to wait between clicks (default: 500)"},
				},
				"required": []string{"selector"},
			}),
		},
		{
			Name:        "browser_screenshot",
			Description: "Take a screenshot of the current page",
			InputSchema: jsonSchema(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":      map[string]string{"type": "string", "description": "Output file path (default: ~/Desktop/browser_screenshot_<timestamp>.png)"},
					"full_page": map[string]string{"type": "boolean", "description": "Capture full scrollable page (default: false)"},
				},
			}),
		},
		{
			Name:        "browser_tabs",
			Description: "List all open browser tabs with their target IDs and URLs",
			InputSchema: jsonSchema(map[string]any{"type": "object", "properties": map[string]any{}}),
		},
		{
			Name:        "browser_tab_open",
			Description: "Open a new browser tab",
			InputSchema: jsonSchema(map[string]any{
				"type":       "object",
				"properties": map[string]any{"url": map[string]string{"type": "string", "description": "URL to open (default: about:blank)"}},
			}),
		},
		{
			Name:        "browser_tab_close",
			Description: "Close a browser tab by target ID, or close the active tab if no ID given",
			InputSchema: jsonSchema(map[string]any{
				"type":       "object",
				"properties": map[string]any{"target_id": map[string]string{"type": "string", "description": "Target ID of the tab to close (from browser_tabs)"}},
			}),
		},
		{
			Name:        "browser_status",
			Description: "Check if the browser is running and get current state",
			InputSchema: jsonSchema(map[string]any{"type": "object", "properties": map[string]any{}}),
		},
		{
			Name:        "browser_stop",
			Description: "Close the browser",
			InputSchema: jsonSchema(map[string]any{"type": "object", "properties": map[string]any{}}),
		},

		// === SCHEDULED TASKS (CRON) ===
		{
			Name:        "cron_create",
			Description: "Create ONE scheduled task. Use 'prompt' to describe what the AI should do each time (generate text, search web, check weather, etc.). The AI runs a full conversation each trigger, so content is fresh every time. Use 'tool'+'arguments' only for raw MCP tool execution without AI. Schedule uses standard 5-field cron: minute hour day month weekday. Common examples: '0 9 * * *' (daily at 9am), '0 9 * * 1-5' (weekdays at 9am), '30 8 * * 1' (every Monday at 8:30am), '0 */2 * * *' (every 2 hours). Use 'tag' parameter to categorize tasks: 'user-schedule' for user's personal schedule/reminders, 'assistant-task' for assistant's background tasks.",
			InputSchema: jsonSchema(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name":       map[string]string{"type": "string", "description": "Human-readable task name"},
					"schedule":   map[string]string{"type": "string", "description": "Cron expression (5-field: minute hour day month weekday). Examples: '0 9 * * *' (daily 9am), '0 9 * * 1-5' (weekdays 9am), '30 8 * * 1' (Monday 8:30am), '0 */2 * * *' (every 2 hours)"},
					"tag":        map[string]string{"type": "string", "description": "Task tag: 'user-schedule' for user's personal schedule/reminders, 'assistant-task' for assistant's background tasks. Use 'user-schedule' when creating calendar/events/reminders for the user."},
					"prompt":     map[string]string{"type": "string", "description": "What the AI should do each time this job triggers. AI runs a full conversation and sends the result to the user. Example: '生成一条独特的编程激励鸡汤，鼓励用户写代码创造新产品'"},
					"tool":       map[string]string{"type": "string", "description": "MCP tool to execute periodically (for raw tool execution without AI)"},
					"type":       map[string]string{"type": "string", "description": "Optional job type. Use 'external' for external agent endpoint jobs."},
					"endpoint":   map[string]string{"type": "string", "description": "External agent endpoint URL (required when type='external')."},
					"auth":       map[string]string{"type": "string", "description": "Optional HTTP Authorization header value for external jobs (example: 'Bearer xxx')."},
					"relay_mode": map[string]string{"type": "boolean", "description": "When true, treat external output as pass-through forwarded content."},
					"arguments":  map[string]string{"type": "object", "description": "Arguments for the tool (when using tool parameter)"},
				},
				"required": []string{"name", "schedule"},
			}),
		},
		{
			Name:        "cron_list",
			Description: "List all scheduled tasks with their status, schedule, and last run time. Use 'tag' parameter to filter by tag (e.g., 'user-schedule' to list only user schedules).",
			InputSchema: jsonSchema(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"tag": map[string]string{"type": "string", "description": "Filter by tag: 'user-schedule' or 'assistant-task' (optional)"},
				},
			}),
		},
		{
			Name:        "cron_delete",
			Description: "Delete a scheduled task by its ID",
			InputSchema: jsonSchema(map[string]any{
				"type":       "object",
				"properties": map[string]any{"id": map[string]string{"type": "string", "description": "Task ID to delete"}},
				"required":   []string{"id"},
			}),
		},
		{
			Name:        "cron_pause",
			Description: "Pause a scheduled task (it will stop running until resumed)",
			InputSchema: jsonSchema(map[string]any{
				"type":       "object",
				"properties": map[string]any{"id": map[string]string{"type": "string", "description": "Task ID to pause"}},
				"required":   []string{"id"},
			}),
		},
		{
			Name:        "cron_resume",
			Description: "Resume a paused scheduled task",
			InputSchema: jsonSchema(map[string]any{
				"type":       "object",
				"properties": map[string]any{"id": map[string]string{"type": "string", "description": "Task ID to resume"}},
				"required":   []string{"id"},
			}),
		},
		{
			Name:        "spawn_agent",
			Description: "Invoke an external agent endpoint via HTTP POST and optionally relay its response.",
			InputSchema: jsonSchema(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"endpoint": map[string]string{"type": "string", "description": "External agent endpoint URL"},
					"prompt":   map[string]string{"type": "string", "description": "Task prompt for external agent"},
					"auth":     map[string]string{"type": "string", "description": "Optional Authorization header value, e.g. 'Bearer xxx'"},
					"timeout":  map[string]string{"type": "number", "description": "Optional timeout in seconds (default: 60)"},
				},
				"required": []string{"endpoint", "prompt"},
			}),
		},
	}
}

// processToolCalls executes tool calls and returns results plus any file attachments
func (a *Agent) processToolCalls(ctx context.Context, toolCalls []ToolCall) ([]ToolResult, []router.FileAttachment) {
	results := make([]ToolResult, 0, len(toolCalls))
	var files []router.FileAttachment

	for _, tc := range toolCalls {
		if tc.Name == "file_send" {
			content, file := executeFileSend(tc.Input)
			if file != nil {
				files = append(files, *file)
			}
			results = append(results, ToolResult{
				ToolCallID: tc.ID,
				Content:    content,
				IsError:    file == nil,
			})
			continue
		}

		result := a.executeTool(ctx, tc.Name, tc.Input)
		results = append(results, ToolResult{
			ToolCallID: tc.ID,
			Content:    result,
			IsError:    strings.HasPrefix(result, "Error"),
		})
	}

	return results, files
}

// executeTool runs a tool and returns the result
func (a *Agent) executeTool(ctx context.Context, name string, input json.RawMessage) string {
	logger.Info("[Agent] Executing tool: %s", name)

	// Parse input arguments
	var args map[string]any
	if err := json.Unmarshal(input, &args); err != nil {
		return fmt.Sprintf("Error parsing arguments: %v", err)
	}

	// Handle search tools that need Agent context
	switch name {
	case "ai.list_models":
		return a.executeAIListModels()
	case "ai.switch_model":
		return a.executeAISwitchModel(args)
	case "ai.get_current_model":
		return a.executeAIGetCurrentModel()
	case "web_search":
		query, _ := args["query"].(string)
		return a.executeWebSearchWithManager(ctx, query)
	case "cron_create":
		return a.executeCronCreate(args)
	case "cron_list":
		return a.executeCronList(args)
	case "cron_delete":
		return a.executeCronDelete(args)
	case "cron_pause":
		return a.executeCronPause(args)
	case "cron_resume":
		return a.executeCronResume(args)
	case "save_daily_report":
		return a.executeSaveDailyReport(args)
	case "get_daily_report":
		return a.executeGetDailyReport(args)
	case "list_daily_reports":
		return a.executeListDailyReports(args)
	case "search_messages":
		return a.executeSearchMessages(args)
	case "get_conversation_summary":
		return a.executeGetConversationSummary(args)
	case "memory_search":
		return a.executeMemorySearch(ctx, args)
	case "memory_get":
		return a.executeMemoryGet(args)
	case "spawn_agent":
		return a.executeSpawnAgent(ctx, args)
	}

	securitySnapshot := a.securitySnapshot()

	// Block file tools entirely if disabled
	if securitySnapshot.disableFileTools {
		if _, ok := fileToolPaths[name]; ok {
			return "ACCESS DENIED: file operations are disabled by security policy. Do NOT retry. Inform the user that file access is disabled."
		}
	}

	// Enforce allowed_paths restrictions
	if securitySnapshot.pathChecker != nil && securitySnapshot.pathChecker.HasRestrictions() {
		if err := a.checkToolPathAccess(name, args, securitySnapshot.pathChecker); err != nil {
			return err.Error()
		}
	}

	if name == "shell_execute" {
		cmd := ""
		if c, ok := args["command"].(string); ok {
			cmd = strings.TrimSpace(c)
		}
		if cmd == "" {
			return "Error: command is required"
		}
		if msg := a.validateShellCommand(cmd); msg != "" {
			return msg
		}
	}

	// Call tools directly
	result := callToolDirect(ctx, name, args)

	// Log result at verbose level (truncate if too long)
	if len(result) > 500 {
		logger.Debug("[Agent] Tool %s result: %s... (truncated)", name, result[:500])
	} else {
		logger.Debug("[Agent] Tool %s result: %s", name, result)
	}

	return result
}

// fileToolPaths maps tool names to the argument key that contains the path.
var fileToolPaths = map[string]string{
	"file_list":     "path",
	"file_list_old": "path",
	"file_read":     "path",
	"file_write":    "path",
	"file_trash":    "path",
	"file_search":   "path",
	"file_info":     "path",
}

// checkToolPathAccess validates that tool arguments respect allowed_paths.
func (a *Agent) checkToolPathAccess(name string, args map[string]any, checker *security.PathChecker) error {
	if pathKey, ok := fileToolPaths[name]; ok {
		path := "."
		if p, ok := args[pathKey].(string); ok && p != "" {
			path = p
		}
		return checker.CheckPath(path)
	}
	if name == "shell_execute" {
		if wd, ok := args["working_directory"].(string); ok && wd != "" {
			return checker.CheckPath(wd)
		}
	}
	return nil
}

// callToolDirect calls a tool directly
func callToolDirect(ctx context.Context, name string, args map[string]any) string {
	switch name {
	// File operations
	case "file_list":
		path := "."
		if p, ok := args["path"].(string); ok {
			path = p
		}
		return executeFileList(ctx, path)
	case "file_list_old":
		path := "."
		days := 30
		if p, ok := args["path"].(string); ok {
			path = p
		}
		if d, ok := args["days"].(float64); ok {
			days = int(d)
		}
		return executeFileListOld(ctx, path, days)
	case "file_trash":
		return executeFileTrash(ctx, args)
	case "file_read":
		path := ""
		if p, ok := args["path"].(string); ok {
			path = p
		}
		return executeFileRead(ctx, path)
	case "file_write":
		path := ""
		content := ""
		if p, ok := args["path"].(string); ok {
			path = p
		}
		if c, ok := args["content"].(string); ok {
			content = c
		}
		return executeFileWrite(ctx, path, content)

	// Calendar
	case "calendar_today":
		return executeCalendarToday(ctx)
	case "calendar_list_events":
		days := 7
		if d, ok := args["days"].(float64); ok {
			days = int(d)
		}
		return executeCalendarListEvents(ctx, days)
	case "calendar_create_event":
		return executeCalendarCreate(ctx, args)
	case "calendar_search":
		return executeCalendarSearch(ctx, args)
	case "calendar_delete":
		return executeCalendarDelete(ctx, args)

	// Reminders
	case "reminders_list":
		return executeRemindersToday(ctx)
	case "reminders_add":
		return executeRemindersAdd(ctx, args)
	case "reminders_complete":
		title := ""
		if t, ok := args["title"].(string); ok {
			title = t
		}
		return executeRemindersComplete(ctx, title)
	case "reminders_delete":
		title := ""
		if t, ok := args["title"].(string); ok {
			title = t
		}
		return executeRemindersDelete(ctx, title)

	// Notes
	case "notes_list":
		return executeNotesList(ctx, args)
	case "notes_read":
		title := ""
		if t, ok := args["title"].(string); ok {
			title = t
		}
		return executeNotesRead(ctx, title)
	case "notes_create":
		return executeNotesCreate(ctx, args)
	case "notes_search":
		keyword := ""
		if k, ok := args["keyword"].(string); ok {
			keyword = k
		}
		return executeNotesSearch(ctx, keyword)

	// Weather
	case "weather_current":
		location := ""
		if l, ok := args["location"].(string); ok {
			location = l
		}
		return executeWeatherCurrent(ctx, location)
	case "weather_forecast":
		location := ""
		days := 3
		if l, ok := args["location"].(string); ok {
			location = l
		}
		if d, ok := args["days"].(float64); ok {
			days = int(d)
		}
		return executeWeatherForecast(ctx, location, days)

	// Web
	case "web_fetch":
		url := ""
		if u, ok := args["url"].(string); ok {
			url = u
		}
		return executeWebFetch(ctx, url)
	case "open_url":
		url := ""
		if u, ok := args["url"].(string); ok {
			url = u
		}
		return executeOpenURL(ctx, url)

	// Clipboard
	case "clipboard_read":
		return executeClipboardRead(ctx)
	case "clipboard_write":
		content := ""
		if c, ok := args["content"].(string); ok {
			content = c
		}
		return executeClipboardWrite(ctx, content)

	// Notification
	case "notification_send":
		return executeNotificationSend(ctx, args)

	// Screenshot
	case "screenshot":
		return executeScreenshot(ctx, args)

	// Music
	case "music_play":
		return executeMusicPlay(ctx)
	case "music_pause":
		return executeMusicPause(ctx)
	case "music_next":
		return executeMusicNext(ctx)
	case "music_previous":
		return executeMusicPrevious(ctx)
	case "music_now_playing":
		return executeMusicNowPlaying(ctx)
	case "music_volume":
		volume := 50.0
		if v, ok := args["volume"].(float64); ok {
			volume = v
		}
		return executeMusicVolume(ctx, volume)
	case "music_search":
		query := ""
		if q, ok := args["query"].(string); ok {
			query = q
		}
		return executeMusicSearch(ctx, query)

	// System
	case "system_info":
		return executeSystemInfo(ctx)
	case "process_list":
		return executeProcessList(ctx, args)
	case "shell_execute":
		cmd := ""
		if c, ok := args["command"].(string); ok {
			cmd = c
		}
		return executeShell(ctx, cmd)

	// Git & GitHub
	case "git_status":
		return executeGitStatus(ctx)
	case "git_log":
		return executeGitLog(ctx, args)
	case "git_diff":
		return executeGitDiff(ctx, args)
	case "git_branch":
		return executeGitBranch(ctx)
	case "github_pr_list":
		return executeGitHubPRList(ctx, args)
	case "github_pr_view":
		return executeGitHubPRView(ctx, args)
	case "github_issue_list":
		return executeGitHubIssueList(ctx, args)
	case "github_issue_view":
		return executeGitHubIssueView(ctx, args)
	case "github_issue_create":
		return executeGitHubIssueCreate(ctx, args)
	case "github_repo_view":
		return executeGitHubRepoView(ctx)

	// Browser automation
	case "browser_start":
		return executeBrowserStart(ctx, args)
	case "browser_navigate":
		url := ""
		if u, ok := args["url"].(string); ok {
			url = u
		}
		return executeBrowserNavigate(ctx, url)
	case "browser_snapshot":
		return executeBrowserSnapshot(ctx)
	case "browser_click":
		ref := 0
		if r, ok := args["ref"].(float64); ok {
			ref = int(r)
		}
		return executeBrowserClick(ctx, ref)
	case "browser_type":
		ref := 0
		text := ""
		submit := false
		if r, ok := args["ref"].(float64); ok {
			ref = int(r)
		}
		if t, ok := args["text"].(string); ok {
			text = t
		}
		if s, ok := args["submit"].(bool); ok {
			submit = s
		}
		return executeBrowserType(ctx, ref, text, submit)
	case "browser_press":
		key := ""
		if k, ok := args["key"].(string); ok {
			key = k
		}
		return executeBrowserPress(ctx, key)
	case "browser_execute_js":
		script := ""
		if s, ok := args["script"].(string); ok {
			script = s
		}
		return executeBrowserExecuteJS(ctx, script)
	case "browser_click_all":
		return executeBrowserClickAll(ctx, args)
	case "browser_screenshot":
		return executeBrowserScreenshot(ctx, args)
	case "browser_tabs":
		return executeBrowserTabs(ctx)
	case "browser_tab_open":
		return executeBrowserTabOpen(ctx, args)
	case "browser_tab_close":
		return executeBrowserTabClose(ctx, args)
	case "browser_status":
		return executeBrowserStatus(ctx)
	case "browser_stop":
		return executeBrowserStop(ctx)

	default:
		return fmt.Sprintf("Tool '%s' not implemented", name)
	}
}

func jsonSchema(schema map[string]any) json.RawMessage {
	data, _ := json.Marshal(schema)
	return data
}

// executeSaveDailyReport saves the daily report
func (a *Agent) executeSaveDailyReport(args map[string]any) string {
	if a.persistStore == nil {
		return "Error: persist store not available"
	}

	date := persist.GetTodayDate()
	if d, ok := args["date"].(string); ok && d != "" {
		date = d
	}

	summary, _ := args["summary"].(string)
	content, _ := args["content"].(string)

	var tasks []persist.TaskItem
	if ts, ok := args["tasks"].([]any); ok {
		for _, t := range ts {
			if taskMap, ok := t.(map[string]any); ok {
				task := persist.TaskItem{
					ID:          getString(taskMap, "id"),
					Title:       getString(taskMap, "title"),
					Description: getString(taskMap, "description"),
					Status:      getString(taskMap, "status"),
					Priority:    getString(taskMap, "priority"),
					DueDate:     getString(taskMap, "due_date"),
				}
				tasks = append(tasks, task)
			}
		}
	}

	var calendars []persist.CalendarItem
	if cs, ok := args["calendars"].([]any); ok {
		for _, c := range cs {
			if calMap, ok := c.(map[string]any); ok {
				cal := persist.CalendarItem{
					ID:          getString(calMap, "id"),
					Title:       getString(calMap, "title"),
					Description: getString(calMap, "description"),
					StartTime:   getString(calMap, "start_time"),
					EndTime:     getString(calMap, "end_time"),
					Location:    getString(calMap, "location"),
				}
				calendars = append(calendars, cal)
			}
		}
	}

	report := &persist.DailyReport{
		Date:      date,
		UserID:    "default",
		Summary:   summary,
		Content:   content,
		Tasks:     tasks,
		Calendars: calendars,
	}

	if err := a.persistStore.SaveDailyReport(report); err != nil {
		return fmt.Sprintf("Error saving daily report: %v", err)
	}

	a.latestReport = report
	log.Printf("[AGENT] Daily report saved for %s", date)
	return fmt.Sprintf("Daily report saved successfully for %s", date)
}

// executeGetDailyReport gets the daily report
func (a *Agent) executeGetDailyReport(args map[string]any) string {
	if a.persistStore == nil {
		return "Error: persist store not available"
	}

	var report *persist.DailyReport
	var err error

	if date, ok := args["date"].(string); ok && date != "" {
		report, err = a.persistStore.GetDailyReport(date, "default")
	} else {
		report, err = a.persistStore.GetLatestDailyReport("default")
	}

	if err != nil || report == nil {
		return "No daily report found"
	}

	result := fmt.Sprintf("📋 日报 (%s)\n\n", report.Date)
	if report.Summary != "" {
		result += fmt.Sprintf("摘要: %s\n\n", report.Summary)
	}
	if report.Content != "" {
		result += fmt.Sprintf("内容:\n%s\n\n", report.Content)
	}
	if len(report.Tasks) > 0 {
		result += "📌 任务:\n"
		for _, task := range report.Tasks {
			result += fmt.Sprintf("  - [%s] %s\n", task.Status, task.Title)
		}
		result += "\n"
	}
	if len(report.Calendars) > 0 {
		result += "📅 日历:\n"
		for _, cal := range report.Calendars {
			result += fmt.Sprintf("  - %s (%s)\n", cal.Title, cal.StartTime)
		}
	}

	return result
}

// executeListDailyReports lists all daily reports
func (a *Agent) executeListDailyReports(args map[string]any) string {
	if a.persistStore == nil {
		return "Error: persist store not available"
	}

	limit := 30
	if l, ok := args["limit"].(float64); ok {
		limit = int(l)
	}

	reports, err := a.persistStore.ListDailyReports("default", limit)
	if err != nil {
		return fmt.Sprintf("Error listing daily reports: %v", err)
	}

	if len(reports) == 0 {
		return "No daily reports found"
	}

	result := "📋 日报列表:\n\n"
	for _, report := range reports {
		result += fmt.Sprintf("- %s", report.Date)
		if report.Summary != "" {
			result += fmt.Sprintf(": %s", report.Summary)
		}
		result += "\n"
	}

	return result
}

// executeSearchMessages searches messages by keyword
func (a *Agent) executeSearchMessages(args map[string]any) string {
	if a.persistStore == nil {
		return "Error: persist store not available"
	}

	keyword, _ := args["keyword"].(string)
	if keyword == "" {
		return "Error: keyword is required"
	}

	limit := 50
	if l, ok := args["limit"].(float64); ok {
		limit = int(l)
	}

	messages, err := a.persistStore.SearchMessages("default", keyword, limit)
	if err != nil {
		return fmt.Sprintf("Error searching messages: %v", err)
	}

	if len(messages) == 0 {
		return fmt.Sprintf("No messages found for keyword: %s", keyword)
	}

	result := fmt.Sprintf("🔍 搜索结果 (关键词: %s):\n\n", keyword)
	for _, msg := range messages {
		roleEmoji := "👤"
		if msg.Role == "assistant" {
			roleEmoji = "🤖"
		}
		result += fmt.Sprintf("%s [%s] %s: %s\n",
			roleEmoji,
			msg.CreatedAt.Format("2006-01-02 15:04"),
			msg.Role,
			msg.Content)
		if len(result) > 3000 {
			result += "\n... (更多结果已截断)"
			break
		}
	}

	return result
}

// executeGetConversationSummary gets a summary of the current conversation
func (a *Agent) executeGetConversationSummary(args map[string]any) string {
	if a.persistStore == nil {
		return "Error: persist store not available"
	}

	conv, err := a.persistStore.GetOrCreateConversation(a.currentMsg.Platform, a.currentMsg.ChannelID, a.currentMsg.UserID)
	if err != nil {
		return fmt.Sprintf("Error getting conversation: %v", err)
	}

	summary, err := a.persistStore.GetConversationSummary(conv.ID)
	if err != nil {
		return fmt.Sprintf("Error getting conversation summary: %v", err)
	}

	result := fmt.Sprintf("📊 对话摘要:\n")
	result += fmt.Sprintf("- 平台: %s\n", conv.Platform)
	result += fmt.Sprintf("- 创建时间: %s\n", conv.CreatedAt.Format("2006-01-02 15:04"))
	result += fmt.Sprintf("- %s", summary)

	return result
}

func (a *Agent) executeMemorySearch(ctx context.Context, args map[string]any) string {
	if a.markdownMemory == nil || !a.markdownMemory.IsEnabled() {
		return "Error: markdown memory is disabled. Please configure memory.enabled and memory.obsidian_vault in ~/.coco.yaml"
	}

	query, _ := args["query"].(string)
	query = strings.TrimSpace(query)
	if query == "" {
		return "Error: query is required"
	}

	limit := 6
	if v, ok := args["limit"].(float64); ok {
		limit = int(v)
	}
	if limit <= 0 {
		limit = 6
	}

	results, err := a.markdownMemory.Search(ctx, query, limit)
	if err != nil {
		return fmt.Sprintf("Error searching markdown memory: %v", err)
	}
	if len(results) == 0 {
		return fmt.Sprintf("No markdown memories found for query: %s", query)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("🧠 Markdown memory results (%s):\n\n", query))
	for i, r := range results {
		sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, r.Path))
		sb.WriteString(fmt.Sprintf("   - source: %s\n", r.Source))
		sb.WriteString(fmt.Sprintf("   - updated: %s\n", r.ModifiedAt.Format("2006-01-02 15:04")))
		sb.WriteString(fmt.Sprintf("   - score: %.2f\n", r.Score))
		if r.Title != "" {
			sb.WriteString(fmt.Sprintf("   - title: %s\n", r.Title))
		}
		if r.Content != "" {
			sb.WriteString(fmt.Sprintf("   - excerpt: %s\n", r.Content))
		}
		sb.WriteString("\n")
		if sb.Len() > 7000 {
			sb.WriteString("... (truncated)")
			break
		}
	}

	return strings.TrimSpace(sb.String())
}

func (a *Agent) executeMemoryGet(args map[string]any) string {
	if a.markdownMemory == nil || !a.markdownMemory.IsEnabled() {
		return "Error: markdown memory is disabled. Please configure memory.enabled and memory.obsidian_vault in ~/.coco.yaml"
	}

	path, _ := args["path"].(string)
	path = strings.TrimSpace(path)
	if path == "" {
		return "Error: path is required"
	}

	result, err := a.markdownMemory.Get(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Sprintf("Memory file not found: %s", path)
		}
		return fmt.Sprintf("Error reading markdown memory: %v", err)
	}

	content := result.Content
	if len(content) > 12000 {
		content = content[:12000] + "\n\n... (truncated)"
	}

	header := fmt.Sprintf("📄 Memory file: %s\nsource: %s\nupdated: %s\n\n",
		result.Path, result.Source, result.ModifiedAt.Format("2006-01-02 15:04"))
	return header + content
}

func getString(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func (a *Agent) executeWebSearchWithManager(ctx context.Context, query string) string {
	if a.searchManager == nil {
		return "Error: search manager not initialized. Please configure search engines in ~/.coco.yaml or use --metaso-api-key or --tavily-api-key"
	}

	// Check if query starts with "搜索" or "search" to trigger multi-engine search
	queryLower := strings.ToLower(query)
	if strings.HasPrefix(queryLower, "搜索") || strings.HasPrefix(queryLower, "search") {
		// Remove the trigger word
		cleanQuery := query
		if strings.HasPrefix(queryLower, "搜索") {
			cleanQuery = strings.TrimSpace(query[len("搜索"):])
		} else if strings.HasPrefix(queryLower, "search") {
			cleanQuery = strings.TrimSpace(query[len("search"):])
		}

		// Multi-engine search
		combined, err := a.searchManager.SearchAll(ctx, cleanQuery, 5)
		if err != nil {
			return fmt.Sprintf("Error searching: %v", err)
		}
		return search.FormatCombinedResults(combined)
	}

	// Normal single-engine search
	resp, err := a.searchManager.Search(ctx, query, 5)
	if err != nil {
		return fmt.Sprintf("Error searching: %v", err)
	}
	return search.FormatSearchResults(resp)
}

// learnUserPreferences analyzes recent conversations and extracts user preferences
func (a *Agent) learnUserPreferences(ctx context.Context, convKey string, msg router.Message) {
	if a.ragMemory == nil || !a.ragMemory.IsEnabled() {
		return
	}

	history := a.memory.GetHistory(convKey)
	if len(history) < 2 {
		return
	}

	// Build conversation history text
	var conversationText strings.Builder
	conversationText.WriteString("Recent conversation history:\n\n")
	for i, m := range history {
		if m.Role == "user" {
			conversationText.WriteString(fmt.Sprintf("User: %s\n", m.Content))
		} else {
			conversationText.WriteString(fmt.Sprintf("Assistant: %s\n", m.Content))
		}
		if i >= 10 {
			break
		}
	}

	// Create a prompt for the AI to extract preferences
	preferencePrompt := fmt.Sprintf(`Analyze the following conversation history and extract the user's preferences. Focus on:
1. Communication style (formal/informal, concise/detailed)
2. Preferred response format (bullet points, paragraphs, code blocks)
3. Tone preferences (friendly, professional, humorous)
4. Any specific likes or dislikes mentioned
5. Technical vs non-technical preferences

Conversation:
%s

Extract ONLY the preferences, one per line, starting with "- ". Keep it concise and actionable.`, conversationText.String())

	// Use AI to extract preferences
	resp, err := a.chatWithModel(ctx, ChatRequest{
		Messages: []Message{
			{Role: "user", Content: preferencePrompt},
		},
		SystemPrompt: "You are an expert at analyzing conversations and extracting user preferences. Be concise and specific.",
		Tools:        nil,
		MaxTokens:    500,
	})
	if err != nil {
		logger.Warn("[Agent] Failed to extract user preferences: %v", err)
		return
	}

	// Parse and save preferences
	lines := strings.Split(resp.Content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") {
			preference := strings.TrimSpace(line[2:])
			if preference != "" {
				err := a.ragMemory.AddMemory(ctx, MemoryItem{
					ID:      fmt.Sprintf("pref-%s-%d", convKey, time.Now().UnixNano()),
					Type:    "preference",
					Content: preference,
					Metadata: map[string]string{
						"platform":  msg.Platform,
						"channel":   msg.ChannelID,
						"user":      msg.UserID,
						"timestamp": time.Now().Format(time.RFC3339),
					},
				})
				if err != nil {
					logger.Warn("[Agent] Failed to save preference: %v", err)
				} else {
					logger.Debug("[Agent] Saved user preference: %s", preference)
				}
			}
		}
	}
}

func (a *Agent) executeAIListModels() string {
	models := a.modelRouter.ListModels()
	if len(models) == 0 {
		return "No models available"
	}

	var sb strings.Builder
	sb.WriteString("可用模型列表：\n\n")
	for _, m := range models {
		sb.WriteString(fmt.Sprintf("- %s\n", m.Name))
		sb.WriteString(fmt.Sprintf("  - 智力：%s\n", m.IntellectText()))
		sb.WriteString(fmt.Sprintf("  - 速度：%s\n", m.SpeedText()))
		sb.WriteString(fmt.Sprintf("  - 费用：%s\n", m.CostText()))
		sb.WriteString(fmt.Sprintf("  - 能力：%s\n", m.SkillsText()))
		sb.WriteString("\n")
	}
	return sb.String()
}

func (a *Agent) executeAISwitchModel(args map[string]any) string {
	modelName, _ := args["model_name"].(string)
	if modelName == "" {
		return "Error: model_name is required"
	}

	force := false
	if f, ok := args["force"].(bool); ok {
		force = f
	}

	if err := a.modelRouter.SwitchToModel(modelName, force); err != nil {
		return fmt.Sprintf("Error switching model: %v", err)
	}

	return fmt.Sprintf("Successfully switched to model: %s", modelName)
}

func (a *Agent) executeAIGetCurrentModel() string {
	model := a.modelRouter.GetCurrentModel()
	if model == nil {
		return "No current model"
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("当前模型：%s\n", model.Name))
	sb.WriteString(fmt.Sprintf("  - 智力：%s\n", model.IntellectText()))
	sb.WriteString(fmt.Sprintf("  - 速度：%s\n", model.SpeedText()))
	sb.WriteString(fmt.Sprintf("  - 费用：%s\n", model.CostText()))
	sb.WriteString(fmt.Sprintf("  - 能力：%s\n", model.SkillsText()))
	return sb.String()
}
