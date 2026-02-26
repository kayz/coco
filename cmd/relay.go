package cmd

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/kayz/coco/internal/agent"
	"github.com/kayz/coco/internal/config"
	cronpkg "github.com/kayz/coco/internal/cron"
	"github.com/kayz/coco/internal/platforms/relay"
	"github.com/kayz/coco/internal/router"
	"github.com/kayz/coco/internal/tools"
	"github.com/kayz/coco/internal/voice"
	"github.com/spf13/cobra"
)

var (
	relayUserID         string
	relayPlatform       string
	relayToken          string
	relayServerURL      string
	relayWebhookURL     string
	relayUseMediaProxy  bool
	relayAIProvider     string
	relayAPIKey         string
	relayBaseURL        string
	relayModel          string
	relayInstructions   string
	// WeCom credentials for cloud relay
	relayWeComCorpID  string
	relayWeComAgentID string
	relayWeComSecret  string
	relayWeComToken   string
	relayWeComAESKey  string
	// WeChat OA credentials
	relayWeChatAppID     string
	relayWeChatAppSecret string
	// Voice STT provider
	relayVoiceSTTProvider string
	relayVoiceSTTAPIKey   string
)

var relayCmd = &cobra.Command{
	Use:   "relay",
	Short: "Connect to the cloud relay service",
	Long: `Connect to the coco cloud relay service to process messages
using your local AI API key.

This allows you to use the official coco service on Feishu/Slack/WeChat
without registering your own bot application.

User Flow (Feishu/Slack/WeChat):
  1. Follow the official coco on Feishu/Slack/WeChat
  2. Send /whoami to get your user ID
  3. Run: coco relay --user-id <ID> --platform feishu
  4. Messages are processed locally with your AI API key
  5. Responses are sent back through the relay service

WeCom Cloud Relay:
  For WeCom, no user-id is needed - just provide your credentials.
  This command handles both callback verification AND message processing.

  coco relay --platform wecom \
    --wecom-corp-id YOUR_CORP_ID \
    --wecom-agent-id YOUR_AGENT_ID \
    --wecom-secret YOUR_SECRET \
    --wecom-token YOUR_TOKEN \
    --wecom-aes-key YOUR_AES_KEY \
    --provider deepseek \
    --api-key YOUR_API_KEY

  1. Run this command first
  2. Configure callback URL in WeCom: https://keeper.kayz.com/wecom
  3. Save config in WeCom - verification will succeed automatically
  4. Messages will be processed with your AI provider

Required:
  --user-id     Your user ID from /whoami (not needed for WeCom)
  --platform    Platform type: feishu, slack, wechat, or wecom
  --api-key     AI API key (or AI_API_KEY env)

WeCom Required (when platform=wecom):
  --wecom-corp-id    WeCom Corp ID (or WECOM_CORP_ID env)
  --wecom-agent-id   WeCom Agent ID (or WECOM_AGENT_ID env)
  --wecom-secret     WeCom Secret (or WECOM_SECRET env)
  --wecom-token      WeCom Callback Token (or WECOM_TOKEN env)
  --wecom-aes-key    WeCom Encoding AES Key (or WECOM_AES_KEY env)

Environment variables:
  RELAY_USER_ID        Alternative to --user-id
  RELAY_PLATFORM       Alternative to --platform
  RELAY_SERVER_URL     Custom WebSocket server URL
  RELAY_WEBHOOK_URL    Custom webhook URL
  AI_PROVIDER          AI provider: claude, deepseek, kimi, qwen (default: claude)
  AI_API_KEY           AI API key
  AI_BASE_URL          Custom API base URL
  AI_MODEL             Model name`,
	Run: runRelay,
}

