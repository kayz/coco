package relay

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/kayz/coco/internal/debug"
	"github.com/kayz/coco/internal/platforms/wechat"
	"github.com/kayz/coco/internal/platforms/wecom"
	"github.com/kayz/coco/internal/router"
	"github.com/kayz/coco/internal/voice"
)

const (
	DefaultServerURL  = "wss://keeper.kayz.com/ws"
	DefaultWebhookURL = "https://keeper.kayz.com/webhook"
	ClientVersion     = "1.7.0"

	writeTimeout      = 10 * time.Second
	readTimeout       = 60 * time.Second
	initialRetryDelay = 5 * time.Second
	maxRetryDelay     = 5 * time.Minute
)

// Config holds relay configuration
type Config struct {
	UserID     string // From /whoami
	Platform   string // "feishu", "slack", or "wechat"
	Token      string // Auth token for Keeper connection
	ServerURL  string // WebSocket URL (default: wss://keeper.kayz.com/ws)
	WebhookURL string // Webhook URL (default: https://keeper.kayz.com/webhook)
	AIProvider string // AI provider name (e.g., "claude", "deepseek")
	AIModel    string // AI model name
	// WeCom credentials for cloud relay (when platform=wecom)
	WeComCorpID  string
	WeComAgentID string
	WeComSecret  string
	WeComToken   string
	WeComAESKey  string
	// WeChat Official Account credentials (when platform=wechat)
	WeChatAppID     string
	WeChatAppSecret string
	// Optional voice transcriber for voice messages
	Transcriber *voice.Transcriber
	// Proxy media through relay server (instead of direct API calls)
	UseMediaProxy bool
}

// Platform implements router.Platform for cloud relay
type Platform struct {
	config         Config
	conn           *websocket.Conn
	connMu         sync.Mutex
	sessionID      string
	messageHandler func(msg router.Message)
	httpClient     *http.Client
	ctx            context.Context
	cancel         context.CancelFunc
	wg             sync.WaitGroup
	// WeCom message cryptographer for local decryption
	msgCrypt *wecom.MsgCrypt
	// WeCom platform for direct API calls (media upload/send)
	wecomPlatform *wecom.Platform
	// WeChat OA client for media upload/send (when platform=wechat)
	wechatClient *wechat.Client
	// KF sync cursor per open_kfid
	kfCursors   map[string]string
	kfCursorsMu sync.Mutex
	kfEnabled   bool
	// Voice transcriber
	transcriber *voice.Transcriber
	// Proxy media through relay server (instead of direct API calls)
	useMediaProxy bool
}

// Protocol message types

// AuthMessage is sent on WebSocket connect
type AuthMessage struct {
	Type          string `json:"type"`
	UserID        string `json:"user_id"`
	Platform      string `json:"platform"`
	Token         string `json:"token,omitempty"`
	ClientVersion string `json:"client_version"`
	AIProvider    string `json:"ai_provider,omitempty"`
	AIModel       string `json:"ai_model,omitempty"`
	// WeCom credentials (for wecom platform)
	WeComCorpID  string `json:"wecom_corp_id,omitempty"`
	WeComAgentID string `json:"wecom_agent_id,omitempty"`
	WeComSecret  string `json:"wecom_secret,omitempty"`
	WeComToken   string `json:"wecom_token,omitempty"`
	WeComAESKey  string `json:"wecom_aes_key,omitempty"`
}

// AuthResult is the response to authentication
type AuthResult struct {
	Type      string `json:"type"`
	Success   bool   `json:"success"`
	SessionID string `json:"session_id"`
	Error     string `json:"error,omitempty"`
}

// IncomingMessage is a message from the server
type IncomingMessage struct {
	Type      string            `json:"type"`
	ID        string            `json:"id"`
	Platform  string            `json:"platform"`
	ChannelID string            `json:"channel_id"`
	UserID    string            `json:"user_id"`
	Username  string            `json:"username"`
	Text      string            `json:"text"`
	ThreadID  string            `json:"thread_id"`
	Metadata  map[string]string `json:"metadata"`
}

// OutgoingResponse is sent via webhook
type OutgoingResponse struct {
	Type      string          `json:"type"`
	MessageID string          `json:"message_id"`
	Platform  string          `json:"platform"`
	ChannelID string          `json:"channel_id"`
	Text      string          `json:"text"`
	Files     []OutgoingFile  `json:"files,omitempty"`
}

// OutgoingFile is a file attachment sent via webhook (base64-encoded)
type OutgoingFile struct {
	Name      string `json:"name"`       // filename
	MediaType string `json:"media_type"` // "image", "voice", "video", "file"
	Data      string `json:"data"`       // base64-encoded file content
}

// ErrorMessage is an error notification from the server
type ErrorMessage struct {
	Type    string `json:"type"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message"`
}

// PingPong for heartbeat
type PingPong struct {
	Type string `json:"type"`
}

// RawWeComMessage is received from server with raw encrypted WeCom XML
type RawWeComMessage struct {
	Type         string `json:"type"` // "wecom_raw"
	MsgSignature string `json:"msg_signature"`
	Timestamp    string `json:"timestamp"`
	Nonce        string `json:"nonce"`
	Body         string `json:"body"` // Raw XML body from WeCom
}

