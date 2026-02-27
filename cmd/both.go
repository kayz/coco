package cmd

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/kayz/coco/internal/config"
	"github.com/spf13/cobra"
)

var bothServiceAction string

var bothCmd = &cobra.Command{
	Use:   "both",
	Short: "Run keeper and relay in one process",
	Long: `Run keeper and relay in one process.

This mode starts the local keeper server first, then starts relay and connects
to that local keeper instance. It is useful for single-machine deployment.`,
	Run: runBoth,
}

func init() {
	rootCmd.AddCommand(bothCmd)

	bothCmd.Flags().IntVar(&keeperPort, "port", 0, "Keeper server port (default: from config or 8080)")
	bothCmd.Flags().StringVar(&relayUserID, "user-id", "", "User ID (or RELAY_USER_ID env)")
	bothCmd.Flags().StringVar(&relayPlatform, "platform", "", "Platform: feishu, slack, wechat, or wecom (or RELAY_PLATFORM env)")
	bothCmd.Flags().StringVar(&relayToken, "token", "", "Auth token for keeper connection (or RELAY_TOKEN env)")
	bothCmd.Flags().StringVar(&relayServerURL, "server", "", "Relay WebSocket URL (default: local keeper ws://127.0.0.1:<port>/ws)")
	bothCmd.Flags().StringVar(&relayWebhookURL, "webhook", "", "Relay webhook URL (default: local keeper http://127.0.0.1:<port>/webhook)")
	bothCmd.Flags().StringVar(&bothServiceAction, "service", "", serviceActionHelp)
}

func runBoth(cmd *cobra.Command, args []string) {
	if runModeServiceAction("both", bothServiceAction) {
		return
	}

	cfg, _ := config.Load()

	port := keeperPort
	if port == 0 && cfg != nil && cfg.Keeper.Port != 0 {
		port = cfg.Keeper.Port
	}
	if port == 0 {
		port = 8080
	}
	keeperPort = port

	// In both mode, default relay target is local keeper.
	if relayServerURL == "" {
		if v := os.Getenv("RELAY_SERVER_URL"); v != "" {
			relayServerURL = v
		}
	}
	if relayServerURL == "" {
		relayServerURL = fmt.Sprintf("ws://127.0.0.1:%d/ws", port)
	}

	if relayWebhookURL == "" {
		if v := os.Getenv("RELAY_WEBHOOK_URL"); v != "" {
			relayWebhookURL = v
		}
	}
	if relayWebhookURL == "" {
		relayWebhookURL = fmt.Sprintf("http://127.0.0.1:%d/webhook", port)
	}

	// Prefer explicit relay token, then config relay token, then keeper token.
	if relayToken == "" {
		relayToken = os.Getenv("RELAY_TOKEN")
	}
	if relayToken == "" && cfg != nil && cfg.Relay.Token != "" {
		relayToken = cfg.Relay.Token
	}
	if relayToken == "" && cfg != nil && cfg.Keeper.Token != "" {
		relayToken = cfg.Keeper.Token
	}

	// Keep defaults compatible with local keeper + wecom usage.
	if relayPlatform == "" {
		relayPlatform = os.Getenv("RELAY_PLATFORM")
	}
	if relayPlatform == "" && cfg != nil && cfg.Relay.Platform != "" {
		relayPlatform = cfg.Relay.Platform
	}
	if relayPlatform == "" && cfg != nil && cfg.Keeper.WeComCorpID != "" {
		relayPlatform = "wecom"
	}

	if relayUserID == "" {
		relayUserID = os.Getenv("RELAY_USER_ID")
	}
	if relayUserID == "" && cfg != nil && cfg.Relay.UserID != "" {
		relayUserID = cfg.Relay.UserID
	}
	if relayUserID == "" && relayPlatform == "wecom" {
		corpID := relayWeComCorpID
		if corpID == "" {
			corpID = os.Getenv("WECOM_CORP_ID")
		}
		if corpID == "" && cfg != nil && cfg.Platforms.WeCom.CorpID != "" {
			corpID = cfg.Platforms.WeCom.CorpID
		}
		if corpID == "" && cfg != nil && cfg.Keeper.WeComCorpID != "" {
			corpID = cfg.Keeper.WeComCorpID
		}
		if corpID != "" {
			relayUserID = "wecom-" + corpID
		}
	}

	go runKeeper(cmd, args)

	if err := waitLocalKeeperReady(port, 15*time.Second); err != nil {
		fmt.Fprintf(os.Stderr, "Error: keeper did not become ready: %v\n", err)
		os.Exit(1)
	}

	runRelay(cmd, args)
}

func waitLocalKeeperReady(port int, timeout time.Duration) error {
	healthURL := fmt.Sprintf("http://127.0.0.1:%d/health", port)
	client := &http.Client{Timeout: 1 * time.Second}
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		resp, err := client.Get(healthURL)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(300 * time.Millisecond)
	}

	return fmt.Errorf("timeout waiting for %s", healthURL)
}
