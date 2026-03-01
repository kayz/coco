package agent

import (
	"context"
	"fmt"

	"github.com/sashabaranov/go-openai"
)

const (
	qwenDefaultBaseURL = "https://dashscope.aliyuncs.com/compatible-mode/v1"
	qwenDefaultModel   = "qwen-plus"
)

// QwenProvider implements the Provider interface for Qianwen (通义千问)
type QwenProvider struct {
	client *openai.Client
	model  string
}

// QwenConfig holds Qwen provider configuration
type QwenConfig struct {
	APIKey  string
	BaseURL string
	Model   string
}

// NewQwenProvider creates a new Qwen provider
func NewQwenProvider(cfg QwenConfig) (*QwenProvider, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("API key is required")
	}

	if cfg.Model == "" {
		cfg.Model = qwenDefaultModel
	}

	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = qwenDefaultBaseURL
	}

	config := openai.DefaultConfig(cfg.APIKey)
	config.BaseURL = baseURL

	return &QwenProvider{
		client: openai.NewClientWithConfig(config),
		model:  cfg.Model,
	}, nil
}

// Name returns the provider name
func (p *QwenProvider) Name() string {
	return "qwen"
}

// Chat sends messages and returns a response
func (p *QwenProvider) Chat(ctx context.Context, req ChatRequest) (ChatResponse, error) {
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

	// Call Qwen API
	resp, err := p.client.CreateChatCompletion(ctx, chatReq)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("qwen API error: %w", err)
	}

	return genericResponseFromOpenAI(resp, codec), nil
}