// New creates a new relay platform
func New(cfg Config) (*Platform, error) {
	if cfg.UserID == "" {
		return nil, fmt.Errorf("user_id is required")
	}
	if cfg.Platform == "" {
		return nil, fmt.Errorf("platform is required")
	}
	if cfg.Platform != "feishu" && cfg.Platform != "slack" && cfg.Platform != "wechat" && cfg.Platform != "wecom" {
		return nil, fmt.Errorf("platform must be 'feishu', 'slack', 'wechat', or 'wecom'")
	}

	if cfg.ServerURL == "" {
		cfg.ServerURL = DefaultServerURL
	}
	if cfg.WebhookURL == "" {
		cfg.WebhookURL = DefaultWebhookURL
	}

	p := &Platform{
		config:         cfg,
		transcriber:    cfg.Transcriber,
		useMediaProxy:  cfg.UseMediaProxy,
		httpClient:     &http.Client{
			Timeout: 30 * time.Second,
		},
		kfCursors: make(map[string]string),
	}

	// Initialize MsgCrypt for WeCom platform (for local decryption)
	if cfg.Platform == "wecom" && cfg.WeComToken != "" && cfg.WeComAESKey != "" {
		msgCrypt, err := wecom.NewMsgCrypt(cfg.WeComToken, cfg.WeComAESKey, cfg.WeComCorpID)
		if err != nil {
			return nil, fmt.Errorf("failed to create WeCom message cryptographer: %w", err)
		}
		p.msgCrypt = msgCrypt
		log.Printf("[Relay] WeCom local decryption enabled")
	}

	// Initialize WeCom platform for direct API calls (media upload/send) - only if not using proxy
	if cfg.Platform == "wecom" && cfg.WeComCorpID != "" && cfg.WeComSecret != "" && !cfg.UseMediaProxy {
		wp, err := wecom.New(wecom.Config{
			CorpID:         cfg.WeComCorpID,
			AgentID:        cfg.WeComAgentID,
			Secret:         cfg.WeComSecret,
			Token:          cfg.WeComToken,
			EncodingAESKey: cfg.WeComAESKey,
			CallbackPort:   -1, // API-only mode, no HTTP server
		})
		if err != nil {
			log.Printf("[Relay] Warning: failed to create WeCom platform for media API: %v", err)
		} else {
			p.wecomPlatform = wp
			log.Printf("[Relay] WeCom media API enabled")

			// Check if KF messaging is available
			if p.wecomPlatform.CheckKfAvailable() {
				p.kfEnabled = true
				log.Printf("[Relay] WeCom KF messaging enabled")
				// Initialize KF cursors to skip historical messages
				go p.initKfCursors()
			} else {
				log.Printf("[Relay] WeCom KF messaging not available (no permission)")
			}
		}
	}

	// Initialize WeChat OA client for media upload/send (when platform=wechat)
	if cfg.Platform == "wechat" && cfg.WeChatAppID != "" && cfg.WeChatAppSecret != "" {
		p.wechatClient = wechat.NewClient(cfg.WeChatAppID, cfg.WeChatAppSecret)
		log.Printf("[Relay] WeChat OA media API enabled")
	}

	return p, nil
}

// Name returns the platform name
func (p *Platform) Name() string {
	return "relay"
}

// SetMessageHandler sets the callback for incoming messages
func (p *Platform) SetMessageHandler(handler func(msg router.Message)) {
	p.messageHandler = handler
}

// Start begins the relay connection
func (p *Platform) Start(ctx context.Context) error {
	p.ctx, p.cancel = context.WithCancel(ctx)

	// Initial connection
	if err := p.connect(); err != nil {
		return fmt.Errorf("initial connection failed: %w", err)
	}

	// Start read loop
	p.wg.Add(1)
	go p.readLoop()

	// Start heartbeat
	p.wg.Add(1)
	go p.heartbeat()

	log.Printf("[Relay] Connected to %s as user %s (%s)", p.config.ServerURL, p.config.UserID, p.config.Platform)
	return nil
}

// Stop shuts down the relay connection
func (p *Platform) Stop() error {
	if p.cancel != nil {
		p.cancel()
	}

	p.connMu.Lock()
	if p.conn != nil {
		p.conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		p.conn.Close()
	}
	p.connMu.Unlock()

	p.wg.Wait()
	return nil
}

// Send sends a response via webhook (text) and direct WeCom API (files)
func (p *Platform) Send(ctx context.Context, channelID string, resp router.Response) error {
	// Handle KF (customer service) messages directly via WeCom API
	if resp.Metadata != nil && resp.Metadata["kf"] == "true" && p.wecomPlatform != nil {
		if resp.Text != "" {
			if err := p.wecomPlatform.SendKfMessage(resp.Metadata["external_userid"], resp.Metadata["open_kfid"], resp.Text); err != nil {
				return fmt.Errorf("failed to send kf message: %w", err)
			}
		}
		return nil
	}

	// Send text via webhook
	if resp.Text != "" {
		if err := p.sendWebhook(ctx, channelID, resp); err != nil {
			return err
		}
	}

	// Send file attachments directly via platform API or proxy
	for _, file := range resp.Files {
		mediaType := file.MediaType
		if mediaType == "" {
			mediaType = "file"
		}

		switch {
		case p.wechatClient != nil:
			// WeChat OA only supports image/voice/video/thumb uploads.
			// For unsupported file types, read content and send as text.
			wxMediaType := wechatMediaType(file.Path, mediaType)
			if wxMediaType == "" {
				if err := p.sendFileAsText(ctx, channelID, file.Path, resp.Metadata); err != nil {
					return err
				}
				continue
			}

			log.Printf("[Relay] Uploading file to WeChat OA: %s (type=%s)", file.Path, wxMediaType)
			mediaID, err := p.wechatClient.UploadMedia(file.Path, wxMediaType)
			if err != nil {
				log.Printf("[Relay] Failed to upload %s: %v", file.Path, err)
				return fmt.Errorf("failed to upload file %s: %w", file.Path, err)
			}
			log.Printf("[Relay] Upload complete, media_id=%s. Sending to %s", mediaID, channelID)

			switch wxMediaType {
			case "voice":
				err = p.wechatClient.SendVoice(channelID, mediaID)
			case "video":
				err = p.wechatClient.SendVideo(channelID, mediaID, "", "")
			default:
				err = p.wechatClient.SendImage(channelID, mediaID)
			}
			if err != nil {
				log.Printf("[Relay] Failed to send media message: %v", err)
				return fmt.Errorf("failed to send file %s: %w", file.Path, err)
			}
			log.Printf("[Relay] File sent successfully via WeChat OA: %s -> %s", file.Path, channelID)

		case p.useMediaProxy:
			// Use proxy for media upload/send
			log.Printf("[Relay] Uploading file via proxy: %s (type=%s)", file.Path, mediaType)
			
			// When using proxy, we send the file via webhook for server-side handling
			if err := p.sendFileViaWebhook(ctx, channelID, file.Path, mediaType, resp.Metadata); err != nil {
				return err
			}

		case p.wecomPlatform != nil:
			// WeCom: upload + send via WeCom API
			log.Printf("[Relay] Uploading file: %s (type=%s)", file.Path, mediaType)
			mediaID, err := p.wecomPlatform.UploadMedia(file.Path, mediaType)
			if err != nil {
				log.Printf("[Relay] Failed to upload %s: %v", file.Path, err)
				return fmt.Errorf("failed to upload file %s: %w", file.Path, err)
			}
			log.Printf("[Relay] Upload complete, media_id=%s. Sending to %s", mediaID, channelID)

			if err := p.wecomPlatform.SendMediaMessage(channelID, mediaID, mediaType); err != nil {
				log.Printf("[Relay] Failed to send media message: %v", err)
				return fmt.Errorf("failed to send file %s: %w", file.Path, err)
			}
			log.Printf("[Relay] File sent successfully: %s -> %s", file.Path, channelID)

		default:
			// No local media API — send file via webhook for server-side handling
			if p.config.Platform == "wechat" {
				wxMediaType := wechatMediaType(file.Path, mediaType)
				if wxMediaType == "" {
					// Text-based files: send content preview via passive reply
					if err := p.sendFileAsText(ctx, channelID, file.Path, resp.Metadata); err != nil {
						return err
					}
					continue
				}
				// Media files: send base64-encoded via webhook for server to upload+send
				if err := p.sendFileViaWebhook(ctx, channelID, file.Path, wxMediaType, resp.Metadata); err != nil {
					return err
				}
				continue
			}
			log.Printf("[Relay] Cannot send file: no media API initialized")
			return fmt.Errorf("media API not available for file sending")
		}
	}

	return nil
}

