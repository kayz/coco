package cmd

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
	agentpkg "github.com/kayz/coco/internal/agent"
	"github.com/kayz/coco/internal/config"
	cronpkg "github.com/kayz/coco/internal/cron"
	"github.com/kayz/coco/internal/logger"
	"github.com/kayz/coco/internal/platforms/relay"
	"github.com/kayz/coco/internal/platforms/wecom"
	"github.com/kayz/coco/internal/router"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var (
	keeperPort          int
	keeperServiceAction string
)

var keeperCmd = &cobra.Command{
	Use:   "keeper",
	Short: "Start the Keeper server (public-facing relay for coco)",
	Long: `Start the Keeper server on a public server.

Keeper handles:
  - WeCom (企业微信) webhook callbacks
  - WebSocket endpoint for coco client connections
  - Offline fallback replies when coco is not connected

Usage:
  coco keeper --port 8080`,
	Run: runKeeper,
}

func init() {
	rootCmd.AddCommand(keeperCmd)
	keeperCmd.Flags().IntVar(&keeperPort, "port", 0, "Server port (default: from config or 8080)")
	keeperCmd.Flags().StringVar(&keeperServiceAction, "service", "", serviceActionHelp)
}

// cocoClient represents a connected coco instance.
type cocoClient struct {
	conn      *websocket.Conn
	userID    string
	platform  string
	sessionID string
	mu        sync.Mutex
}

// keeperServer holds all Keeper state.
type keeperServer struct {
	cfg      *config.Config
	msgCrypt *wecom.MsgCrypt
	wecom    *wecom.Platform
	upgrader websocket.Upgrader
	client   *cocoClient // single coco connection (Phase 0)
	clientMu sync.RWMutex
	ctx      context.Context
	cancel   context.CancelFunc

	heartbeatScheduler *cronpkg.Scheduler
	heartbeatExecutor  *keeperPromptExecutor
	fallbackExecutor   *keeperPromptExecutor
}

