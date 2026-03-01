package agent

import (
	"context"
	"fmt"

	"github.com/sashabaranov/go-openai"
)

const (
	kimiDefaultBaseURL = "https://api.moonshot.cn/v1"
	kimiDefaultModel   = "moonshot-v1-8k"
)

// KimiProvider implements the Provider interface for Kimi (Moonshot AI)
type KimiProvider struct {
	client *openai.Client
	model  string
}

// KimiConfig holds Kimi provider configuration
type KimiConfig struct {
	APIKey  string
	BaseURL string
	Model   string
}

// NewKimiProvider creates a new Kimi provider
func NewKimiProvider(cfg KimiConfig) (*KimiProvider, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("API key is required")
	}

	if cfg.Model == "" {
		cfg.Model = kimiDefaultModel
	}

	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = kimiDefaultBaseURL
	}

	config := openai.DefaultConfig(cfg.APIKey)
	config.BaseURL = baseURL

	return &KimiProvider{
		client: openai.NewClientWithConfig(config),
		model:  cfg.Model,
	}, nil
}

// Name returns the provider name
func (p *KimiProvider) Name() string {
	return "kimi"
}

// Chat sends messages and returns a response
func (p *KimiProvider) Chat(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	codec := newOpenAIToolCodec(req.Tools)

	// Convert messages to OpenAI format
	messages := make([]openai.ChatCompletionMessage, 0, len(req.Messages)+1)

	// Add system message
	if req.SystemPrompt != "" {
		messages = append(messages, openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleSystem,
			Content: req.SystemPrompt,
		})
	}

	// Add conversation messages
	for _, msg := range req.Messages {
		messages = append(messages, openAIMessageFromGeneric(msg, codec))
	}

	// Convert tools to OpenAI format
	tools := openAIToolsFromGeneric(req.Tools, codec)

	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 4096
	}

	// Build request
	chatReq := openai.ChatCompletionRequest{
		Model:     p.model,
		Messages:  messages,
		MaxTokens: maxTokens,
	}
	if len(tools) > 0 {
		chatReq.Tools = tools
	}

	// Call Kimi API
	resp, err := p.client.CreateChatCompletion(ctx, chatReq)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("kimi API error: %w", err)
	}

	return genericResponseFromOpenAI(resp, codec), nil
}