// sendWebhook sends a text response via the relay webhook
func (p *Platform) sendWebhook(ctx context.Context, channelID string, resp router.Response) error {
	outgoing := OutgoingResponse{
		Type:      "response",
		MessageID: resp.Metadata["message_id"],
		Platform:  p.config.Platform,
		ChannelID: channelID,
		Text:      resp.Text,
	}

	body, err := json.Marshal(outgoing)
	if err != nil {
		return fmt.Errorf("failed to marshal response: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.config.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Session-ID", p.sessionID)
	req.Header.Set("X-User-ID", p.config.UserID)

	httpResp, err := p.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send webhook: %w", err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode >= 400 {
		return fmt.Errorf("webhook returned status %d", httpResp.StatusCode)
	}

	return nil
}

// sendFileAsText reads a file and sends its content as a truncated text message via webhook (passive reply).
func (p *Platform) sendFileAsText(ctx context.Context, channelID, filePath string, metadata map[string]string) error {
	log.Printf("[Relay] Sending file as text preview (passive): %s", filePath)
	content, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read file %s: %w", filePath, err)
	}
	runes := []rune(string(content))
	const maxRunes = 500
	body := string(content)
	if len(runes) > maxRunes {
		body = string(runes[:maxRunes]) + "\n\n... (内容过长，已截断)"
	}
	text := fmt.Sprintf("📎 %s\n\n%s", filepath.Base(filePath), body)
	if err := p.sendWebhook(ctx, channelID, router.Response{
		Text:     text,
		Metadata: metadata,
	}); err != nil {
		return fmt.Errorf("failed to send file content as text: %w", err)
	}
	return nil
}

// sendFileViaWebhook sends a file as base64-encoded data via webhook for server-side upload+send.
func (p *Platform) sendFileViaWebhook(ctx context.Context, channelID, filePath, mediaType string, metadata map[string]string) error {
	log.Printf("[Relay] Sending file via webhook (server-side): %s (type=%s)", filePath, mediaType)
	content, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read file %s: %w", filePath, err)
	}

	outgoing := OutgoingResponse{
		Type:      "response",
		Platform:  p.config.Platform,
		ChannelID: channelID,
		Files: []OutgoingFile{
			{
				Name:      filepath.Base(filePath),
				MediaType: mediaType,
				Data:      base64.StdEncoding.EncodeToString(content),
			},
		},
	}
	if metadata != nil {
		outgoing.MessageID = metadata["message_id"]
	}

	body, err := json.Marshal(outgoing)
	if err != nil {
		return fmt.Errorf("failed to marshal file response: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.config.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Session-ID", p.sessionID)
	req.Header.Set("X-User-ID", p.config.UserID)

	httpResp, err := p.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send file webhook: %w", err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode >= 400 {
		return fmt.Errorf("file webhook returned status %d", httpResp.StatusCode)
	}

	log.Printf("[Relay] File sent via webhook successfully: %s -> %s", filePath, channelID)
	return nil
}