func newKeeperServer(cfg *config.Config) (*keeperServer, error) {
	kc := cfg.Keeper

	// Validate required WeCom fields
	if kc.WeComCorpID == "" || kc.WeComAgentID == "" || kc.WeComSecret == "" {
		return nil, fmt.Errorf("keeper.wecom_corp_id, wecom_agent_id, and wecom_secret are required")
	}
	if kc.WeComToken == "" || kc.WeComAESKey == "" {
		return nil, fmt.Errorf("keeper.wecom_token and wecom_aes_key are required for WeCom callback")
	}

	// Initialize WeCom message cryptographer for callback verification
	msgCrypt, err := wecom.NewMsgCrypt(kc.WeComToken, kc.WeComAESKey, kc.WeComCorpID)
	if err != nil {
		return nil, fmt.Errorf("failed to create WeCom MsgCrypt: %w", err)
	}

	// Initialize WeCom platform for sending messages (API-only, no HTTP server)
	wp, err := wecom.New(wecom.Config{
		CorpID:         kc.WeComCorpID,
		AgentID:        kc.WeComAgentID,
		Secret:         kc.WeComSecret,
		Token:          kc.WeComToken,
		EncodingAESKey: kc.WeComAESKey,
		CallbackPort:   -1, // API-only mode
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create WeCom platform: %w", err)
	}

	s := &keeperServer{
		cfg:      cfg,
		msgCrypt: msgCrypt,
		wecom:    wp,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}
	return s, nil
}

type keeperPromptExecutor struct {
	provider agentpkg.Provider
}

func newKeeperPromptExecutor(cfg *config.Config) (*keeperPromptExecutor, error) {
	providerName := strings.ToLower(strings.TrimSpace(cfg.Keeper.DefaultProvider))
	apiKey := strings.TrimSpace(cfg.Keeper.DefaultAPIKey)
	baseURL := strings.TrimSpace(cfg.Keeper.DefaultBaseURL)
	model := strings.TrimSpace(cfg.Keeper.DefaultModel)

	if apiKey == "" {
		return nil, fmt.Errorf("keeper.default_api_key is empty")
	}
	if model == "" {
		model = "qwen-turbo"
	}
	if providerName == "" {
		providerName = inferProviderFromBaseURL(baseURL)
	}
	if providerName == "" {
		providerName = "qwen"
	}

	var (
		p   agentpkg.Provider
		err error
	)
	switch providerName {
	case "qwen", "qianwen", "tongyi":
		p, err = agentpkg.NewQwenProvider(agentpkg.QwenConfig{
			APIKey:  apiKey,
			BaseURL: baseURL,
			Model:   model,
		})
	case "deepseek":
		p, err = agentpkg.NewDeepSeekProvider(agentpkg.DeepSeekConfig{
			APIKey:  apiKey,
			BaseURL: baseURL,
			Model:   model,
		})
	case "kimi", "moonshot":
		p, err = agentpkg.NewKimiProvider(agentpkg.KimiConfig{
			APIKey:  apiKey,
			BaseURL: baseURL,
			Model:   model,
		})
	case "claude", "anthropic":
		p, err = agentpkg.NewClaudeProvider(agentpkg.ClaudeConfig{
			APIKey:  apiKey,
			BaseURL: baseURL,
			Model:   model,
		})
	default:
		defaultURL := "https://api.openai.com/v1"
		if baseURL != "" {
			defaultURL = baseURL
		}
		defaultModel := model
		if defaultModel == "" {
			defaultModel = "gpt-4o-mini"
		}
		p, err = agentpkg.NewOpenAICompatProvider(agentpkg.OpenAICompatConfig{
			ProviderName: providerName,
			APIKey:       apiKey,
			BaseURL:      baseURL,
			Model:        model,
			DefaultURL:   defaultURL,
			DefaultModel: defaultModel,
		})
	}
	if err != nil {
		return nil, err
	}

	return &keeperPromptExecutor{provider: p}, nil
}

func (s *keeperServer) initFallbackExecutor() {
	executor, err := newKeeperPromptExecutor(s.cfg)
	if err != nil {
		logger.Warn("[KeeperFallback] Disabled LLM fallback: %v", err)
		return
	}
	s.fallbackExecutor = executor
	logger.Info("[KeeperFallback] LLM fallback enabled (provider=%s model=%s)",
		strings.TrimSpace(s.cfg.Keeper.DefaultProvider),
		strings.TrimSpace(s.cfg.Keeper.DefaultModel),
	)
}

func inferProviderFromBaseURL(baseURL string) string {
	baseURL = strings.ToLower(strings.TrimSpace(baseURL))
	switch {
	case strings.Contains(baseURL, "dashscope"):
		return "qwen"
	case strings.Contains(baseURL, "deepseek"):
		return "deepseek"
	case strings.Contains(baseURL, "moonshot"):
		return "kimi"
	case strings.Contains(baseURL, "anthropic"):
		return "claude"
	default:
		return ""
	}
}

func (e *keeperPromptExecutor) ExecutePrompt(ctx context.Context, platform, channelID, userID, prompt string) (string, error) {
	if e == nil || e.provider == nil {
		return "", fmt.Errorf("keeper prompt executor not available")
	}

	req := agentpkg.ChatRequest{
		Messages: []agentpkg.Message{
			{Role: "user", Content: strings.TrimSpace(prompt)},
		},
		SystemPrompt: `你是运行在 keeper 上的轻量调度助手。
目标：
- 以低成本完成巡检/提醒类任务
- 输出简短、可执行、直接可发送给用户
- 不使用工具，不编造外部事实
`,
		MaxTokens: 1200,
	}

	resp, err := e.provider.Chat(ctx, req)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(resp.Content), nil
}

type keeperHeartbeatSpec struct {
	Enabled  bool                 `yaml:"enabled"`
	Interval string               `yaml:"interval"`
	Tasks    []keeperHeartbeatJob `yaml:"tasks"`
	Checks   []keeperHeartbeatJob `yaml:"checks"`
}

type keeperHeartbeatJob struct {
	Name     string `yaml:"name"`
	Schedule string `yaml:"schedule"`
	Prompt   string `yaml:"prompt"`
	Notify   string `yaml:"notify"`
}

type keeperCronNotifier struct {
	server *keeperServer
}

func (n *keeperCronNotifier) NotifyChat(message string) error {
	logger.Info("[KeeperCron] %s", strings.TrimSpace(message))
	return nil
}

func (n *keeperCronNotifier) NotifyChatUser(platform, channelID, userID, message string) error {
	if n == nil || n.server == nil {
		return fmt.Errorf("keeper notifier unavailable")
	}
	if !strings.EqualFold(strings.TrimSpace(platform), "wecom") {
		logger.Warn("[KeeperCron] unsupported platform %s, skip notify", platform)
		return nil
	}
	return n.server.sendWeComReply(userID, message)
}

func (s *keeperServer) initHeartbeatScheduler() {
	workspaceDir := keeperWorkspaceDir()
	dbPath := filepath.Join(workspaceDir, ".coco-keeper.db")
	store, err := cronpkg.NewStore(dbPath)
	if err != nil {
		logger.Warn("[KeeperCron] Failed to open cron store: %v", err)
		return
	}

	executor, err := newKeeperPromptExecutor(s.cfg)
	if err != nil {
		logger.Warn("[KeeperCron] Prompt jobs will be limited: %v", err)
	} else {
		s.heartbeatExecutor = executor
		if s.fallbackExecutor == nil {
			s.fallbackExecutor = executor
		}
	}
	s.heartbeatScheduler = cronpkg.NewScheduler(
		store,
		nil,
		executor,
		&keeperCronNotifier{server: s},
	)
	if err := s.heartbeatScheduler.Start(); err != nil {
		logger.Warn("[KeeperCron] Failed to start scheduler: %v", err)
		s.heartbeatScheduler = nil
		return
	}
	logger.Info("[KeeperCron] Scheduler started (store: %s)", dbPath)
}

func (s *keeperServer) stopHeartbeatScheduler() {
	if s == nil || s.heartbeatScheduler == nil {
		return
	}
	if err := s.heartbeatScheduler.Stop(); err != nil {
		logger.Warn("[KeeperCron] stop scheduler failed: %v", err)
	}
}

func (s *keeperServer) ensureHeartbeatJobsForUser(userID string) {
	if s == nil || s.heartbeatScheduler == nil || s.heartbeatExecutor == nil {
		return
	}
	spec, err := loadKeeperHeartbeatSpec()
	if err != nil {
		logger.Warn("[KeeperCron] Failed to load HEARTBEAT.md: %v", err)
		return
	}
	if spec == nil || !spec.Enabled || len(spec.Tasks) == 0 {
		return
	}

	existing := s.heartbeatScheduler.ListJobsByTag("heartbeat")
	for idx, task := range spec.Tasks {
		name := strings.TrimSpace(task.Name)
		if name == "" {
			name = fmt.Sprintf("task-%d", idx+1)
		}
		schedule := strings.TrimSpace(task.Schedule)
		prompt := strings.TrimSpace(task.Prompt)
		if schedule == "" || prompt == "" {
			continue
		}

		jobName := keeperHeartbeatJobName(userID, name)
		if keeperHeartbeatJobExists(existing, jobName, "wecom", userID, userID) {
			continue
		}

		_, err := s.heartbeatScheduler.AddJobWithPromptAndTag(
			jobName,
			"heartbeat",
			schedule,
			decorateKeeperHeartbeatPrompt(prompt, task.Notify),
			"wecom",
			userID,
			userID,
		)
		if err != nil {
			logger.Warn("[KeeperCron] Failed to create heartbeat job %s: %v", jobName, err)
			continue
		}
		logger.Info("[KeeperCron] Heartbeat job created for %s: %s (%s)", userID, jobName, schedule)
	}
}

func (s *keeperServer) buildOfflineReply(userID, text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return "coco 暂时不在线，请稍后再试。"
	}
	if s == nil || s.fallbackExecutor == nil {
		return "coco 暂时不在线，请稍后再试。"
	}

	ctx, cancel := context.WithTimeout(s.ctx, 8*time.Second)
	defer cancel()

	prompt := fmt.Sprintf(`用户通过企业微信发来消息，但 coco 当前不在线。
你是 keeper，职责是做一个“简单、克制、低成本”的代答：
- 明确说明 coco 当前不在线
- 如果用户消息里有明显可立即回答的轻量问题，可给一句简短帮助
- 不使用工具，不假装自己是 coco，不展开长对话
- 输出 2 到 4 行，简洁直接

用户ID: %s
用户消息:
%s`, userID, text)

	reply, err := s.fallbackExecutor.ExecutePrompt(ctx, "wecom", userID, userID, prompt)
	if err != nil {
		logger.Warn("[KeeperFallback] LLM fallback failed: %v", err)
		return "coco 暂时不在线，请稍后再试。"
	}
	reply = strings.TrimSpace(reply)
	if reply == "" {
		return "coco 暂时不在线，请稍后再试。"
	}
	return reply
}

