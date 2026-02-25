package config

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
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

type Config struct {
	Transport string          `yaml:"transport"` // "stdio" or "sse"
	Port      int             `yaml:"port"`
	Security  SecurityConfig  `yaml:"security"`
	Logging   LoggingConfig   `yaml:"logging"`
	AI        AIConfig        `yaml:"ai,omitempty"`
	Embedding EmbeddingConfig `yaml:"embedding,omitempty"`
	Platforms PlatformConfig  `yaml:"platforms,omitempty"`
	Mode      string          `yaml:"mode,omitempty"` // "relay" or "router"
	Relay     RelayConfig     `yaml:"relay,omitempty"`
	Skills    SkillsConfig    `yaml:"skills,omitempty"`
	Browser   BrowserConfig   `yaml:"browser,omitempty"`
	Search    SearchConfig    `yaml:"search,omitempty"`
	Keeper    KeeperConfig    `yaml:"keeper,omitempty"`
}

// KeeperConfig holds configuration for Keeper mode (public server).
type KeeperConfig struct {
	Port         int    `yaml:"port,omitempty"`           // HTTP listen port, default 8080
	Token        string `yaml:"token,omitempty"`          // Auth token for coco connections
	WeComCorpID  string `yaml:"wecom_corp_id,omitempty"`
	WeComAgentID string `yaml:"wecom_agent_id,omitempty"`
	WeComSecret  string `yaml:"wecom_secret,omitempty"`
	WeComToken   string `yaml:"wecom_token,omitempty"`    // WeCom callback verification token
	WeComAESKey  string `yaml:"wecom_aes_key,omitempty"`  // WeCom callback EncodingAESKey
}

// SearchEngineConfig 单个搜索引擎配置
type SearchEngineConfig struct {
	Name       string                 `yaml:"name"`
	Type       string                 `yaml:"type"`
	APIKey     string                 `yaml:"api_key,omitempty"`
	BaseURL    string                 `yaml:"base_url,omitempty"`
	Enabled    bool                   `yaml:"enabled"`
	Priority   int                    `yaml:"priority"`
	Options    map[string]interface{} `yaml:"options,omitempty"`
}

// SearchConfig 搜索引擎整体配置
type SearchConfig struct {
	PrimaryEngine   string               `yaml:"primary_engine"`
	SecondaryEngine string               `yaml:"secondary_engine"`
	Engines         []SearchEngineConfig `yaml:"engines"`
	AutoSearch      bool                 `yaml:"auto_search"`
}

// BrowserConfig configures browser automation.
type BrowserConfig struct {
	// ScreenSize controls the browser window size.
	// Use "fullscreen" for fullscreen mode, or "WIDTHxHEIGHT" (e.g. "1024x768").
	// Default: "fullscreen"
	ScreenSize string `yaml:"screen_size,omitempty"`
}

type RelayConfig struct {
	UserID        string `yaml:"user_id,omitempty"`
	Platform      string `yaml:"platform,omitempty"` // "feishu", "slack", "wechat", "wecom"
	Token         string `yaml:"token,omitempty"`    // Auth token for Keeper connection
	ServerURL     string `yaml:"server_url,omitempty"` // Custom relay server WebSocket URL
	WebhookURL    string `yaml:"webhook_url,omitempty"` // Custom relay server webhook URL
	UseMediaProxy bool   `yaml:"use_media_proxy,omitempty"` // Proxy media download/upload through relay server
}

type SkillsConfig struct {
	Disabled  []string `yaml:"disabled,omitempty"`
	ExtraDirs []string `yaml:"extra_dirs,omitempty"`
}

// SkillsDir returns the managed skills directory path
func SkillsDir() string {
	exeDir := getExecutableDir()
	return filepath.Join(exeDir, ".coco", "skills")
}

type AIConfig struct {
	Provider string          `yaml:"provider,omitempty"`
	APIKey   string          `yaml:"api_key,omitempty"`
	BaseURL  string          `yaml:"base_url,omitempty"`
	Model    string          `yaml:"model,omitempty"`
	Models   []ModelConfig   `yaml:"models,omitempty"`
}