// connect establishes WebSocket connection and authenticates
func (p *Platform) connect() error {
	debug.Log("Connecting to %s", p.config.ServerURL)

	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	conn, resp, err := dialer.DialContext(p.ctx, p.config.ServerURL, nil)
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}
	debug.Log("WebSocket connected, status: %s", resp.Status)

	// Send authentication
	authMsg := AuthMessage{
		Type:          "auth",
		UserID:        p.config.UserID,
		Platform:      p.config.Platform,
		Token:         p.config.Token,
		ClientVersion: ClientVersion,
		AIProvider:    p.config.AIProvider,
		AIModel:       p.config.AIModel,
		WeComCorpID:   p.config.WeComCorpID,
		WeComAgentID:  p.config.WeComAgentID,
		WeComSecret:   p.config.WeComSecret,
		WeComToken:    p.config.WeComToken,
		WeComAESKey:   p.config.WeComAESKey,
	}

	debug.Log("Sending auth message")
	conn.SetWriteDeadline(time.Now().Add(writeTimeout))
	if err := conn.WriteJSON(authMsg); err != nil {
		conn.Close()
		return fmt.Errorf("failed to send auth: %w", err)
	}

	// Wait for auth response
	debug.Log("Waiting for auth response")
	conn.SetReadDeadline(time.Now().Add(readTimeout))
	var authResult AuthResult
	if err := conn.ReadJSON(&authResult); err != nil {
		conn.Close()
		return fmt.Errorf("failed to read auth response: %w", err)
	}
	debug.Log("Auth response: success=%v, session=%s", authResult.Success, authResult.SessionID)

	if authResult.Type != "auth_result" {
		conn.Close()
		return fmt.Errorf("unexpected response type: %s", authResult.Type)
	}

	if !authResult.Success {
		conn.Close()
		return fmt.Errorf("authentication failed: %s", authResult.Error)
	}

	// Set up pong handler to reset read deadline
	conn.SetPongHandler(func(appData string) error {
		debug.Log("Received pong")
		conn.SetReadDeadline(time.Now().Add(readTimeout))
		return nil
	})

	// Set up ping handler
	conn.SetPingHandler(func(appData string) error {
		debug.Log("Received ping from server")
		conn.SetReadDeadline(time.Now().Add(readTimeout))
		conn.SetWriteDeadline(time.Now().Add(writeTimeout))
		return conn.WriteMessage(websocket.PongMessage, []byte(appData))
	})

	p.connMu.Lock()
	p.conn = conn
	p.sessionID = authResult.SessionID
	p.connMu.Unlock()

	log.Printf("[Relay] Authenticated, session: %s", p.sessionID)
	return nil
}

// readLoop handles incoming WebSocket messages
func (p *Platform) readLoop() {
	defer p.wg.Done()

	retryDelay := initialRetryDelay

	for {
		select {
		case <-p.ctx.Done():
			debug.Log("Context done, exiting readLoop")
			return
		default:
		}

		p.connMu.Lock()
		conn := p.conn
		p.connMu.Unlock()

		if conn == nil {
			debug.Log("No connection, reconnecting")
			p.reconnect(&retryDelay)
			continue
		}

		debug.Log("Waiting for message (timeout: %v)", readTimeout)
		conn.SetReadDeadline(time.Now().Add(readTimeout))
		msgType, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				log.Printf("[Relay] Connection closed normally")
				return
			}

			// Check for close error with reason (e.g., duplicate connection)
			if closeErr, ok := err.(*websocket.CloseError); ok {
				// Policy violation means another client connected with same user-id
				if closeErr.Code == websocket.ClosePolicyViolation {
					log.Printf("[Relay] Disconnected: %s", closeErr.Text)
					log.Printf("[Relay] Exiting - please ensure only one client is running per user-id")
					os.Exit(1)
				}
				if closeErr.Text != "" {
					log.Printf("[Relay] Connection closed by server: %s", closeErr.Text)
				} else {
					log.Printf("[Relay] Connection closed with code %d", closeErr.Code)
				}
			} else {
				debug.Log("Read error (msgType=%d): %v", msgType, err)
				log.Printf("[Relay] Read error: %v", err)
			}
			p.connMu.Lock()
			if p.conn != nil {
				p.conn.Close()
				p.conn = nil
			}
			p.connMu.Unlock()

			p.reconnect(&retryDelay)
			continue
		}

		debug.Log("Received message (type=%d, len=%d)", msgType, len(message))

		// Reset retry delay on successful read
		retryDelay = initialRetryDelay

		// Parse message type
		var jsonMsg struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(message, &jsonMsg); err != nil {
			debug.Log("Failed to parse JSON: %v, raw: %s", err, string(message))
			log.Printf("[Relay] Failed to parse message type: %v", err)
			continue
		}

		debug.Log("Message type: %s", jsonMsg.Type)

		switch jsonMsg.Type {
		case "ping":
			debug.Log("Received app-level ping, sending pong")
			p.sendPong()
		case "pong":
			debug.Log("Received app-level pong")
		case "message":
			debug.Log("Received message, handling")
			p.handleMessage(message)
		case "wecom_raw":
			debug.Log("Received raw WeCom message, decrypting locally")
			p.handleRawWeComMessage(message)
		case "error":
			p.handleError(message)
		default:
			log.Printf("[Relay] Unknown message type: %s", jsonMsg.Type)
		}
	}
}

// handleMessage processes an incoming message
func (p *Platform) handleMessage(data []byte) {
	var msg IncomingMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		log.Printf("[Relay] Failed to parse message: %v", err)
		return
	}

	// Log detailed message info for debugging
	log.Printf("[Relay] Received message: id=%s, platform=%s, user_id=%s, channel_id=%s",
		msg.ID, msg.Platform, msg.UserID, msg.ChannelID)
	if msg.Metadata != nil {
		if corpID := msg.Metadata["corp_id"]; corpID != "" {
			log.Printf("[Relay] Message metadata: corp_id=%s, agent_id=%s, chat_type=%s",
				corpID, msg.Metadata["agent_id"], msg.Metadata["chat_type"])
		}
	}
	log.Printf("[Relay] Message content from %s: %s", msg.Username, msg.Text)

	if p.messageHandler != nil {
		metadata := msg.Metadata
		if metadata == nil {
			metadata = make(map[string]string)
		}
		metadata["message_id"] = msg.ID

		p.messageHandler(router.Message{
			ID:        msg.ID,
			Platform:  "relay",
			ChannelID: msg.ChannelID,
			UserID:    msg.UserID,
			Username:  msg.Username,
			Text:      msg.Text,
			ThreadID:  msg.ThreadID,
			Metadata:  metadata,
		})
	}
}

