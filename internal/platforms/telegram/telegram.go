package telegram

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/kayz/coco/internal/router"
)

// VoiceTranscriber transcribes voice messages to text
type VoiceTranscriber interface {
	Transcribe(ctx context.Context, audio []byte) (string, error)
}

// Platform implements router.Platform for Telegram
type Platform struct {
	bot            *tgbotapi.BotAPI
	messageHandler func(msg router.Message)
	transcriber    VoiceTranscriber
	ctx            context.Context
	cancel         context.CancelFunc
}

// Config holds Telegram configuration
type Config struct {
	Token       string           // Bot token from @BotFather
	Debug       bool             // Enable debug logging
	Transcriber VoiceTranscriber // Optional voice transcriber for voice messages
}

// New creates a new Telegram platform
func New(cfg Config) (*Platform, error) {
	if cfg.Token == "" {
		return nil, fmt.Errorf("Telegram bot token is required")
	}

	bot, err := tgbotapi.NewBotAPI(cfg.Token)
	if err != nil {
		return nil, fmt.Errorf("failed to create Telegram bot: %w", err)
	}

	bot.Debug = cfg.Debug

	return &Platform{
		bot:         bot,
		transcriber: cfg.Transcriber,
	}, nil
}

// Name returns the platform name
func (p *Platform) Name() string {
	return "telegram"
}

// SetMessageHandler sets the callback for incoming messages
func (p *Platform) SetMessageHandler(handler func(msg router.Message)) {
	p.messageHandler = handler
}

// Start begins listening for Telegram updates
func (p *Platform) Start(ctx context.Context) error {
	p.ctx, p.cancel = context.WithCancel(ctx)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := p.bot.GetUpdatesChan(u)

	go p.handleUpdates(updates)

	log.Printf("[Telegram] Connected as bot: @%s", p.bot.Self.UserName)
	return nil
}

// Stop shuts down the Telegram connection
func (p *Platform) Stop() error {
	if p.cancel != nil {
		p.cancel()
	}
	p.bot.StopReceivingUpdates()
	return nil
}

// Send sends a message to a Telegram chat
func (p *Platform) Send(ctx context.Context, channelID string, resp router.Response) error {
	chatID, err := parseChatID(channelID)
	if err != nil {
		return err
	}

	msg := tgbotapi.NewMessage(chatID, resp.Text)

	// Enable Markdown formatting
	msg.ParseMode = "Markdown"

	// Reply to specific message if ThreadID is set
	if resp.ThreadID != "" {
		if msgID, err := parseMessageID(resp.ThreadID); err == nil {
			msg.ReplyToMessageID = msgID
		}
	}

	_, err = p.bot.Send(msg)
	return err
}

// handleUpdates processes incoming Telegram updates
func (p *Platform) handleUpdates(updates tgbotapi.UpdatesChannel) {
	for {
		select {
		case <-p.ctx.Done():
			return
		case update := <-updates:
			if update.Message == nil {
				continue
			}

			// Skip messages from bots
			if update.Message.From.IsBot {
				continue
			}

			// Check if we should respond
			if !p.shouldRespond(update.Message) {
				continue
			}

			var text string
			var isVoice bool

			// Handle voice messages
			if update.Message.Voice != nil {
				if p.transcriber == nil {
					log.Printf("[Telegram] Voice message received but no transcriber configured")
					continue
				}

				// Transcribe voice message
				transcribed, err := p.transcribeVoice(update.Message.Voice.FileID)
				if err != nil {
					log.Printf("[Telegram] Failed to transcribe voice: %v", err)
					continue
				}
				text = transcribed
				isVoice = true
				log.Printf("[Telegram] Transcribed voice: %s", text)
			} else if update.Message.Audio != nil {
				// Handle audio files (sent as audio, not voice)
				if p.transcriber == nil {
					log.Printf("[Telegram] Audio message received but no transcriber configured")
					continue
				}

				transcribed, err := p.transcribeVoice(update.Message.Audio.FileID)
				if err != nil {
					log.Printf("[Telegram] Failed to transcribe audio: %v", err)
					continue
				}
				text = transcribed
				isVoice = true
				log.Printf("[Telegram] Transcribed audio: %s", text)
			} else {
				text = p.cleanMention(update.Message.Text)
			}

			if text == "" {
				continue
			}

			if p.messageHandler != nil {
				threadID := ""
				if update.Message.ReplyToMessage != nil {
					threadID = fmt.Sprintf("%d", update.Message.ReplyToMessage.MessageID)
				}

				metadata := map[string]string{
					"chat_type": update.Message.Chat.Type,
					"mentioned": strconv.FormatBool(isTelegramMentioned(update.Message, p.bot.Self.UserName, p.bot.Self.ID)),
				}
				if isVoice {
					metadata["message_type"] = "voice"
				}

				p.messageHandler(router.Message{
					ID:        fmt.Sprintf("%d", update.Message.MessageID),
					Platform:  "telegram",
					ChannelID: fmt.Sprintf("%d", update.Message.Chat.ID),
					UserID:    fmt.Sprintf("%d", update.Message.From.ID),
					Username:  getUsername(update.Message.From),
					Text:      text,
					ThreadID:  threadID,
					Metadata:  metadata,
				})
			}
		}
	}
}

