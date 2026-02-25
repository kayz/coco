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
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
	"github.com/kayz/coco/internal/config"
	"github.com/kayz/coco/internal/logger"
	"github.com/kayz/coco/internal/platforms/relay"
	"github.com/kayz/coco/internal/platforms/wecom"
	"github.com/kayz/coco/internal/router"
	"github.com/spf13/cobra"
)

var keeperPort int

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
	cfg       *config.Config
	msgCrypt  *wecom.MsgCrypt
	wecom     *wecom.Platform
	upgrader  websocket.Upgrader
	client    *cocoClient // single coco connection (Phase 0)
	clientMu  sync.RWMutex
	ctx       context.Context
	cancel    context.CancelFunc
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

	// Try to forward to coco
	s.clientMu.RLock()
	client := s.client
	s.clientMu.RUnlock()

	if client == nil {
		// coco offline — send fallback reply
		logger.Info("[Keeper] coco offline, sending fallback reply to %s", userID)
		if err := s.sendWeComReply(userID, "coco 暂时不在线，请稍后再试。"); err != nil {
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
		if err := s.sendWeComReply(userID, "coco 暂时不在线，请稍后再试。"); err != nil {
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

// ---------- Main entry ----------

func runKeeper(cmd *cobra.Command, args []string) {
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

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", srv.handleWebSocket)
	mux.HandleFunc("/wecom", srv.handleWeComCallback)
	mux.HandleFunc("/webhook", srv.handleWebhook)
	mux.HandleFunc("/health", srv.handleHealth)

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
