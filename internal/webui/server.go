package webui

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/kayz/coco/internal/router"
)

type MessageProcessor interface {
	HandleMessage(ctx context.Context, msg router.Message) (router.Response, error)
}

type Server struct {
	processor MessageProcessor
	startedAt time.Time
}

func NewServer(processor MessageProcessor) *Server {
	return &Server{
		processor: processor,
		startedAt: time.Now().UTC(),
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/api/status", s.handleStatus)
	mux.HandleFunc("/api/chat", s.handleChat)
	return mux
}

func (s *Server) handleIndex(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(defaultIndexHTML))
}

func (s *Server) handleStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"started_at": s.startedAt.Format(time.RFC3339),
		"uptime_sec": int(time.Since(s.startedAt).Seconds()),
	})
}

type chatRequest struct {
	SessionID string `json:"session_id"`
	UserID    string `json:"user_id"`
	Text      string `json:"text"`
}

type chatResponse struct {
	Text string `json:"text"`
}

func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if s.processor == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "processor is not initialized"})
		return
	}

	var req chatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json body"})
		return
	}

	req.Text = strings.TrimSpace(req.Text)
	req.SessionID = strings.TrimSpace(req.SessionID)
	req.UserID = strings.TrimSpace(req.UserID)
	if req.Text == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "text is required"})
		return
	}
	if req.SessionID == "" {
		req.SessionID = "web-default"
	}
	if req.UserID == "" {
		req.UserID = "web-user"
	}

	resp, err := s.processor.HandleMessage(r.Context(), router.Message{
		Platform:  "web",
		ChannelID: req.SessionID,
		UserID:    req.UserID,
		Username:  req.UserID,
		Text:      req.Text,
		Metadata: map[string]string{
			"chat_type": "private",
		},
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, chatResponse{Text: resp.Text})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

const defaultIndexHTML = `<!doctype html>
<html>
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>coco Web UI</title>
  <style>
    body { font-family: "Segoe UI", sans-serif; margin: 0; background: linear-gradient(145deg,#f7fafc,#e9eef7); color: #1f2937; }
    .wrap { max-width: 900px; margin: 0 auto; padding: 20px; }
    .panel { background: #fff; border-radius: 12px; box-shadow: 0 8px 30px rgba(15,23,42,.08); padding: 16px; }
    #log { min-height: 320px; max-height: 60vh; overflow: auto; white-space: pre-wrap; border: 1px solid #d1d5db; border-radius: 8px; padding: 12px; background: #f9fafb; }
    .row { display: flex; gap: 8px; margin-top: 10px; }
    input { flex: 1; padding: 10px; border: 1px solid #cbd5e1; border-radius: 8px; }
    button { padding: 10px 16px; border: 0; border-radius: 8px; background: #0f766e; color: #fff; cursor: pointer; }
    button:hover { background: #0d9488; }
  </style>
</head>
<body>
  <div class="wrap">
    <div class="panel">
      <h2>coco Web UI</h2>
      <div id="log"></div>
      <div class="row">
        <input id="msg" placeholder="输入消息..." />
        <button id="send">发送</button>
      </div>
    </div>
  </div>
  <script>
    const log = document.getElementById('log');
    const msg = document.getElementById('msg');
    const send = document.getElementById('send');
    const sessionId = 'web-' + Date.now();
    const append = (role, text) => { log.textContent += role + ': ' + text + '\n\n'; log.scrollTop = log.scrollHeight; };
    async function sendMessage() {
      const text = msg.value.trim();
      if (!text) return;
      append('You', text);
      msg.value = '';
      const resp = await fetch('/api/chat', { method:'POST', headers:{'Content-Type':'application/json'}, body: JSON.stringify({ session_id: sessionId, user_id:'web-user', text })});
      const data = await resp.json();
      append('coco', data.text || data.error || '(empty)');
    }
    send.addEventListener('click', sendMessage);
    msg.addEventListener('keydown', (e) => { if (e.key === 'Enter') sendMessage(); });
  </script>
</body>
</html>`