// handleRawWeComMessage decrypts and processes a raw WeCom message locally
func (p *Platform) handleRawWeComMessage(data []byte) {
	var rawMsg RawWeComMessage
	if err := json.Unmarshal(data, &rawMsg); err != nil {
		log.Printf("[Relay] Failed to parse raw WeCom message: %v", err)
		return
	}

	if p.msgCrypt == nil {
		log.Printf("[Relay] Cannot decrypt WeCom message: MsgCrypt not initialized")
		return
	}

	// Parse the encrypted XML
	var encryptedMsg wecom.EncryptedMsg
	if err := xml.Unmarshal([]byte(rawMsg.Body), &encryptedMsg); err != nil {
		log.Printf("[Relay] Failed to parse WeCom XML: %v", err)
		return
	}

	log.Printf("[Relay] Raw WeCom: ToUserName=%s, AgentID=%s (our agent: %s)",
		encryptedMsg.ToUserName, encryptedMsg.AgentID, p.config.WeComAgentID)

	// Check if this message is for our agent (skip messages from other apps in same corp)
	if encryptedMsg.AgentID != "" && p.config.WeComAgentID != "" && encryptedMsg.AgentID != p.config.WeComAgentID {
		log.Printf("[Relay] Skipping message from different agent: %s", encryptedMsg.AgentID)
		return
	}

	// Decrypt the message locally
	plaintext, err := p.msgCrypt.DecryptMsg(rawMsg.MsgSignature, rawMsg.Timestamp, rawMsg.Nonce, &encryptedMsg)
	if err != nil {
		log.Printf("[Relay] Failed to decrypt WeCom message (agent=%s): %v", encryptedMsg.AgentID, err)
		return
	}

	// Parse the decrypted message
	var receivedMsg wecom.ReceivedMsg
	if err := xml.Unmarshal(plaintext, &receivedMsg); err != nil {
		log.Printf("[Relay] Failed to parse decrypted message: %v", err)
		return
	}

	// Handle event messages
	if receivedMsg.MsgType == "event" {
		if receivedMsg.Event == "kf_msg_or_event" {
			log.Printf("[Relay] Received kf_msg_or_event, token=%s", receivedMsg.Token)
			p.handleKfEvent(receivedMsg.Token)
		} else {
			log.Printf("[Relay] Ignoring event: %s", receivedMsg.Event)
		}
		return
	}

	userID := receivedMsg.FromUserName
	routerMsg := router.Message{
		ID:        receivedMsg.MsgId,
		Platform:  "relay",
		ChannelID: userID,
		UserID:    userID,
		Username:  userID,
		Text:      strings.TrimSpace(receivedMsg.Content),
		Metadata: map[string]string{
			"message_id": receivedMsg.MsgId,
			"agent_id":   receivedMsg.AgentID,
			"corp_id":    p.config.WeComCorpID,
			"msg_type":   receivedMsg.MsgType,
		},
	}

	switch receivedMsg.MsgType {
	case "text":
		if routerMsg.Text == "" {
			return
		}
	case "image":
		routerMsg.MediaID = receivedMsg.MediaId
		routerMsg.Metadata["pic_url"] = receivedMsg.PicUrl
		
		if p.wecomPlatform != nil {
			log.Printf("[Relay] Downloading image: media_id=%s", receivedMsg.MediaId)
			
			tempDir := os.TempDir()
			tempFile := filepath.Join(tempDir, fmt.Sprintf("relay_image_%s.jpg", receivedMsg.MediaId))
			
			var err error
			if p.useMediaProxy {
				err = p.proxyGetMedia(receivedMsg.MediaId, tempFile)
			} else {
				err = p.wecomPlatform.GetMedia(receivedMsg.MediaId, tempFile)
			}
			
			if err != nil {
				log.Printf("[Relay] ❌ Failed to download image: %v", err)
				routerMsg.Text = "[图片] (下载失败)"
			} else {
				if fileInfo, err := os.Stat(tempFile); err == nil {
					log.Printf("[Relay] ✅ Downloaded image: size=%d bytes", fileInfo.Size())
					
					if imgData, err := os.ReadFile(tempFile); err == nil {
						routerMsg.Attachments = append(routerMsg.Attachments, router.Attachment{
							Type:     "image",
							Data:     imgData,
							MIMEType: "image/jpeg",
						})
						routerMsg.Text = ""
						log.Printf("[Relay] ✅ Added image to message attachments")
					}
				}
				defer os.Remove(tempFile)
			}
		} else {
			routerMsg.Text = "[图片]"
		}
	case "voice":
		routerMsg.MediaID = receivedMsg.MediaId
		routerMsg.Metadata["format"] = receivedMsg.Format
		
		// Transcribe voice to text if transcriber is available
		log.Printf("[Relay] Voice message received: media_id=%s, format=%s", receivedMsg.MediaId, receivedMsg.Format)
		log.Printf("[Relay] transcriber available: %v, wecomPlatform available: %v", (p.transcriber != nil), (p.wecomPlatform != nil))
		
		if p.transcriber != nil && (p.wecomPlatform != nil || p.useMediaProxy) {
			log.Printf("[Relay] Starting voice transcription, media_id=%s", receivedMsg.MediaId)
			
			// Download voice file
			tempDir := os.TempDir()
			tempFile := filepath.Join(tempDir, fmt.Sprintf("relay_voice_%s.%s", receivedMsg.MediaId, receivedMsg.Format))
			wavFile := filepath.Join(tempDir, fmt.Sprintf("relay_voice_%s.wav", receivedMsg.MediaId))
			log.Printf("[Relay] Downloading to: %s", tempFile)
			
			var err error
			if p.useMediaProxy {
				err = p.proxyGetMedia(receivedMsg.MediaId, tempFile)
			} else {
				err = p.wecomPlatform.GetMedia(receivedMsg.MediaId, tempFile)
			}
			
			if err != nil {
				log.Printf("[Relay] ❌ Failed to download voice: %v", err)
				routerMsg.Text = "[语音] (下载失败)"
			} else {
				// Check file size and content
				if fileInfo, err := os.Stat(tempFile); err == nil {
					log.Printf("[Relay] ✅ Downloaded voice file: size=%d bytes", fileInfo.Size())
					
					// Read first 512 bytes to check what it is
					if fileContent, err := os.ReadFile(tempFile); err == nil {
						previewLen := min(512, len(fileContent))
						log.Printf("[Relay] File content preview (first %d bytes): %q", previewLen, fileContent[:previewLen])
					}
				}
				
				// Convert AMR to WAV using ffmpeg if needed
				var audioFile string
				if receivedMsg.Format == "amr" {
					log.Printf("[Relay] Converting AMR to WAV...")
					if err := convertAMRToWAV(tempFile, wavFile); err != nil {
						log.Printf("[Relay] ❌ Failed to convert AMR to WAV: %v", err)
						routerMsg.Text = "[语音] (格式转换失败)"
					} else {
						log.Printf("[Relay] ✅ Converted to WAV")
						audioFile = wavFile
					}
				} else {
					audioFile = tempFile
				}
				
				if audioFile != "" {
					// Read audio file
					audio, err := os.ReadFile(audioFile)
					if err != nil {
						log.Printf("[Relay] ❌ Failed to read voice file: %v", err)
						routerMsg.Text = "[语音] (读取失败)"
					} else {
						log.Printf("[Relay] ✅ Read audio data: %d bytes", len(audio))
						
						// Transcribe to text
						transcribed, err := p.transcriber.Transcribe(p.ctx, audio)
						if err != nil {
							log.Printf("[Relay] ❌ Failed to transcribe voice: %v", err)
							routerMsg.Text = "[语音] (转文字失败)"
						} else {
							routerMsg.Text = transcribed
							routerMsg.Metadata["message_type"] = "voice"
							log.Printf("[Relay] ✅ Transcribed voice: %s", transcribed)
						}
					}
				}
			}
			
			// Clean up temp files
			defer func() {
				os.Remove(tempFile)
				os.Remove(wavFile)
			}()
		} else {
			log.Printf("[Relay] ⚠️ No transcriber or wecomPlatform available")
			routerMsg.Text = "[语音]"
		}
	case "video":
		routerMsg.MediaID = receivedMsg.MediaId
		routerMsg.Text = "[视频]"
	case "file":
		routerMsg.MediaID = receivedMsg.MediaId
		routerMsg.FileName = receivedMsg.FileName
		routerMsg.Metadata["file_size"] = receivedMsg.FileSize
		
		if p.wecomPlatform != nil || p.useMediaProxy {
			log.Printf("[Relay] Downloading file: media_id=%s, filename=%s", receivedMsg.MediaId, receivedMsg.FileName)
			
			tempDir := os.TempDir()
			tempFile := filepath.Join(tempDir, receivedMsg.FileName)
			
			var err error
			if p.useMediaProxy {
				err = p.proxyGetMedia(receivedMsg.MediaId, tempFile)
			} else {
				err = p.wecomPlatform.GetMedia(receivedMsg.MediaId, tempFile)
			}
			
			if err != nil {
				log.Printf("[Relay] ❌ Failed to download file: %v", err)
				routerMsg.Text = "[文件] " + receivedMsg.FileName + " (下载失败)"
			} else {
				if fileInfo, err := os.Stat(tempFile); err == nil {
					log.Printf("[Relay] ✅ Downloaded file: size=%d bytes", fileInfo.Size())
					
					if fileData, err := os.ReadFile(tempFile); err == nil {
						ext := filepath.Ext(receivedMsg.FileName)
						mimeType := "application/octet-stream"
						switch ext {
						case ".pdf":
							mimeType = "application/pdf"
						case ".doc":
							mimeType = "application/msword"
						case ".docx":
							mimeType = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
						case ".xls":
							mimeType = "application/vnd.ms-excel"
						case ".xlsx":
							mimeType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
						case ".ppt":
							mimeType = "application/vnd.ms-powerpoint"
						case ".pptx":
							mimeType = "application/vnd.openxmlformats-officedocument.presentationml.presentation"
						case ".txt":
							mimeType = "text/plain"
						case ".md":
							mimeType = "text/markdown"
						case ".json":
							mimeType = "application/json"
						case ".zip":
							mimeType = "application/zip"
						case ".rar":
							mimeType = "application/x-rar-compressed"
						case ".png":
							mimeType = "image/png"
						case ".jpg", ".jpeg":
							mimeType = "image/jpeg"
						case ".gif":
							mimeType = "image/gif"
						}
						
						routerMsg.Attachments = append(routerMsg.Attachments, router.Attachment{
							Type:     "file",
							Data:     fileData,
							MIMEType: mimeType,
						})
						routerMsg.Text = ""
						log.Printf("[Relay] ✅ Added file to message attachments: %s (type: %s)", receivedMsg.FileName, mimeType)
					}
				}
				defer os.Remove(tempFile)
			}
		} else {
			routerMsg.Text = "[文件] " + receivedMsg.FileName
		}
	default:
		log.Printf("[Relay] Ignoring WeCom message type: %s", receivedMsg.MsgType)
		return
	}

	log.Printf("[Relay] Decrypted WeCom message: user_id=%s, msg_id=%s, agent_id=%s, type=%s",
		userID, receivedMsg.MsgId, receivedMsg.AgentID, receivedMsg.MsgType)
	log.Printf("[Relay] Message content from %s: %s", userID, routerMsg.Text)

	if p.messageHandler != nil {
		p.messageHandler(routerMsg)
	}
}