func keeperHeartbeatJobExists(jobs []*cronpkg.Job, name, platform, channelID, userID string) bool {
	for _, j := range jobs {
		if j.Name == name && j.Platform == platform && j.ChannelID == channelID && j.UserID == userID {
			return true
		}
	}
	return false
}

func keeperHeartbeatJobName(userID, taskName string) string {
	return "keeper-heartbeat:" + sanitizeHeartbeatTokenKeeper(userID) + ":" + sanitizeHeartbeatTokenKeeper(taskName)
}

func sanitizeHeartbeatTokenKeeper(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return "x"
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "x"
	}
	return out
}

func decorateKeeperHeartbeatPrompt(prompt, notify string) string {
	notify = normalizeKeeperHeartbeatNotify(notify)
	return fmt.Sprintf("[HEARTBEAT_NOTIFY=%s]\n%s", notify, strings.TrimSpace(prompt))
}

func normalizeKeeperHeartbeatNotify(notify string) string {
	switch strings.ToLower(strings.TrimSpace(notify)) {
	case "always", "on_change", "auto", "never":
		return strings.ToLower(strings.TrimSpace(notify))
	default:
		return "never"
	}
}

func loadKeeperHeartbeatSpec() (*keeperHeartbeatSpec, error) {
	path := filepath.Join(keeperWorkspaceDir(), "HEARTBEAT.md")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	fm, _ := splitKeeperMarkdownFrontMatter(string(data))
	if strings.TrimSpace(fm) == "" {
		return nil, nil
	}

	spec := &keeperHeartbeatSpec{Enabled: true}
	if err := yaml.Unmarshal([]byte(fm), spec); err != nil {
		return nil, err
	}
	if !spec.Enabled {
		return spec, nil
	}
	if len(spec.Tasks) == 0 && len(spec.Checks) > 0 {
		spec.Tasks = spec.Checks
	}

	filtered := make([]keeperHeartbeatJob, 0, len(spec.Tasks))
	for _, task := range spec.Tasks {
		task.Schedule = normalizeKeeperHeartbeatSchedule(strings.TrimSpace(task.Schedule), strings.TrimSpace(spec.Interval))
		if strings.TrimSpace(task.Schedule) == "" || strings.TrimSpace(task.Prompt) == "" {
			continue
		}
		filtered = append(filtered, task)
	}
	spec.Tasks = filtered
	return spec, nil
}

