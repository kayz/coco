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

	"github.com/pltanton/lingti-bot/internal/config"
	cronpkg "github.com/pltanton/lingti-bot/internal/cron"
	"github.com/pltanton/lingti-bot/internal/logger"
	"github.com/pltanton/lingti-bot/internal/persist"
	"github.com/pltanton/lingti-bot/internal/router"
	"github.com/pltanton/lingti-bot/internal/search"
	"github.com/pltanton/lingti-bot/internal/security"
	"github.com/pltanton/lingti-bot/internal/skills"
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
	provider           Provider
	memory             *ConversationMemory
	ragMemory          *RAGMemory
	sessions           *SessionStore
	autoApprove        bool
	customInstructions string
	cronScheduler      *cronpkg.Scheduler
	currentMsg         router.Message // set during HandleMessage for cron_create context
	cronCreatedCount   int            // tracks cron_create calls per HandleMessage turn
	pathChecker        *security.PathChecker
	disableFileTools   bool
	persistStore       *persist.Store
	firstMessageSent   map[string]bool
	firstMessageMu     sync.RWMutex
	latestReport       *persist.DailyReport
	searchRegistry     *search.Registry
	searchManager      *search.Manager
}

// Config holds agent configuration
type Config struct {
	Provider           string // "claude" or "deepseek" (default: "claude")
	APIKey             string
	BaseURL            string // Custom API base URL (optional)
	Model              string // Model name (optional, uses provider default)
	AutoApprove        bool     // Skip all confirmation prompts (default: false)
	CustomInstructions string   // Additional instructions appended to system prompt (optional)
	AllowedPaths       []string // Restrict file/shell operations to these directories (empty = no restriction)
	DisableFileTools   bool     // Completely disable all file operation tools
	Embedding          config.EmbeddingConfig
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

// ConversationKey generates a unique key for a conversation
func ConversationKey(platform, channelID, userID string) string {
	return platform + ":" + channelID + ":" + userID
}

// New creates a new Agent with the specified provider
func New(cfg Config) (*Agent, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("API key is required")
	}

	provider, err := createProvider(cfg)
	if err != nil {
		return nil, err
	}

	exeDir := getExecutableDir()
	if exeDir == "" {
		exeDir = "."
	}

	dbPath := filepath.Join(exeDir, ".lingti.db")
	persistStore, err := persist.NewStore(dbPath)
	if err != nil {
		return nil, err
	}

	memory := NewMemory(persistStore, 200)

	searchRegistry := search.NewRegistry()
	configCfg, err := config.Load()
	if err != nil {
		configCfg = config.DefaultConfig()
	}

	searchManager, err := search.NewManager(configCfg.Search, searchRegistry)
	if err != nil {
		log.Printf("[AGENT] Failed to initialize search manager: %v", err)
	}

	var ragMemory *RAGMemory
	if cfg.Embedding.Enabled {
		ragMemory, err = NewRAGMemory(cfg.Embedding)
		if err != nil {
			log.Printf("[AGENT] Failed to initialize RAG memory: %v", err)
		}
	} else {
		ragMemory = &RAGMemory{enabled: false}
	}

	agent := &Agent{
		provider:           provider,
		memory:             memory,
		ragMemory:          ragMemory,
		sessions:           NewSessionStore(),
		autoApprove:        cfg.AutoApprove,
		customInstructions: cfg.CustomInstructions,
		pathChecker:        security.NewPathChecker(cfg.AllowedPaths),
		disableFileTools:   cfg.DisableFileTools,
		persistStore:       persistStore,
		firstMessageSent:   make(map[string]bool),
		searchRegistry:     searchRegistry,
		searchManager:      searchManager,
	}

	agent.initializeDailyReport()

	return agent, nil
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
		Date:    date,
		UserID:  "default",
		Summary: "系统启动时自动生成的日报",
		Content: fmt.Sprintf("日报自动生成于 %s", time.Now().Format(time.RFC3339)),
		Tasks:   []persist.TaskItem{},
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

// openaiCompatProviders maps provider names to their default base URLs and models.
var openaiCompatProviders = map[string]struct {
	baseURL string
	model   string
}{
	"minimax":    {"https://api.minimax.chat/v1", "MiniMax-Text-01"},
	"doubao":     {"https://ark.cn-beijing.volces.com/api/v3", "doubao-pro-32k"},
	"zhipu":      {"https://open.bigmodel.cn/api/paas/v4", "glm-4-flash"},
	"openai":     {"https://api.openai.com/v1", "gpt-4o"},
	"gemini":     {"https://generativelanguage.googleapis.com/v1beta/openai", "gemini-2.0-flash"},
	"yi":         {"https://api.lingyiwanwu.com/v1", "yi-large"},
	"stepfun":    {"https://api.stepfun.com/v1", "step-2-16k"},
	"siliconflow": {"https://api.siliconflow.cn/v1", "Qwen/Qwen2.5-72B-Instruct"},
	"grok":       {"https://api.x.ai/v1", "grok-2-latest"},
	"baichuan":   {"https://api.baichuan-ai.com/v1", "Baichuan4"},
	"spark":      {"https://spark-api-open.xf-yun.com/v1", "generalv3.5"},
	"hunyuan":    {"https://api.hunyuan.cloud.tencent.com/v1", "hunyuan-turbos-latest"},
}

// openaiCompatAliases maps alternative names to canonical provider names.
var openaiCompatAliases = map[string]string{
	"glm":          "zhipu",
	"chatglm":      "zhipu",
	"gpt":          "openai",
	"chatgpt":      "openai",
	"lingyiwanwu":  "yi",
	"wanwu":        "yi",
	"google":       "gemini",
	"xai":          "grok",
	"bytedance":    "doubao",
	"volcengine":   "doubao",
	"iflytek":      "spark",
	"xunfei":       "spark",
	"tencent":      "hunyuan",
	"hungyuan":     "hunyuan",
}

// createProvider creates the appropriate AI provider based on config
func createProvider(cfg Config) (Provider, error) {
	name := strings.ToLower(cfg.Provider)

	switch name {
	case "deepseek":
		return NewDeepSeekProvider(DeepSeekConfig{
			APIKey:  cfg.APIKey,
			BaseURL: cfg.BaseURL,
			Model:   cfg.Model,
		})
	case "kimi", "moonshot":
		return NewKimiProvider(KimiConfig{
			APIKey:  cfg.APIKey,
			BaseURL: cfg.BaseURL,
			Model:   cfg.Model,
		})
	case "qwen", "qianwen", "tongyi":
		return NewQwenProvider(QwenConfig{
			APIKey:  cfg.APIKey,
			BaseURL: cfg.BaseURL,
			Model:   cfg.Model,
		})
	case "claude", "anthropic", "":
		return NewClaudeProvider(ClaudeConfig{
			APIKey:  cfg.APIKey,
			BaseURL: cfg.BaseURL,
			Model:   cfg.Model,
		})
	default:
		// Check aliases
		if canonical, ok := openaiCompatAliases[name]; ok {
			name = canonical
		}
		// Check OpenAI-compatible providers
		if defaults, ok := openaiCompatProviders[name]; ok {
			return NewOpenAICompatProvider(OpenAICompatConfig{
				ProviderName: name,
				APIKey:       cfg.APIKey,
				BaseURL:      cfg.BaseURL,
				Model:        cfg.Model,
				DefaultURL:   defaults.baseURL,
				DefaultModel: defaults.model,
			})
		}
		return nil, fmt.Errorf("unknown provider: %s (supported: claude, deepseek, kimi, qwen, minimax, doubao, zhipu, openai, gemini, yi, stepfun, siliconflow, grok, baichuan, spark, hunyuan)", cfg.Provider)
	}
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
				settings.ThinkingLevel, settings.Verbose, a.provider.Name()),
		}, true

	case "/model", "模型":
		return router.Response{
			Text: fmt.Sprintf("当前模型: %s", a.provider.Name()),
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

// HandleMessage processes a message and returns a response
func (a *Agent) HandleMessage(ctx context.Context, msg router.Message) (router.Response, error) {
	a.currentMsg = msg
	a.cronCreatedCount = 0
	logger.Info("[Agent] Processing message from %s: %s (provider: %s)", msg.Username, msg.Text, a.provider.Name())

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
	
	// Fallback to default if files not found
	if aboutMe == "" {
		aboutMe = "You are 灵缇 (Lingti), a helpful AI assistant running on the user's computer."
	}
	
	// Retrieve relevant memories from RAG if enabled
	var memoriesSection string
	var preferencesSection string
	if a.ragMemory != nil && a.ragMemory.IsEnabled() {
		memories, err := a.ragMemory.SearchMemories(ctx, msg.Text, 5)
		if err == nil && len(memories) > 0 {
			memoriesSection = "\n\n## Relevant Memories\nHere are some relevant memories from previous conversations that might help you respond:\n"
			for i, mem := range memories {
				memoriesSection += fmt.Sprintf("%d. [%s] %s\n", i+1, mem.Type, mem.Content)
			}
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
			logger.Debug("[Agent] Retrieved %d user preferences", len(preferences))
		}
	}
	
	// System prompt with actual paths
	var systemPrompt string
	if systemContent != "" {
		systemPrompt = fmt.Sprintf(aboutMe+"%s\n\n"+systemContent, 
			autoApprovalNotice, runtime.GOOS, runtime.GOARCH, exeDir, msg.Username, time.Now().Format("2006-01-02"))
	} else {
		systemPrompt = fmt.Sprintf(`You are 灵缇 (Lingti), a helpful AI assistant running on the user's computer.%s

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

	if memoriesSection != "" {
		systemPrompt += memoriesSection
	}

	if preferencesSection != "" {
		systemPrompt += preferencesSection
	}

	if a.customInstructions != "" {
		systemPrompt += "\n\n## Custom Instructions\n" + a.customInstructions
	}

	// Call AI provider
	resp, err := a.provider.Chat(ctx, ChatRequest{
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
		resp, err = a.provider.Chat(ctx, ChatRequest{
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

	// Save conversation to memory
	a.memory.AddExchange(convKey,
		Message{Role: "user", Content: msg.Text},
		Message{Role: "assistant", Content: resp.Content},
	)

	// Save conversation to RAG memory for long-term recall
	if a.ragMemory != nil && a.ragMemory.IsEnabled() {
		conversationText := fmt.Sprintf("User: %s\nAssistant: %s", msg.Text, resp.Content)
		err := a.ragMemory.AddMemory(ctx, MemoryItem{
			ID:        fmt.Sprintf("conv-%s-%d", convKey, time.Now().Unix()),
			Type:      "conversation",
			Content:   conversationText,
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

		// Extract and learn user preferences (every 5th conversation)
		history := a.memory.GetHistory(convKey)
		if len(history) > 0 && len(history)%4 == 0 {
			a.learnUserPreferences(ctx, convKey, msg)
		}
	}

	// Check if this is the first message and add report notification
	if a.isFirstMessage(convKey) {
		if notification := a.getReportNotification(); notification != "" {
			resp.Content = notification + "\n\n" + resp.Content
		}
	}

	// Log response at verbose level
	logger.Debug("[Agent] Response: %s", resp.Content)

	return router.Response{Text: resp.Content, Files: pendingFiles}, nil
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
				"type": "object",
				"properties": map[string]any{},
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
				"type":       "object",
				"properties": map[string]any{
					"query": map[string]string{"type": "string", "description": "Search query string"},
					"limit": map[string]string{"type": "number", "description": "Maximum number of results (default: 5)"},
				},
				"required":   []string{"query"},
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
					"name":      map[string]string{"type": "string", "description": "Human-readable task name"},
					"schedule":  map[string]string{"type": "string", "description": "Cron expression (5-field: minute hour day month weekday). Examples: '0 9 * * *' (daily 9am), '0 9 * * 1-5' (weekdays 9am), '30 8 * * 1' (Monday 8:30am), '0 */2 * * *' (every 2 hours)"},
					"tag":       map[string]string{"type": "string", "description": "Task tag: 'user-schedule' for user's personal schedule/reminders, 'assistant-task' for assistant's background tasks. Use 'user-schedule' when creating calendar/events/reminders for the user."},
					"prompt":    map[string]string{"type": "string", "description": "What the AI should do each time this job triggers. AI runs a full conversation and sends the result to the user. Example: '生成一条独特的编程激励鸡汤，鼓励用户写代码创造新产品'"},
					"tool":      map[string]string{"type": "string", "description": "MCP tool to execute periodically (for raw tool execution without AI)"},
					"arguments": map[string]string{"type": "object", "description": "Arguments for the tool (when using tool parameter)"},
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
	}

	// Block file tools entirely if disabled
	if a.disableFileTools {
		if _, ok := fileToolPaths[name]; ok {
			return "ACCESS DENIED: file operations are disabled by security policy. Do NOT retry. Inform the user that file access is disabled."
		}
	}

	// Enforce allowed_paths restrictions
	if a.pathChecker.HasRestrictions() {
		if err := a.checkToolPathAccess(name, args); err != nil {
			return err.Error()
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
func (a *Agent) checkToolPathAccess(name string, args map[string]any) error {
	if pathKey, ok := fileToolPaths[name]; ok {
		path := "."
		if p, ok := args[pathKey].(string); ok && p != "" {
			path = p
		}
		return a.pathChecker.CheckPath(path)
	}
	if name == "shell_execute" {
		if wd, ok := args["working_directory"].(string); ok && wd != "" {
			return a.pathChecker.CheckPath(wd)
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

func getString(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func (a *Agent) executeWebSearchWithManager(ctx context.Context, query string) string {
	if a.searchManager == nil {
		return "Error: search manager not initialized. Please configure search engines in ~/.lingti.yaml or use --metaso-api-key or --tavily-api-key"
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
	resp, err := a.provider.Chat(ctx, ChatRequest{
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
					ID:        fmt.Sprintf("pref-%s-%d", convKey, time.Now().UnixNano()),
					Type:      "preference",
					Content:   preference,
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