// initKfCursors does an initial sync to set cursors, skipping historical messages
func (p *Platform) initKfCursors() {
	if p.wecomPlatform == nil {
		return
	}

	accounts, err := p.wecomPlatform.ListKfAccounts()
	if err != nil {
		log.Printf("[Relay] Failed to list kf accounts for cursor init: %v", err)
		return
	}

	for _, acc := range accounts {
		result, err := p.wecomPlatform.SyncKfMessages("", "", acc.OpenKfID, 1000)
		if err != nil {
			log.Printf("[Relay] Failed to init cursor for kf %s: %v", acc.OpenKfID, err)
			continue
		}
		if result.NextCursor != "" {
			p.kfCursorsMu.Lock()
			p.kfCursors[acc.OpenKfID] = result.NextCursor
			p.kfCursorsMu.Unlock()
		}
		log.Printf("[Relay] KF cursor initialized for %s (%s): skipped %d historical messages", acc.Name, acc.OpenKfID, len(result.MsgList))
	}
}

// handleKfEvent processes a kf_msg_or_event by calling sync_msg to fetch actual messages
func (p *Platform) handleKfEvent(token string) {
	if p.wecomPlatform == nil || !p.kfEnabled {
		return
	}

	// List kf accounts to get open_kfid(s)
	accounts, err := p.wecomPlatform.ListKfAccounts()
	if err != nil {
		log.Printf("[Relay] Failed to list kf accounts: %v", err)
		return
	}

	if len(accounts) == 0 {
		log.Printf("[Relay] No kf accounts found")
		return
	}

	// Sync messages from each kf account, using saved cursor to avoid re-processing
	var allMessages []wecom.KfMessage
	for _, acc := range accounts {
		p.kfCursorsMu.Lock()
		cursor := p.kfCursors[acc.OpenKfID]
		p.kfCursorsMu.Unlock()

		result, err := p.wecomPlatform.SyncKfMessages(token, cursor, acc.OpenKfID, 1000)
		if err != nil {
			log.Printf("[Relay] Failed to sync kf messages for %s (%s): %v", acc.Name, acc.OpenKfID, err)
			continue
		}

		// Save cursor for next sync
		if result.NextCursor != "" {
			p.kfCursorsMu.Lock()
			p.kfCursors[acc.OpenKfID] = result.NextCursor
			p.kfCursorsMu.Unlock()
		}

		log.Printf("[Relay] KF sync_msg for %s: %d messages, has_more=%d, cursor=%s", acc.Name, len(result.MsgList), result.HasMore, result.NextCursor)
		allMessages = append(allMessages, result.MsgList...)
	}

	for _, msg := range allMessages {
		// Only process messages from customers (origin=3)
		if msg.Origin != 3 {
			log.Printf("[Relay] Skipping kf message origin=%d, type=%s", msg.Origin, msg.MsgType)
			continue
		}

		if msg.MsgType != "text" || msg.Text == nil || msg.Text.Content == "" {
			log.Printf("[Relay] Skipping non-text kf message: type=%s, msgid=%s", msg.MsgType, msg.MsgID)
			continue
		}

		log.Printf("[Relay] KF message from %s via %s: %s", msg.ExternalUserID, msg.OpenKfID, msg.Text.Content)

		routerMsg := router.Message{
			ID:        msg.MsgID,
			Platform:  "relay",
			ChannelID: msg.ExternalUserID,
			UserID:    msg.ExternalUserID,
			Username:  msg.ExternalUserID,
			Text:      strings.TrimSpace(msg.Text.Content),
			Metadata: map[string]string{
				"message_id":      msg.MsgID,
				"corp_id":         p.config.WeComCorpID,
				"msg_type":        msg.MsgType,
				"kf":              "true",
				"open_kfid":       msg.OpenKfID,
				"external_userid": msg.ExternalUserID,
			},
		}

		if p.messageHandler != nil {
			p.messageHandler(routerMsg)
		}
	}
}