func normalizeKeeperHeartbeatSchedule(taskSchedule, interval string) string {
	if taskSchedule != "" {
		return taskSchedule
	}
	interval = strings.TrimSpace(interval)
	if interval == "" {
		return ""
	}
	if strings.HasPrefix(interval, "@every ") || strings.HasPrefix(interval, "@daily") || strings.HasPrefix(interval, "@hourly") {
		return interval
	}
	return "@every " + interval
}

func splitKeeperMarkdownFrontMatter(content string) (frontMatter string, body string) {
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	normalized = strings.TrimSpace(normalized)
	if !strings.HasPrefix(normalized, "---\n") {
		return "", strings.TrimSpace(content)
	}
	rest := normalized[len("---\n"):]
	idx := strings.Index(rest, "\n---\n")
	if idx < 0 {
		return "", strings.TrimSpace(content)
	}
	return strings.TrimSpace(rest[:idx]), strings.TrimSpace(rest[idx+len("\n---\n"):])
}

// ---------- HTTP handlers ----------

func (s *keeperServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	s.clientMu.RLock()
	cocoOnline := s.client != nil
	s.clientMu.RUnlock()

	status := "offline"
	if cocoOnline {
		status = "online"
	}
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"status":"ok","coco":"%s"}`, status)
}

// handleWeComCallback handles GET (URL verification) and POST (message callback).
func (s *keeperServer) handleWeComCallback(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	msgSignature := query.Get("msg_signature")
	timestamp := query.Get("timestamp")
	nonce := query.Get("nonce")

	switch r.Method {
	case http.MethodGet:
		// URL verification
		echostr := query.Get("echostr")
		plaintext, err := s.msgCrypt.VerifyURL(msgSignature, timestamp, nonce, echostr)
		if err != nil {
			logger.Error("[Keeper] WeCom URL verification failed: %v", err)
			http.Error(w, "verification failed", http.StatusForbidden)
			return
		}
		logger.Info("[Keeper] WeCom URL verification passed")
		w.Write([]byte(plaintext))

	case http.MethodPost:
		body, err := io.ReadAll(r.Body)
		if err != nil {
			logger.Error("[Keeper] Failed to read body: %v", err)
			http.Error(w, "read failed", http.StatusBadRequest)
			return
		}

		var encryptedMsg wecom.EncryptedMsg
		if err := xml.Unmarshal(body, &encryptedMsg); err != nil {
			logger.Error("[Keeper] Failed to parse XML: %v", err)
			http.Error(w, "parse failed", http.StatusBadRequest)
			return
		}

		plaintext, err := s.msgCrypt.DecryptMsg(msgSignature, timestamp, nonce, &encryptedMsg)
		if err != nil {
			logger.Error("[Keeper] Failed to decrypt message: %v", err)
			http.Error(w, "decrypt failed", http.StatusBadRequest)
			return
		}

		// Return 200 immediately (WeCom requires fast response)
		w.WriteHeader(http.StatusOK)

		// Process asynchronously
		go s.processWeComMessage(plaintext)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// processWeComMessage decrypts and routes a WeCom message.
func (s *keeperServer) processWeComMessage(plaintext []byte) {
	var msg wecom.ReceivedMsg
	if err := xml.Unmarshal(plaintext, &msg); err != nil {
		logger.Error("[Keeper] Failed to parse message: %v", err)
		return
	}

	// Skip event messages for now
	if msg.MsgType == "event" {
		logger.Trace("[Keeper] Ignoring event: %s", msg.Event)
		return
	}

	// Only handle text messages in Phase 0
	if msg.MsgType != "text" {
		logger.Info("[Keeper] Ignoring non-text message type: %s", msg.MsgType)
		return
	}

	userID := msg.FromUserName
	text := msg.Content
	logger.Info("[Keeper] WeCom message from %s: %s", userID, text)
	s.ensureHeartbeatJobsForUser(userID)

	// Try to forward to coco
	s.clientMu.RLock()
	client := s.client
	s.clientMu.RUnlock()

	if client == nil {
		// coco offline — send fallback reply
		logger.Info("[Keeper] coco offline, sending fallback reply to %s", userID)
		reply := s.buildOfflineReply(userID, text)
		if err := s.sendWeComReply(userID, reply); err != nil {
			logger.Error("[Keeper] Failed to send fallback reply: %v", err)
		}
		return
	}

	// Forward to coco via WebSocket
	incoming := relay.IncomingMessage{
		Type:      "message",
		ID:        msg.MsgId,
		Platform:  "wecom",
		ChannelID: userID,
		UserID:    userID,
		Username:  userID,
		Text:      text,
		Metadata: map[string]string{
			"agent_id": msg.AgentID,
			"msg_type": msg.MsgType,
		},
	}

	client.mu.Lock()
	err := client.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if err == nil {
		err = client.conn.WriteJSON(incoming)
	}
	client.mu.Unlock()

	if err != nil {
		logger.Error("[Keeper] Failed to forward message to coco: %v", err)
		// coco connection broken, send fallback
		reply := s.buildOfflineReply(userID, text)
		if err := s.sendWeComReply(userID, reply); err != nil {
			logger.Error("[Keeper] Failed to send fallback reply: %v", err)
		}
		return
	}

	logger.Info("[Keeper] Forwarded message to coco: %s -> %s", userID, text)
}

// sendWeComReply sends a text message to a WeCom user via the WeCom API.
func (s *keeperServer) sendWeComReply(userID, text string) error {
	ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
	defer cancel()
	return s.wecom.Send(ctx, userID, router.Response{Text: text})
}

// ---------- WebSocket handler (coco connects here) ----------

func (s *keeperServer) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.Error("[Keeper] WebSocket upgrade failed: %v", err)
		return
	}

	logger.Info("[Keeper] New WebSocket connection from %s", r.RemoteAddr)

	// Read auth message
	conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	var authMsg relay.AuthMessage
	if err := conn.ReadJSON(&authMsg); err != nil {
		logger.Error("[Keeper] Failed to read auth message: %v", err)
		conn.Close()
		return
	}

	if authMsg.Type != "auth" {
		logger.Error("[Keeper] Expected auth message, got: %s", authMsg.Type)
		conn.WriteJSON(relay.AuthResult{Type: "auth_result", Success: false, Error: "expected auth message"})
		conn.Close()
		return
	}

	// Validate token if configured
	if token := s.cfg.Keeper.Token; token != "" {
		if authMsg.Token != token {
			logger.Warn("[Keeper] Auth rejected: invalid token from %s", r.RemoteAddr)
			conn.WriteJSON(relay.AuthResult{Type: "auth_result", Success: false, Error: "invalid token"})
			conn.Close()
			return
		}
	}

	sessionID := fmt.Sprintf("keeper-%s-%d", authMsg.UserID, time.Now().UnixMilli())

	// Send auth result
	conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if err := conn.WriteJSON(relay.AuthResult{
		Type:      "auth_result",
		Success:   true,
		SessionID: sessionID,
	}); err != nil {
		logger.Error("[Keeper] Failed to send auth result: %v", err)
		conn.Close()
		return
	}

	client := &cocoClient{
		conn:      conn,
		userID:    authMsg.UserID,
		platform:  authMsg.Platform,
		sessionID: sessionID,
	}

	// Register client (replace existing if any)
	s.clientMu.Lock()
	old := s.client
	s.client = client
	s.clientMu.Unlock()

	if old != nil {
		logger.Info("[Keeper] Replacing previous coco connection")
		old.conn.Close()
	}

	logger.Info("[Keeper] coco connected: user=%s, platform=%s, session=%s", authMsg.UserID, authMsg.Platform, sessionID)

	// Read loop — handle responses from coco
	s.cocoReadLoop(client)
}

// cocoReadLoop reads messages from a connected coco client.
func (s *keeperServer) cocoReadLoop(client *cocoClient) {
	defer func() {
		// Unregister client
		s.clientMu.Lock()
		if s.client == client {
			s.client = nil
		}
		s.clientMu.Unlock()
		client.conn.Close()
		logger.Info("[Keeper] coco disconnected: user=%s, session=%s", client.userID, client.sessionID)
	}()

	// Set up ping/pong handlers
	client.conn.SetPongHandler(func(appData string) error {
		client.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	// Start ping ticker to keep connection alive
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-s.ctx.Done():
				return
			case <-ticker.C:
				client.mu.Lock()
				client.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
				err := client.conn.WriteMessage(websocket.PingMessage, nil)
				client.mu.Unlock()
				if err != nil {
					return
				}
			}
		}
	}()

	for {
		client.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		_, message, err := client.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				logger.Error("[Keeper] coco read error: %v", err)
			}
			return
		}

		// Parse message type
		var jsonMsg struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(message, &jsonMsg); err != nil {
			logger.Error("[Keeper] Failed to parse coco message: %v", err)
			continue
		}

		switch jsonMsg.Type {
		case "response":
			s.handleCocoResponse(message)
		case "ping":
			client.mu.Lock()
			client.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			client.conn.WriteJSON(relay.PingPong{Type: "pong"})
			client.mu.Unlock()
		case "pong":
			// ignore
		default:
			logger.Trace("[Keeper] Unknown message type from coco: %s", jsonMsg.Type)
		}
	}
}

// handleCocoResponse processes a response from coco and sends it to WeCom.
// Called both from the WebSocket read loop and the /webhook HTTP handler.
func (s *keeperServer) handleCocoResponse(data []byte) {
	var resp relay.OutgoingResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		logger.Error("[Keeper] Failed to parse coco response: %v", err)
		return
	}

	if resp.Text == "" {
		return
	}

	logger.Info("[Keeper] Sending coco reply to WeCom user %s: %s", resp.ChannelID, truncate(resp.Text, 80))

	if err := s.sendWeComReply(resp.ChannelID, resp.Text); err != nil {
		logger.Error("[Keeper] Failed to send WeCom reply: %v", err)
	}
}

// handleWebhook receives response POSTs from the coco relay client.
// coco sends replies via HTTP POST /webhook (not via WebSocket).
func (s *keeperServer) handleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Validate session: the request must carry a session ID matching a connected coco client.
	sessionID := r.Header.Get("X-Session-ID")
	if sessionID == "" {
		http.Error(w, "missing session", http.StatusUnauthorized)
		return
	}

	s.clientMu.RLock()
	client := s.client
	s.clientMu.RUnlock()

	if client == nil || client.sessionID != sessionID {
		logger.Warn("[Keeper] Webhook from unknown session %s (coco not connected or session mismatch)", sessionID)
		http.Error(w, "unknown session", http.StatusUnauthorized)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read failed", http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)

	go s.handleCocoResponse(body)
}

// handleHeartbeatUpload receives HEARTBEAT.md content from onboard bootstrap.
func (s *keeperServer) handleHeartbeatUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if token := strings.TrimSpace(s.cfg.Keeper.Token); token != "" {
		authToken := ""
		if h := strings.TrimSpace(r.Header.Get("Authorization")); strings.HasPrefix(strings.ToLower(h), "bearer ") {
			authToken = strings.TrimSpace(h[len("bearer "):])
		}
		if authToken == "" {
			authToken = strings.TrimSpace(r.Header.Get("X-Keeper-Token"))
		}
		if authToken == "" {
			authToken = strings.TrimSpace(r.URL.Query().Get("token"))
		}
		if authToken != token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read failed", http.StatusBadRequest)
		return
	}

	var payload struct {
		Filename string `json:"filename"`
		Content  string `json:"content"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		http.Error(w, "invalid json payload", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(payload.Content) == "" {
		http.Error(w, "content is required", http.StatusBadRequest)
		return
	}

	filename := strings.TrimSpace(payload.Filename)
	if filename == "" {
		filename = "HEARTBEAT.md"
	}
	filename = filepath.Base(filename)
	if !strings.EqualFold(filepath.Ext(filename), ".md") {
		http.Error(w, "filename must be a .md file", http.StatusBadRequest)
		return
	}

	workspaceDir := keeperWorkspaceDir()
	if err := os.MkdirAll(workspaceDir, 0755); err != nil {
		http.Error(w, "failed to create workspace dir", http.StatusInternalServerError)
		return
	}
	target := filepath.Join(workspaceDir, filename)
	if err := os.WriteFile(target, []byte(payload.Content), 0644); err != nil {
		http.Error(w, "failed to save heartbeat", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":   true,
		"path": target,
	})
}