type ModelConfig struct {
	Provider string `yaml:"provider,omitempty"`
	APIKey   string `yaml:"api_key,omitempty"`
	BaseURL  string `yaml:"base_url,omitempty"`
	Model    string `yaml:"model,omitempty"`
	Priority int    `yaml:"priority,omitempty"`
	Enabled  bool   `yaml:"enabled,omitempty"`
}

type EmbeddingConfig struct {
	Provider string `yaml:"provider,omitempty"`
	APIKey   string `yaml:"api_key,omitempty"`
	BaseURL  string `yaml:"base_url,omitempty"`
	Model    string `yaml:"model,omitempty"`
	Enabled  bool   `yaml:"enabled,omitempty"`
}

type PlatformConfig struct {
	WeCom    WeComConfig    `yaml:"wecom,omitempty"`
	Slack    SlackConfig    `yaml:"slack,omitempty"`
	Telegram TelegramConfig `yaml:"telegram,omitempty"`
	Discord  DiscordConfig  `yaml:"discord,omitempty"`
	WeChat   WeChatConfig   `yaml:"wechat,omitempty"`
	Feishu   FeishuConfig   `yaml:"feishu,omitempty"`
	DingTalk DingTalkConfig `yaml:"dingtalk,omitempty"`
	WhatsApp WhatsAppConfig `yaml:"whatsapp,omitempty"`
	LINE     LINEConfig     `yaml:"line,omitempty"`
	Teams    TeamsConfig    `yaml:"teams,omitempty"`
	Matrix   MatrixConfig   `yaml:"matrix,omitempty"`
	GoogleChat GoogleChatConfig `yaml:"googlechat,omitempty"`
	Mattermost MattermostConfig `yaml:"mattermost,omitempty"`
	IMessage   IMessageConfig   `yaml:"imessage,omitempty"`
	Signal     SignalConfig     `yaml:"signal,omitempty"`
	Twitch     TwitchConfig     `yaml:"twitch,omitempty"`
	NOSTR      NOSTRConfig      `yaml:"nostr,omitempty"`
	Zalo       ZaloConfig       `yaml:"zalo,omitempty"`
	Nextcloud  NextcloudConfig  `yaml:"nextcloud,omitempty"`
}

type WeComConfig struct {
	CorpID       string `yaml:"corp_id,omitempty"`
	AgentID      string `yaml:"agent_id,omitempty"`
	Secret       string `yaml:"secret,omitempty"`
	Token        string `yaml:"token,omitempty"`
	AESKey       string `yaml:"aes_key,omitempty"`
	CallbackPort int    `yaml:"callback_port,omitempty"`
}

type SlackConfig struct {
	BotToken string `yaml:"bot_token,omitempty"`
	AppToken string `yaml:"app_token,omitempty"`
}

type TelegramConfig struct {
	Token string `yaml:"token,omitempty"`
}

type DiscordConfig struct {
	Token string `yaml:"token,omitempty"`
}

type WeChatConfig struct {
	AppID     string `yaml:"app_id,omitempty"`
	AppSecret string `yaml:"app_secret,omitempty"`
}

type FeishuConfig struct {
	AppID     string `yaml:"app_id,omitempty"`
	AppSecret string `yaml:"app_secret,omitempty"`
}

type DingTalkConfig struct {
	ClientID     string `yaml:"client_id,omitempty"`
	ClientSecret string `yaml:"client_secret,omitempty"`
}

type WhatsAppConfig struct {
	PhoneNumberID string `yaml:"phone_number_id,omitempty"`
	AccessToken   string `yaml:"access_token,omitempty"`
	VerifyToken   string `yaml:"verify_token,omitempty"`
}

type LINEConfig struct {
	ChannelSecret string `yaml:"channel_secret,omitempty"`
	ChannelToken  string `yaml:"channel_token,omitempty"`
}

type TeamsConfig struct {
	AppID       string `yaml:"app_id,omitempty"`
	AppPassword string `yaml:"app_password,omitempty"`
	TenantID    string `yaml:"tenant_id,omitempty"`
}

