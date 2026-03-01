package agent

import (
	"context"
	"fmt"

	"github.com/sashabaranov/go-openai"
)

const (
	deepseekDefaultBaseURL = "https://api.deepseek.com/v1"
	deepseekDefaultModel   = "deepseek-chat"
)

// DeepSeekProvider implements the Provider interface for DeepSeek
type DeepSeekProvider struct {
	client *openai.Client
	model  string
}

// DeepSeekConfig holds DeepSeek provider configuration
type DeepSeekConfig struct {
	APIKey  string
	BaseURL string
	Model   string
}

// NewDeepSeekProvider creates a new DeepSeek provider
func NewDeepSeekProvider(cfg DeepSeekConfig) (*DeepSeekProvider, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("API key is required")
	}

	if cfg.Model == "" {
		cfg.Model = deepseekDefaultModel
	}

	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = deepseekDefaultBaseURL
	}

	config := openai.DefaultConfig(cfg.APIKey)
	config.BaseURL = baseURL

	return &DeepSeekProvider{
		client: openai.NewClientWithConfig(config),
		model:  cfg.Model,
	}, nil
}

// Name returns the provider name
func (p *DeepSeekProvider) Name() string {
	return "deepseek"
}

// Chat sends messages and returns a response
func (p *DeepSeekProvider) Chat(ctx context.Context, req ChatRequest) (ChatResponse, error) {
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

	// Call DeepSeek API
	resp, err := p.client.CreateChatCompletion(ctx, chatReq)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("deepseek API error: %w", err)
	}

	return genericResponseFromOpenAI(resp, codec), nil
}