func (s *keeperServer) requireKeeperAPIAuth(w http.ResponseWriter, r *http.Request) bool {
	token := strings.TrimSpace(s.cfg.Keeper.Token)
	if token == "" {
		return true
	}

	authToken := ""
	if h := strings.TrimSpace(r.Header.Get("Authorization")); strings.HasPrefix(strings.ToLower(h), "bearer ") {
		authToken = strings.TrimSpace(h[len("bearer "):])
	}
	if authToken == "" {
		authToken = strings.TrimSpace(r.Header.Get("X-Keeper-Token"))
	}
	if authToken == "" {
		authToken = strings.TrimSpace(r.URL.Query().Get("token"))
	}
	if authToken != token {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return false
	}
	return true
}

type keeperCronCreateRequest struct {
	Name      string         `json:"name"`
	Tag       string         `json:"tag,omitempty"`
	Type      string         `json:"type,omitempty"`
	Schedule  string         `json:"schedule"`
	Message   string         `json:"message,omitempty"`
	Prompt    string         `json:"prompt,omitempty"`
	Tool      string         `json:"tool,omitempty"`
	Arguments map[string]any `json:"arguments,omitempty"`
	Endpoint  string         `json:"endpoint,omitempty"`
	Auth      string         `json:"auth,omitempty"`
	RelayMode bool           `json:"relay_mode,omitempty"`
	Platform  string         `json:"platform"`
	ChannelID string         `json:"channel_id"`
	UserID    string         `json:"user_id"`
}