func init() {
	rootCmd.AddCommand(relayCmd)

	relayCmd.Flags().StringVar(&relayUserID, "user-id", "", "User ID from /whoami (required, or RELAY_USER_ID env)")
	relayCmd.Flags().StringVar(&relayPlatform, "platform", "", "Platform: feishu, slack, wechat, or wecom (required, or RELAY_PLATFORM env)")
	relayCmd.Flags().StringVar(&relayToken, "token", "", "Auth token for Keeper connection (or RELAY_TOKEN env)")
	relayCmd.Flags().StringVar(&relayServerURL, "server", "", "WebSocket URL (default: wss://keeper.kayz.com/ws, or RELAY_SERVER_URL env)")
	relayCmd.Flags().StringVar(&relayWebhookURL, "webhook", "", "Webhook URL (default: https://keeper.kayz.com/webhook, or RELAY_WEBHOOK_URL env)")
	relayCmd.Flags().BoolVar(&relayUseMediaProxy, "use-media-proxy", false, "Proxy media download/upload through relay server")
	relayCmd.Flags().StringVar(&relayAIProvider, "provider", "", "AI provider: claude, deepseek, kimi, qwen (or AI_PROVIDER env)")
	relayCmd.Flags().StringVar(&relayAPIKey, "api-key", "", "AI API key (or AI_API_KEY env)")
	relayCmd.Flags().StringVar(&relayBaseURL, "base-url", "", "Custom API base URL (or AI_BASE_URL env)")
	relayCmd.Flags().StringVar(&relayModel, "model", "", "Model name (or AI_MODEL env)")
	relayCmd.Flags().StringVar(&relayInstructions, "instructions", "", "Path to custom instructions file appended to system prompt")

	// WeCom credentials for cloud relay
	relayCmd.Flags().StringVar(&relayWeComCorpID, "wecom-corp-id", "", "WeCom Corp ID (or WECOM_CORP_ID env)")
	relayCmd.Flags().StringVar(&relayWeComAgentID, "wecom-agent-id", "", "WeCom Agent ID (or WECOM_AGENT_ID env)")
	relayCmd.Flags().StringVar(&relayWeComSecret, "wecom-secret", "", "WeCom Secret (or WECOM_SECRET env)")
	relayCmd.Flags().StringVar(&relayWeComToken, "wecom-token", "", "WeCom Callback Token (or WECOM_TOKEN env)")
	relayCmd.Flags().StringVar(&relayWeComAESKey, "wecom-aes-key", "", "WeCom Encoding AES Key (or WECOM_AES_KEY env)")

	// WeChat OA credentials
	relayCmd.Flags().StringVar(&relayWeChatAppID, "wechat-app-id", "", "WeChat OA App ID (or WECHAT_APP_ID env)")
	relayCmd.Flags().StringVar(&relayWeChatAppSecret, "wechat-app-secret", "", "WeChat OA App Secret (or WECHAT_APP_SECRET env)")
	// Voice STT parameters
	relayCmd.Flags().StringVar(&relayVoiceSTTProvider, "voice-stt-provider", "", "Voice STT provider: system, openai (or VOICE_STT_PROVIDER env, default: system)")
	relayCmd.Flags().StringVar(&relayVoiceSTTAPIKey, "voice-stt-api-key", "", "Voice STT API key (or VOICE_STT_API_KEY env)")
}

