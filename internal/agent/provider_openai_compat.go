package agent

import (
	"context"
	"fmt"

	"github.com/sashabaranov/go-openai"
)

// OpenAICompatProvider implements the Provider interface for any OpenAI-compatible API.
// This covers: MiniMax, Doubao, Zhipu/GLM, OpenAI/GPT, Gemini, Yi, StepFun, SiliconFlow, Grok, etc.
type OpenAICompatProvider struct {
	client       *openai.Client
	model        string
	providerName string
}

// OpenAICompatConfig holds configuration for an OpenAI-compatible provider
type OpenAICompatConfig struct {
	ProviderName string // Display name (e.g., "minimax", "openai")
	APIKey       string
	BaseURL      string
	Model        string
	DefaultURL   string // Default base URL if not specified
	DefaultModel string // Default model if not specified
}

// NewOpenAICompatProvider creates a new OpenAI-compatible provider
func NewOpenAICompatProvider(cfg OpenAICompatConfig) (*OpenAICompatProvider, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("API key is required")
	}

	if cfg.Model == "" {
		cfg.Model = cfg.DefaultModel
	}

	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = cfg.DefaultURL
	}

	config := openai.DefaultConfig(cfg.APIKey)
	config.BaseURL = baseURL

	return &OpenAICompatProvider{
		client:       openai.NewClientWithConfig(config),
		model:        cfg.Model,
		providerName: cfg.ProviderName,
	}, nil
}

// Name returns the provider name
func (p *OpenAICompatProvider) Name() string {
	return p.providerName
}

// Chat sends messages and returns a response
func (p *OpenAICompatProvider) Chat(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	codec := newOpenAIToolCodec(req.Tools)

	messages := make([]openai.ChatCompletionMessage, 0, len(req.Messages)+1)

	if req.SystemPrompt != "" {
		messages = append(messages, openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleSystem,
			Content: req.SystemPrompt,
		})
	}

	for _, msg := range req.Messages {
		messages = append(messages, openAIMessageFromGeneric(msg, codec))
	}

	tools := openAIToolsFromGeneric(req.Tools, codec)

	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 4096
	}

	chatReq := openai.ChatCompletionRequest{
		Model:     p.model,
		Messages:  messages,
		MaxTokens: maxTokens,
	}
	if len(tools) > 0 {
		chatReq.Tools = tools
	}

	resp, err := p.client.CreateChatCompletion(ctx, chatReq)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("%s API error: %w", p.providerName, err)
	}

	return genericResponseFromOpenAI(resp, codec), nil
}