type keeperCronIDRequest struct {
	ID string `json:"id"`
}

func (s *keeperServer) handleCronCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.requireKeeperAPIAuth(w, r) {
		return
	}
	if s.heartbeatScheduler == nil {
		http.Error(w, "scheduler unavailable", http.StatusServiceUnavailable)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read failed", http.StatusBadRequest)
		return
	}
	var req keeperCronCreateRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "invalid json payload", http.StatusBadRequest)
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	req.Schedule = strings.TrimSpace(req.Schedule)
	req.Type = strings.ToLower(strings.TrimSpace(req.Type))
	req.Platform = strings.TrimSpace(req.Platform)
	req.ChannelID = strings.TrimSpace(req.ChannelID)
	req.UserID = strings.TrimSpace(req.UserID)
	if req.Name == "" || req.Schedule == "" || req.Platform == "" || req.ChannelID == "" || req.UserID == "" {
		http.Error(w, "name/schedule/platform/channel_id/user_id are required", http.StatusBadRequest)
		return
	}

	var job *cronpkg.Job
	switch {
	case req.Type == "external" || strings.TrimSpace(req.Endpoint) != "":
		if strings.TrimSpace(req.Endpoint) == "" {
			http.Error(w, "endpoint is required for external job", http.StatusBadRequest)
			return
		}
		job, err = s.heartbeatScheduler.AddExternalJob(
			req.Name, req.Tag, req.Schedule, strings.TrimSpace(req.Endpoint), strings.TrimSpace(req.Auth),
			req.RelayMode, req.Arguments, req.Platform, req.ChannelID, req.UserID,
		)
	case strings.TrimSpace(req.Prompt) != "":
		job, err = s.heartbeatScheduler.AddJobWithPromptAndTag(
			req.Name, req.Tag, req.Schedule, req.Prompt, req.Platform, req.ChannelID, req.UserID,
		)
	case strings.TrimSpace(req.Message) != "":
		job, err = s.heartbeatScheduler.AddJobWithMessageAndTag(
			req.Name, req.Tag, req.Schedule, req.Message, req.Platform, req.ChannelID, req.UserID,
		)
	case strings.TrimSpace(req.Tool) != "":
		job, err = s.heartbeatScheduler.AddJobWithTag(req.Name, req.Tag, req.Schedule, req.Tool, req.Arguments)
	default:
		http.Error(w, "one of prompt/message/tool/endpoint is required", http.StatusBadRequest)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":  true,
		"job": job,
	})
}

