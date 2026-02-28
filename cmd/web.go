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

	"github.com/kayz/coco/internal/agent"
	"github.com/kayz/coco/internal/webui"
	"github.com/spf13/cobra"
)

var webPort int

var webCmd = &cobra.Command{
	Use:   "web",
	Short: "Run coco Web UI server",
	Run:   runWeb,
}

func init() {
	rootCmd.AddCommand(webCmd)
	webCmd.Flags().IntVar(&webPort, "port", 18080, "Web UI listen port")
}

func runWeb(cmd *cobra.Command, args []string) {
	aiAgent, err := agent.New(agent.Config{
		AllowedPaths:          loadAllowedPaths(),
		BlockedCommands:       loadBlockedCommands(),
		RequireConfirmation:   loadRequireConfirmation(),
		AllowFrom:             loadAllowFrom(),
		RequireMentionInGroup: loadRequireMentionInGroup(),
		DisableFileTools:      loadDisableFileTools(),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating agent: %v\n", err)
		os.Exit(1)
	}

	server := webui.NewServer(aiAgent)
	httpServer := &http.Server{
		Addr:              fmt.Sprintf(":%d", webPort),
		Handler:           server.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Printf("Web UI listening on http://127.0.0.1:%d", webPort)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Web UI server error: %v", err)
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(ctx)
}