// handleError processes an error message from the server
func (p *Platform) handleError(data []byte) {
	var errMsg ErrorMessage
	if err := json.Unmarshal(data, &errMsg); err != nil {
		log.Printf("[Relay] Failed to parse error message: %v", err)
		return
	}

	if errMsg.Code != "" {
		log.Printf("[Relay] Server error [%s]: %s", errMsg.Code, errMsg.Message)
	} else {
		log.Printf("[Relay] Server error: %s", errMsg.Message)
	}
}

// sendPong sends a pong response
func (p *Platform) sendPong() {
	p.connMu.Lock()
	defer p.connMu.Unlock()

	if p.conn == nil {
		return
	}

	pong := PingPong{Type: "pong"}
	p.conn.SetWriteDeadline(time.Now().Add(writeTimeout))
	if err := p.conn.WriteJSON(pong); err != nil {
		log.Printf("[Relay] Failed to send pong: %v", err)
	}
}

// heartbeat sends periodic pings to keep connection alive
func (p *Platform) heartbeat() {
	defer p.wg.Done()

	debug.Log("Heartbeat started, waiting 500ms before first ping")

	// Short delay then send initial ping
	time.Sleep(500 * time.Millisecond)
	p.sendPing()

	// Send pings frequently to keep connection alive
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-p.ctx.Done():
			debug.Log("Heartbeat stopped (context done)")
			return
		case <-ticker.C:
			p.sendPing()
		}
	}
}

func (p *Platform) sendPing() {
	p.connMu.Lock()
	conn := p.conn
	p.connMu.Unlock()

	if conn == nil {
		debug.Log("sendPing: no connection")
		return
	}

	debug.Log("Sending WebSocket ping")
	// Use WebSocket-level ping for better proxy/load balancer compatibility
	conn.SetWriteDeadline(time.Now().Add(writeTimeout))
	if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
		debug.Log("sendPing error: %v", err)
		log.Printf("[Relay] Failed to send ping: %v", err)
	} else {
		debug.Log("Ping sent successfully")
	}
}