func (s *keeperServer) handleCronList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.requireKeeperAPIAuth(w, r) {
		return
	}
	if s.heartbeatScheduler == nil {
		http.Error(w, "scheduler unavailable", http.StatusServiceUnavailable)
		return
	}

	tag := strings.TrimSpace(r.URL.Query().Get("tag"))
	platform := strings.TrimSpace(r.URL.Query().Get("platform"))
	channelID := strings.TrimSpace(r.URL.Query().Get("channel_id"))
	userID := strings.TrimSpace(r.URL.Query().Get("user_id"))

	var jobs []*cronpkg.Job
	if tag != "" {
		jobs = s.heartbeatScheduler.ListJobsByTag(tag)
	} else {
		jobs = s.heartbeatScheduler.ListJobs()
	}

	filtered := make([]*cronpkg.Job, 0, len(jobs))
	for _, job := range jobs {
		if platform != "" && !strings.EqualFold(job.Platform, platform) {
			continue
		}
		if channelID != "" && job.ChannelID != channelID {
			continue
		}
		if userID != "" && job.UserID != userID {
			continue
		}
		filtered = append(filtered, job)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":   true,
		"jobs": filtered,
	})
}

func (s *keeperServer) decodeCronIDRequest(r *http.Request) (string, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return "", err
	}
	var req keeperCronIDRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return "", err
	}
	req.ID = strings.TrimSpace(req.ID)
	if req.ID == "" {
		return "", fmt.Errorf("id is required")
	}
	return req.ID, nil
}

