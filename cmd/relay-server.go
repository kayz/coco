package cmd

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
	"github.com/spf13/cobra"
)

var (
	relayServerPort int
)

var relayServerCmd = &cobra.Command{
	Use:   "relay-server",
	Short: "Start the self-hosted cloud relay server",
	Long: `Start a self-hosted cloud relay server for Lingti-Bot.

This server provides:
  - WebSocket endpoint for local client connections
  - WeCom callback handler
  - Webhook endpoint for message sending
  - Media file proxy endpoints

For deployment guide, see: https://github.com/pltanton/lingti-bot/docs/cloud-relay-guide.md`,
	Run: runRelayServer,
}

func init() {
	rootCmd.AddCommand(relayServerCmd)
	relayServerCmd.Flags().IntVar(&relayServerPort, "port", 8080, "Server port")
}

// session represents a connected client
type session struct {
	conn *websocket.Conn
	userID string
}

var (
	upgrader = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}
	sessions = make(map[string]*session)
)

func runRelayServer(cmd *cobra.Command, args []string) {
	mux := http.NewServeMux()

	// WebSocket endpoint
	mux.HandleFunc("/ws", handleWebSocket)

	// WeCom callback endpoint
	mux.HandleFunc("/wecom", handleWeComCallback)

	// Webhook endpoint
	mux.HandleFunc("/webhook", handleWebhook)

	// Media proxy endpoints
	mux.HandleFunc("/proxy/media/get", handleProxyMediaGet)
	mux.HandleFunc("/proxy/media/upload", handleProxyMediaUpload)

	// Health check
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	addr := fmt.Sprintf(":%d", relayServerPort)
	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	// Start server in goroutine
	go func() {
		log.Printf("Relay server starting on %s", addr)
		log.Printf("WebSocket: ws://localhost%s/ws", addr)
		log.Printf("WeCom callback: http://localhost%s/wecom", addr)
		log.Printf("Webhook: http://localhost%s/webhook", addr)
		log.Printf("Health check: http://localhost%s/health", addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	log.Println("Shutting down server...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("Server shutdown error: %v", err)
	}
	log.Println("Server stopped")
}

func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}
	defer conn.Close()

	log.Println("New WebSocket connection established")

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			log.Printf("WebSocket read error: %v", err)
			return
		}
		log.Printf("Received message: %s", msg)
	}
}

func handleWeComCallback(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(r.URL.Query().Get("echostr")))
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("success"))
}

func handleWebhook(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

func handleProxyMediaGet(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

func handleProxyMediaUpload(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}