// reconnect attempts to reconnect with exponential backoff
func (p *Platform) reconnect(retryDelay *time.Duration) {
	select {
	case <-p.ctx.Done():
		return
	default:
	}

	log.Printf("[Relay] Reconnecting in %v...", *retryDelay)

	select {
	case <-p.ctx.Done():
		return
	case <-time.After(*retryDelay):
	}

	if err := p.connect(); err != nil {
		log.Printf("[Relay] Reconnection failed: %v", err)

		// Exponential backoff
		*retryDelay *= 2
		if *retryDelay > maxRetryDelay {
			*retryDelay = maxRetryDelay
		}
	} else {
		log.Printf("[Relay] Reconnected successfully")
		*retryDelay = initialRetryDelay
	}
}

// wechatMediaType maps a file path and media type hint to a WeChat OA media type.
// Returns "" if the file type is not supported by WeChat OA media upload.
func wechatMediaType(filePath, mediaType string) string {
	// If already a supported WeChat media type, use it directly
	switch mediaType {
	case "image", "voice", "video", "thumb":
		return mediaType
	}

	// Infer from file extension
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".gif", ".bmp":
		return "image"
	case ".amr", ".mp3", ".speex":
		return "voice"
	case ".mp4":
		return "video"
	default:
		return ""
	}
}

// findFFmpeg tries to find ffmpeg executable in common locations
func findFFmpeg() (string, error) {
	// Try PATH first
	if path, err := exec.LookPath("ffmpeg"); err == nil {
		return path, nil
	}

	// Common Windows installation paths
	commonPaths := []string{
		"C:\\Program Files\\ffmpeg\\bin\\ffmpeg.exe",
		"C:\\Program Files (x86)\\ffmpeg\\bin\\ffmpeg.exe",
		"C:\\ffmpeg\\bin\\ffmpeg.exe",
		filepath.Join(os.Getenv("PROGRAMFILES"), "Gyan.FFmpeg\\bin\\ffmpeg.exe"),
		filepath.Join(os.Getenv("PROGRAMFILES(X86)"), "Gyan.FFmpeg\\bin\\ffmpeg.exe"),
	}

	for _, path := range commonPaths {
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}

	// Try winget installation path (with version number)
	wingetPackagesDir := filepath.Join(os.Getenv("LOCALAPPDATA"), "Microsoft\\WinGet\\Packages\\Gyan.FFmpeg_Microsoft.Winget.Source_8wekyb3d8bbwe")
	if entries, err := os.ReadDir(wingetPackagesDir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() && strings.HasPrefix(entry.Name(), "ffmpeg-") {
				ffmpegPath := filepath.Join(wingetPackagesDir, entry.Name(), "bin", "ffmpeg.exe")
				if _, err := os.Stat(ffmpegPath); err == nil {
					return ffmpegPath, nil
				}
			}
		}
	}

	return "", fmt.Errorf("ffmpeg not found in PATH or common locations")
}

// convertAMRToWAV converts an AMR audio file to WAV format using ffmpeg
func convertAMRToWAV(inputPath, outputPath string) error {
	// Check if ffmpeg is available
	ffmpegPath, err := findFFmpeg()
	if err != nil {
		return fmt.Errorf("ffmpeg not found: %w", err)
	}

	log.Printf("[Relay] Using FFmpeg: %s", ffmpegPath)

	// Run ffmpeg to convert AMR to WAV
	cmd := exec.Command(ffmpegPath, "-i", inputPath, "-ar", "16000", "-ac", "1", "-c:a", "pcm_s16le", "-y", outputPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg failed: %w\n%s", err, output)
	}

	return nil
}

// getProxyURL derives the proxy API URL from the relay server URL
func (p *Platform) getProxyURL(path string) string {
	serverURL := p.config.ServerURL
	if strings.HasPrefix(serverURL, "wss://") {
		return "https://" + strings.TrimPrefix(serverURL, "wss://") + path
	} else if strings.HasPrefix(serverURL, "ws://") {
		return "http://" + strings.TrimPrefix(serverURL, "ws://") + path
	}
	return ""
}

// proxyGetMedia downloads a media file through the relay server
func (p *Platform) proxyGetMedia(mediaID string, savePath string) error {
	proxyURL := p.getProxyURL("/proxy/media/get")
	if proxyURL == "" {
		return fmt.Errorf("invalid relay server URL")
	}

	url := fmt.Sprintf("%s?media_id=%s", proxyURL, mediaID)
	req, err := http.NewRequestWithContext(p.ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Session-ID", p.sessionID)
	req.Header.Set("X-User-ID", p.config.UserID)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("proxy returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	// Check if response is an error JSON
	if len(body) > 0 && body[0] == '{' {
		var errResult struct {
			ErrCode int    `json:"errcode"`
			ErrMsg  string `json:"errmsg"`
		}
		if json.Unmarshal(body, &errResult) == nil && errResult.ErrCode != 0 {
			return fmt.Errorf("proxy API error: %d - %s", errResult.ErrCode, errResult.ErrMsg)
		}
	}

	if err := os.MkdirAll(filepath.Dir(savePath), 0o755); err != nil {
		return err
	}

	return os.WriteFile(savePath, body, 0o644)
}

// proxyUploadMedia uploads a media file through the relay server
func (p *Platform) proxyUploadMedia(filePath string, mediaType string) (string, error) {
	proxyURL := p.getProxyURL("/proxy/media/upload")
	if proxyURL == "" {
		return "", fmt.Errorf("invalid relay server URL")
	}

	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("media", filepath.Base(filePath))
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(part, file); err != nil {
		return "", err
	}
	writer.Close()

	url := fmt.Sprintf("%s?type=%s", proxyURL, mediaType)
	req, err := http.NewRequestWithContext(p.ctx, http.MethodPost, url, body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-Session-ID", p.sessionID)
	req.Header.Set("X-User-ID", p.config.UserID)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("proxy returned status %d", resp.StatusCode)
	}

	var result struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
		MediaID string `json:"media_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	if result.ErrCode != 0 {
		return "", fmt.Errorf("proxy API error: %d - %s", result.ErrCode, result.ErrMsg)
	}

	return result.MediaID, nil
}
