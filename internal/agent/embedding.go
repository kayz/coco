package agent

import (
	"context"
	"fmt"

	"github.com/sashabaranov/go-openai"
)

// EmbeddingProvider defines the interface for embedding backends
type EmbeddingProvider interface {
	// CreateEmbedding creates embeddings for the given texts
	CreateEmbedding(ctx context.Context, texts []string) ([][]float32, error)

	// Name returns the provider name (e.g., "qwen", "openai")
	Name() string

	// Dimension returns the embedding vector dimension
	Dimension() int
}

// EmbeddingConfig holds embedding provider configuration
type EmbeddingConfig struct {
	Provider string
	APIKey   string
	BaseURL  string
	Model    string
}

// NewEmbeddingProvider creates a new embedding provider based on configuration
func NewEmbeddingProvider(cfg EmbeddingConfig) (EmbeddingProvider, error) {
	switch cfg.Provider {
	case "qwen":
		return NewQwenEmbeddingProvider(cfg)
	case "openai":
		return NewOpenAIEmbeddingProvider(cfg)
	default:
		return nil, fmt.Errorf("unsupported embedding provider: %s", cfg.Provider)
	}
}

const (
	qwenEmbeddingDefaultBaseURL = "https://dashscope.aliyuncs.com/compatible-mode/v1"
	qwenEmbeddingDefaultModel   = "text-embedding-v3"
	qwenEmbeddingDimension      = 1536
)

// QwenEmbeddingProvider implements EmbeddingProvider for Qianwen
type QwenEmbeddingProvider struct {
	client    *openai.Client
	model     string
	dimension int
}

// NewQwenEmbeddingProvider creates a new Qwen embedding provider
func NewQwenEmbeddingProvider(cfg EmbeddingConfig) (*QwenEmbeddingProvider, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("API key is required for Qwen embedding")
	}

	model := cfg.Model
	if model == "" {
		model = qwenEmbeddingDefaultModel
	}

	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = qwenEmbeddingDefaultBaseURL
	}

	config := openai.DefaultConfig(cfg.APIKey)
	config.BaseURL = baseURL

	return &QwenEmbeddingProvider{
		client:    openai.NewClientWithConfig(config),
		model:     model,
		dimension: qwenEmbeddingDimension,
	}, nil
}

// Name returns the provider name
func (p *QwenEmbeddingProvider) Name() string {
	return "qwen"
}

// Dimension returns the embedding vector dimension
func (p *QwenEmbeddingProvider) Dimension() int {
	return p.dimension
}

// CreateEmbedding creates embeddings for the given texts
func (p *QwenEmbeddingProvider) CreateEmbedding(ctx context.Context, texts []string) ([][]float32, error) {
	req := openai.EmbeddingRequest{
		Model: openai.EmbeddingModel(p.model),
		Input: texts,
	}

	resp, err := p.client.CreateEmbeddings(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("qwen embedding API error: %w", err)
	}

	embeddings := make([][]float32, len(resp.Data))
	for i, data := range resp.Data {
		embeddings[i] = data.Embedding
	}

	return embeddings, nil
}

const (
	openaiEmbeddingDefaultModel = "text-embedding-3-small"
	openaiEmbeddingDimension     = 1536
)

// OpenAIEmbeddingProvider implements EmbeddingProvider for OpenAI
type OpenAIEmbeddingProvider struct {
	client    *openai.Client
	model     string
	dimension int
}

// NewOpenAIEmbeddingProvider creates a new OpenAI embedding provider
func NewOpenAIEmbeddingProvider(cfg EmbeddingConfig) (*OpenAIEmbeddingProvider, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("API key is required for OpenAI embedding")
	}

	model := cfg.Model
	if model == "" {
		model = openaiEmbeddingDefaultModel
	}

	config := openai.DefaultConfig(cfg.APIKey)
	if cfg.BaseURL != "" {
		config.BaseURL = cfg.BaseURL
	}

	return &OpenAIEmbeddingProvider{
		client:    openai.NewClientWithConfig(config),
		model:     model,
		dimension: openaiEmbeddingDimension,
	}, nil
}

// Name returns the provider name
func (p *OpenAIEmbeddingProvider) Name() string {
	return "openai"
}

// Dimension returns the embedding vector dimension
func (p *OpenAIEmbeddingProvider) Dimension() int {
	return p.dimension
}

// CreateEmbedding creates embeddings for the given texts
func (p *OpenAIEmbeddingProvider) CreateEmbedding(ctx context.Context, texts []string) ([][]float32, error) {
	req := openai.EmbeddingRequest{
		Model: openai.EmbeddingModel(p.model),
		Input: texts,
	}

	resp, err := p.client.CreateEmbeddings(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("openai embedding API error: %w", err)
	}

	embeddings := make([][]float32, len(resp.Data))
	for i, data := range resp.Data {
		embeddings[i] = data.Embedding
	}

	return embeddings, nil
}