type MatrixConfig struct {
	HomeserverURL string `yaml:"homeserver_url,omitempty"`
	UserID        string `yaml:"user_id,omitempty"`
	AccessToken   string `yaml:"access_token,omitempty"`
}

type GoogleChatConfig struct {
	ProjectID       string `yaml:"project_id,omitempty"`
	CredentialsFile string `yaml:"credentials_file,omitempty"`
}

type MattermostConfig struct {
	ServerURL string `yaml:"server_url,omitempty"`
	Token     string `yaml:"token,omitempty"`
	TeamName  string `yaml:"team_name,omitempty"`
}

type IMessageConfig struct {
	BlueBubblesURL      string `yaml:"bluebubbles_url,omitempty"`
	BlueBubblesPassword string `yaml:"bluebubbles_password,omitempty"`
}

type SignalConfig struct {
	APIURL      string `yaml:"api_url,omitempty"`
	PhoneNumber string `yaml:"phone_number,omitempty"`
}

type TwitchConfig struct {
	Token   string `yaml:"token,omitempty"`
	Channel string `yaml:"channel,omitempty"`
	BotName string `yaml:"bot_name,omitempty"`
}

type NOSTRConfig struct {
	PrivateKey string `yaml:"private_key,omitempty"`
	Relays     string `yaml:"relays,omitempty"`
}

type ZaloConfig struct {
	AppID       string `yaml:"app_id,omitempty"`
	SecretKey   string `yaml:"secret_key,omitempty"`
	AccessToken string `yaml:"access_token,omitempty"`
}

type NextcloudConfig struct {
	ServerURL string `yaml:"server_url,omitempty"`
	Username  string `yaml:"username,omitempty"`
	Password  string `yaml:"password,omitempty"`
	RoomToken string `yaml:"room_token,omitempty"`
}

type SecurityConfig struct {
	AllowedPaths        []string `yaml:"allowed_paths"`
	BlockedCommands     []string `yaml:"blocked_commands"`
	RequireConfirmation []string `yaml:"require_confirmation"`
	DisableFileTools    bool     `yaml:"disable_file_tools"`
}

type LoggingConfig struct {
	Level string `yaml:"level"`
	File  string `yaml:"file"`
}

func DefaultConfig() *Config {
	return &Config{
		Transport: "stdio",
		Port:      8686,
		Security: SecurityConfig{
			AllowedPaths:        []string{},
			BlockedCommands:     []string{"rm -rf /", "mkfs", "dd if="},
			RequireConfirmation: []string{},
		},
		Logging: LoggingConfig{
			Level: "info",
			File:  "/tmp/coco.log",
		},
		AI: AIConfig{},
		Embedding: EmbeddingConfig{
			Provider: "qwen",
			Model:    "text-embedding-v3",
			Enabled:  false,
		},
		Search: SearchConfig{
			PrimaryEngine:   "metaso",
			SecondaryEngine: "tavily",
			AutoSearch:      true,
			Engines: []SearchEngineConfig{
				{
					Name:     "metaso",
					Type:     "metaso",
					Enabled:  true,
					Priority: 1,
				},
				{
					Name:     "tavily",
					Type:     "tavily",
					Enabled:  true,
					Priority: 2,
				},
			},
		},
		Relay: RelayConfig{
			ServerURL:     "wss://keeper.kayz.com/ws",
			WebhookURL:    "https://keeper.kayz.com/webhook",
			UseMediaProxy: true,
		},
	}
}

func ConfigDir() string {
	exeDir := getExecutableDir()
	return filepath.Join(exeDir, ".coco")
}

func ConfigPath() string {
	exeDir := getExecutableDir()
	return filepath.Join(exeDir, ".coco.yaml")
}

func Load() (*Config, error) {
	cfg := DefaultConfig()

	data, err := os.ReadFile(ConfigPath())
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, err
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

func (c *Config) Save() error {
	dir := ConfigDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}

	return os.WriteFile(ConfigPath(), data, 0600)
}