func (s *keeperServer) handleCronDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.requireKeeperAPIAuth(w, r) {
		return
	}
	if s.heartbeatScheduler == nil {
		http.Error(w, "scheduler unavailable", http.StatusServiceUnavailable)
		return
	}
	id, err := s.decodeCronIDRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.heartbeatScheduler.RemoveJob(id); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

func (s *keeperServer) handleCronPause(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.requireKeeperAPIAuth(w, r) {
		return
	}
	if s.heartbeatScheduler == nil {
		http.Error(w, "scheduler unavailable", http.StatusServiceUnavailable)
		return
	}
	id, err := s.decodeCronIDRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.heartbeatScheduler.PauseJob(id); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

func (s *keeperServer) handleCronResume(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.requireKeeperAPIAuth(w, r) {
		return
	}
	if s.heartbeatScheduler == nil {
		http.Error(w, "scheduler unavailable", http.StatusServiceUnavailable)
		return
	}
	id, err := s.decodeCronIDRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.heartbeatScheduler.ResumeJob(id); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

func keeperWorkspaceDir() string {
	if env := strings.TrimSpace(os.Getenv("COCO_WORKSPACE_DIR")); env != "" {
		return env
	}
	execPath, err := os.Executable()
	if err != nil {
		if wd, wdErr := os.Getwd(); wdErr == nil {
			return wd
		}
		return "."
	}
	return filepath.Dir(execPath)
}

// ---------- Main entry ----------

func runKeeper(cmd *cobra.Command, args []string) {
	if runModeServiceAction("keeper", keeperServiceAction) {
		return
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	// Port priority: flag > config > default
	port := keeperPort
	if port == 0 {
		port = cfg.Keeper.Port
	}
	if port == 0 {
		port = 8080
	}

	srv, err := newKeeperServer(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize Keeper: %v\n", err)
		os.Exit(1)
	}

	srv.ctx, srv.cancel = context.WithCancel(context.Background())
	defer srv.cancel()

	// Start WeCom token refresh
	if err := srv.wecom.Start(srv.ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to start WeCom platform: %v\n", err)
		os.Exit(1)
	}

	srv.initFallbackExecutor()
	srv.initHeartbeatScheduler()

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", srv.handleWebSocket)
	mux.HandleFunc("/wecom", srv.handleWeComCallback)
	mux.HandleFunc("/webhook", srv.handleWebhook)
	mux.HandleFunc("/health", srv.handleHealth)
	mux.HandleFunc("/api/heartbeat/upload", srv.handleHeartbeatUpload)
	mux.HandleFunc("/api/cron/create", srv.handleCronCreate)
	mux.HandleFunc("/api/cron/list", srv.handleCronList)
	mux.HandleFunc("/api/cron/delete", srv.handleCronDelete)
	mux.HandleFunc("/api/cron/pause", srv.handleCronPause)
	mux.HandleFunc("/api/cron/resume", srv.handleCronResume)

	addr := fmt.Sprintf(":%d", port)
	httpServer := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	go func() {
		logger.Info("[Keeper] Listening on %s", addr)
		logger.Info("[Keeper] WebSocket:      ws://0.0.0.0%s/ws", addr)
		logger.Info("[Keeper] WeCom callback: http://0.0.0.0%s/wecom", addr)
		logger.Info("[Keeper] Webhook:        http://0.0.0.0%s/webhook", addr)
		logger.Info("[Keeper] Health check:   http://0.0.0.0%s/health", addr)
		logger.Info("[Keeper] Bootstrap API:  http://0.0.0.0%s/api/heartbeat/upload", addr)
		logger.Info("[Keeper] Cron API:       http://0.0.0.0%s/api/cron/*", addr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("[Keeper] Server error: %v", err)
			os.Exit(1)
		}
	}()

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	logger.Info("[Keeper] Shutting down...")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	srv.wecom.Stop()
	srv.stopHeartbeatScheduler()
	httpServer.Shutdown(shutdownCtx)
	logger.Info("[Keeper] Stopped")
}

// truncate shortens a string for log display.
func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}