func runRelay(cmd *cobra.Command, args []string) {
	// Get values from flags or environment
	if relayUserID == "" {
		relayUserID = os.Getenv("RELAY_USER_ID")
	}
	if relayPlatform == "" {
		relayPlatform = os.Getenv("RELAY_PLATFORM")
	}
	if relayServerURL == "" {
		relayServerURL = os.Getenv("RELAY_SERVER_URL")
	}
	if relayWebhookURL == "" {
		relayWebhookURL = os.Getenv("RELAY_WEBHOOK_URL")
	}
	if relayToken == "" {
		relayToken = os.Getenv("RELAY_TOKEN")
	}
	if !relayUseMediaProxy {
		if os.Getenv("RELAY_USE_MEDIA_PROXY") == "true" || os.Getenv("RELAY_USE_MEDIA_PROXY") == "1" {
			relayUseMediaProxy = true
		}
	}
	// Check environment variable first, then use default
	if envVal := os.Getenv("VOICE_STT_PROVIDER"); envVal != "" {
		relayVoiceSTTProvider = envVal
	} else if relayVoiceSTTProvider == "" {
		relayVoiceSTTProvider = "system"
	}
	if relayVoiceSTTAPIKey == "" {
		relayVoiceSTTAPIKey = os.Getenv("VOICE_STT_API_KEY")
	}
	if relayAIProvider == "" {
		relayAIProvider = os.Getenv("AI_PROVIDER")
	}
	if relayAPIKey == "" {
		relayAPIKey = os.Getenv("AI_API_KEY")
		// Fallback: ANTHROPIC_OAUTH_TOKEN (setup token) > ANTHROPIC_API_KEY
		if relayAPIKey == "" {
			relayAPIKey = os.Getenv("ANTHROPIC_OAUTH_TOKEN")
		}
		if relayAPIKey == "" {
			relayAPIKey = os.Getenv("ANTHROPIC_API_KEY")
		}
	}
	if relayBaseURL == "" {
		relayBaseURL = os.Getenv("AI_BASE_URL")
		if relayBaseURL == "" {
			relayBaseURL = os.Getenv("ANTHROPIC_BASE_URL")
		}
	}
	if relayModel == "" {
		relayModel = os.Getenv("AI_MODEL")
		if relayModel == "" {
			relayModel = os.Getenv("ANTHROPIC_MODEL")
		}
	}

	// Get WeCom credentials from flags or environment
	if relayWeComCorpID == "" {
		relayWeComCorpID = os.Getenv("WECOM_CORP_ID")
	}
	if relayWeComAgentID == "" {
		relayWeComAgentID = os.Getenv("WECOM_AGENT_ID")
	}
	if relayWeComSecret == "" {
		relayWeComSecret = os.Getenv("WECOM_SECRET")
	}
	if relayWeComToken == "" {
		relayWeComToken = os.Getenv("WECOM_TOKEN")
	}
	if relayWeComAESKey == "" {
		relayWeComAESKey = os.Getenv("WECOM_AES_KEY")
	}

	// Get WeChat OA credentials from flags or environment
	if relayWeChatAppID == "" {
		relayWeChatAppID = os.Getenv("WECHAT_APP_ID")
	}
	if relayWeChatAppSecret == "" {
		relayWeChatAppSecret = os.Getenv("WECHAT_APP_SECRET")
	}

	// Fallback to saved config file
	if savedCfg, err := config.Load(); err == nil {
		if relayAIProvider == "" {
			relayAIProvider = savedCfg.AI.Provider
		}
		if relayAPIKey == "" {
			relayAPIKey = savedCfg.AI.APIKey
		}
		if relayBaseURL == "" {
			relayBaseURL = savedCfg.AI.BaseURL
		}
		if relayModel == "" {
			relayModel = savedCfg.AI.Model
		}
		// Read relay-specific config (platform, user-id, server, webhook, media-proxy) from saved config
		if relayPlatform == "" && savedCfg.Relay.Platform != "" {
			relayPlatform = savedCfg.Relay.Platform
		}
		if relayUserID == "" && savedCfg.Relay.UserID != "" {
			relayUserID = savedCfg.Relay.UserID
		}
		if relayServerURL == "" && savedCfg.Relay.ServerURL != "" {
			relayServerURL = savedCfg.Relay.ServerURL
		}
		if relayWebhookURL == "" && savedCfg.Relay.WebhookURL != "" {
			relayWebhookURL = savedCfg.Relay.WebhookURL
		}
		if relayToken == "" && savedCfg.Relay.Token != "" {
			relayToken = savedCfg.Relay.Token
		}
		if !relayUseMediaProxy && savedCfg.Relay.UseMediaProxy {
			relayUseMediaProxy = savedCfg.Relay.UseMediaProxy
		}
		if relayPlatform == "" && savedCfg.Mode == "relay" {
			// Infer platform from saved platform credentials
			if savedCfg.Platforms.WeCom.CorpID != "" {
				relayPlatform = "wecom"
			}
		}
		if relayWeComCorpID == "" {
			relayWeComCorpID = savedCfg.Platforms.WeCom.CorpID
		}
		if relayWeComAgentID == "" {
			relayWeComAgentID = savedCfg.Platforms.WeCom.AgentID
		}
		if relayWeComSecret == "" {
			relayWeComSecret = savedCfg.Platforms.WeCom.Secret
		}
		if relayWeComToken == "" {
			relayWeComToken = savedCfg.Platforms.WeCom.Token
		}
		if relayWeComAESKey == "" {
			relayWeComAESKey = savedCfg.Platforms.WeCom.AESKey
		}
		if relayWeChatAppID == "" {
			relayWeChatAppID = savedCfg.Platforms.WeChat.AppID
		}
		if relayWeChatAppSecret == "" {
			relayWeChatAppSecret = savedCfg.Platforms.WeChat.AppSecret
		}
	}

	// Validate required parameters
	if relayPlatform == "" {
		fmt.Fprintln(os.Stderr, "Error: --platform is required (feishu, slack, wechat, or wecom)")
		os.Exit(1)
	}
	if relayPlatform != "feishu" && relayPlatform != "slack" && relayPlatform != "wechat" && relayPlatform != "wecom" {
		fmt.Fprintln(os.Stderr, "Error: --platform must be 'feishu', 'slack', 'wechat', or 'wecom'")
		os.Exit(1)
	}
	if relayAPIKey == "" {
		fmt.Fprintln(os.Stderr, "Error: AI API key is required (--api-key or AI_API_KEY env)")
		os.Exit(1)
	}

	// For WeCom, user-id is optional - auto-generate from corp_id
	// For other platforms, user-id is required
	if relayUserID == "" {
		if relayPlatform == "wecom" && relayWeComCorpID != "" {
			relayUserID = "wecom-" + relayWeComCorpID
		} else if relayPlatform != "wecom" {
			fmt.Fprintln(os.Stderr, "Error: --user-id is required (get it from /whoami)")
			os.Exit(1)
		}
	}

	// Validate WeCom credentials when platform is wecom
	// Skip validation when connecting to a self-hosted Keeper (non-default server_url),
	// because the Keeper holds the WeCom credentials, not the coco client.
	isDefaultRelay := relayServerURL == "" || relayServerURL == "wss://keeper.kayz.com/ws"
	if relayPlatform == "wecom" && isDefaultRelay {
		missing := []string{}
		if relayWeComCorpID == "" {
			missing = append(missing, "--wecom-corp-id")
		}
		if relayWeComAgentID == "" {
			missing = append(missing, "--wecom-agent-id")
		}
		if relayWeComSecret == "" {
			missing = append(missing, "--wecom-secret")
		}
		if relayWeComToken == "" {
			missing = append(missing, "--wecom-token")
		}
		if relayWeComAESKey == "" {
			missing = append(missing, "--wecom-aes-key")
		}
		if len(missing) > 0 {
			fmt.Fprintf(os.Stderr, "Error: WeCom credentials required for cloud relay: %v\n", missing)
			fmt.Fprintln(os.Stderr, "Configure callback URL in WeCom: https://keeper.kayz.com/wecom")
			os.Exit(1)
		}
	}

	// Load custom instructions if specified
	var customInstructions string
	if relayInstructions != "" {
		data, err := os.ReadFile(relayInstructions)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading instructions file: %v\n", err)
			os.Exit(1)
		}
		customInstructions = string(data)
		log.Printf("Loaded custom instructions from %s (%d bytes)", relayInstructions, len(data))
	}

	// Create the AI agent
	aiAgent, err := agent.New(agent.Config{
		Provider:           relayAIProvider,
		APIKey:             relayAPIKey,
		BaseURL:            relayBaseURL,
		Model:              relayModel,
		CustomInstructions: customInstructions,
		AllowedPaths:       loadAllowedPaths(),
		DisableFileTools:   loadDisableFileTools(),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating agent: %v\n", err)
		os.Exit(1)
	}

	// Resolve provider and model names
	providerName := relayAIProvider
	if providerName == "" {
		providerName = "claude"
	}
	modelName := relayModel
	if modelName == "" {
		switch providerName {
		case "deepseek":
			modelName = "deepseek-chat"
		case "kimi", "moonshot":
			modelName = "moonshot-v1-8k"
		default:
			modelName = "claude-sonnet-4-20250514"
		}
	}

	// Create the router with the agent as message handler
	r := router.New(aiAgent.HandleMessage)

	// Initialize cron scheduler
	exeDir := tools.GetExecutableDir()
	if exeDir == "" {
		exeDir = os.TempDir()
	}
	cronPath := filepath.Join(exeDir, ".coco.db")
	cronStore, err := cronpkg.NewStore(cronPath)
	if err != nil {
		log.Fatalf("Failed to open cron store: %v", err)
	}
	cronNotifier := agent.NewRouterCronNotifier(r)
	cronScheduler := cronpkg.NewScheduler(cronStore, aiAgent, aiAgent, cronNotifier)
	aiAgent.SetCronScheduler(cronScheduler)
	if err := cronScheduler.Start(); err != nil {
		log.Printf("Warning: Failed to start cron scheduler: %v", err)
	}

	// Create voice transcriber if STT provider is configured
	var transcriber *voice.Transcriber
	if relayVoiceSTTProvider != "" {
		var err error
		transcriber, err = voice.NewTranscriber(voice.TranscriberConfig{
			Provider: relayVoiceSTTProvider,
			APIKey:   relayVoiceSTTAPIKey,
		})
		if err != nil {
			log.Printf("Warning: Failed to create voice transcriber: %v", err)
		} else {
			log.Printf("Voice transcription enabled (provider: %s)", relayVoiceSTTProvider)
		}
	}

	// Create and register relay platform
	relayPlatformInstance, err := relay.New(relay.Config{
		UserID:           relayUserID,
		Platform:         relayPlatform,
		Token:            relayToken,
		ServerURL:        relayServerURL,
		WebhookURL:       relayWebhookURL,
		UseMediaProxy:    relayUseMediaProxy,
		AIProvider:       providerName,
		AIModel:          modelName,
		WeComCorpID:      relayWeComCorpID,
		WeComAgentID:     relayWeComAgentID,
		WeComSecret:      relayWeComSecret,
		WeComToken:       relayWeComToken,
		WeComAESKey:      relayWeComAESKey,
		WeChatAppID:      relayWeChatAppID,
		WeChatAppSecret:  relayWeChatAppSecret,
		Transcriber:      transcriber,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating relay platform: %v\n", err)
		os.Exit(1)
	}
	r.Register(relayPlatformInstance)

	// Start the router
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := r.Start(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Error starting relay: %v\n", err)
		os.Exit(1)
	}

	log.Printf("Relay connected. User: %s, Platform: %s", relayUserID, relayPlatform)
	log.Printf("AI Provider: %s, Model: %s", providerName, modelName)
	log.Println("Press Ctrl+C to stop.")

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	log.Println("Shutting down...")
	cronScheduler.Stop()
	r.Stop()
}