// transcribeVoice downloads and transcribes a voice message
func (p *Platform) transcribeVoice(fileID string) (string, error) {
	// Get file info from Telegram
	file, err := p.bot.GetFile(tgbotapi.FileConfig{FileID: fileID})
	if err != nil {
		return "", fmt.Errorf("failed to get file info: %w", err)
	}

	// Download the file
	fileURL := file.Link(p.bot.Token)
	resp, err := http.Get(fileURL)
	if err != nil {
		return "", fmt.Errorf("failed to download file: %w", err)
	}
	defer resp.Body.Close()

	audio, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read file: %w", err)
	}

	// Transcribe using the configured transcriber
	return p.transcriber.Transcribe(p.ctx, audio)
}

// shouldRespond checks if the bot should respond to this message
func (p *Platform) shouldRespond(msg *tgbotapi.Message) bool {
	// Always respond in private chats
	if msg.Chat.IsPrivate() {
		return true
	}

	// In groups, only respond to mentions or replies to bot
	if msg.Chat.IsGroup() || msg.Chat.IsSuperGroup() {
		// Check for @mention
		if strings.Contains(msg.Text, "@"+p.bot.Self.UserName) {
			return true
		}

		// Check if replying to bot's message
		if msg.ReplyToMessage != nil && msg.ReplyToMessage.From.ID == p.bot.Self.ID {
			return true
		}

		// Check for bot command (e.g., /ask)
		if msg.IsCommand() {
			return true
		}

		return false
	}

	return true
}

func isTelegramMentioned(msg *tgbotapi.Message, botUsername string, botID int64) bool {
	if msg == nil || msg.Chat.IsPrivate() {
		return false
	}
	if strings.Contains(msg.Text, "@"+botUsername) {
		return true
	}
	if msg.ReplyToMessage != nil && msg.ReplyToMessage.From.ID == botID {
		return true
	}
	return msg.IsCommand()
}

// cleanMention removes the bot mention from the message
func (p *Platform) cleanMention(text string) string {
	mention := "@" + p.bot.Self.UserName
	text = strings.ReplaceAll(text, mention, "")
	return strings.TrimSpace(text)
}

// getUsername returns a human-readable username
func getUsername(user *tgbotapi.User) string {
	if user.UserName != "" {
		return user.UserName
	}
	if user.FirstName != "" {
		name := user.FirstName
		if user.LastName != "" {
			name += " " + user.LastName
		}
		return name
	}
	return fmt.Sprintf("%d", user.ID)
}

// parseChatID parses a string chat ID to int64
func parseChatID(s string) (int64, error) {
	var id int64
	_, err := fmt.Sscanf(s, "%d", &id)
	return id, err
}

// parseMessageID parses a string message ID to int
func parseMessageID(s string) (int, error) {
	var id int
	_, err := fmt.Sscanf(s, "%d", &id)
	return id, err
}
