package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/philippgille/chromem-go"
	"github.com/kayz/coco/internal/config"
	"github.com/kayz/coco/internal/logger"
)

const (
	ragCollectionName = "coco-memory"
	maxChunkSize      = 1000
	maxChunks         = 10000
)

// MemoryType represents the type of memory
type MemoryType string

const (
	MemoryTypeConversation MemoryType = "conversation"
	MemoryTypeFact         MemoryType = "fact"
	MemoryTypePreference   MemoryType = "preference"
)

// MemoryItem represents a single memory item
type MemoryItem struct {
	ID        string
	Type      MemoryType
	Content   string
	Metadata  map[string]string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// RAGMemory provides long-term memory with semantic search
type RAGMemory struct {
	db          *chromem.DB
	collection  *chromem.Collection
	embProvider EmbeddingProvider
	enabled     bool
	dataDir     string
}

// NewRAGMemory creates a new RAG memory store
func NewRAGMemory(cfg config.EmbeddingConfig) (*RAGMemory, error) {
	if !cfg.Enabled {
		return &RAGMemory{enabled: false}, nil
	}

	if cfg.APIKey == "" {
		return nil, fmt.Errorf("embedding API key is required")
	}

	embCfg := EmbeddingConfig{
		Provider: cfg.Provider,
		APIKey:   cfg.APIKey,
		BaseURL:  cfg.BaseURL,
		Model:    cfg.Model,
	}

	embProvider, err := NewEmbeddingProvider(embCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create embedding provider: %w", err)
	}

	dataDir := filepath.Join(getExecutableDir(), ".coco", "rag")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	dbPath := filepath.Join(dataDir, "chromem.db")
	db, err := chromem.NewPersistentDB(dbPath, false)
	if err != nil {
		return nil, fmt.Errorf("failed to create chromem DB: %w", err)
	}

	collection, err := db.GetOrCreateCollection(ragCollectionName, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get/create collection: %w", err)
	}

	return &RAGMemory{
		db:          db,
		collection:  collection,
		embProvider: embProvider,
		enabled:     true,
		dataDir:     dataDir,
	}, nil
}

// IsEnabled returns whether RAG memory is enabled
func (m *RAGMemory) IsEnabled() bool {
	return m.enabled
}

// AddMemory adds a memory item
func (m *RAGMemory) AddMemory(ctx context.Context, item MemoryItem) error {
	if !m.enabled {
		return nil
	}

	if item.CreatedAt.IsZero() {
		item.CreatedAt = time.Now()
	}
	if item.UpdatedAt.IsZero() {
		item.UpdatedAt = time.Now()
	}

	chunks := m.splitIntoChunks(item.Content)
	if len(chunks) == 0 {
		return nil
	}

	embeddings, err := m.embProvider.CreateEmbedding(ctx, chunks)
	if err != nil {
		return fmt.Errorf("failed to create embeddings: %w", err)
	}

	docs := make([]chromem.Document, 0, len(chunks))
	for i, chunk := range chunks {
		metadata := map[string]string{
			"id":         item.ID,
			"type":       string(item.Type),
			"created_at": item.CreatedAt.Format(time.RFC3339),
			"updated_at": item.UpdatedAt.Format(time.RFC3339),
			"chunk_idx":  fmt.Sprintf("%d", i),
		}
		for k, v := range item.Metadata {
			metadata[k] = v
		}

		docs = append(docs, chromem.Document{
			ID:        fmt.Sprintf("%s-%d", item.ID, i),
			Embedding: embeddings[i],
			Metadata:  metadata,
			Content:   chunk,
		})
	}

	if err := m.collection.AddDocuments(ctx, docs, 1); err != nil {
		return fmt.Errorf("failed to add documents: %w", err)
	}

	logger.Debug("[RAG] Added memory: %s (%d chunks)", item.ID, len(chunks))
	return nil
}

// SearchMemories searches for relevant memories
func (m *RAGMemory) SearchMemories(ctx context.Context, query string, limit int) ([]MemoryItem, error) {
	if !m.enabled {
		return nil, nil
	}

	if limit <= 0 {
		limit = 5
	}

	queryEmbedding, err := m.embProvider.CreateEmbedding(ctx, []string{query})
	if err != nil {
		return nil, fmt.Errorf("failed to create query embedding: %w", err)
	}

	results, err := m.collection.QueryEmbedding(
		ctx,
		queryEmbedding[0],
		limit,
		nil,
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query collection: %w", err)
	}

	items := make([]MemoryItem, 0, len(results))
	for _, res := range results {
		item := m.resultToMemoryItem(res)
		items = append(items, item)
	}

	logger.Debug("[RAG] Found %d memories for query: %s", len(items), query)
	return items, nil
}

// DeleteMemory deletes a memory by ID
func (m *RAGMemory) DeleteMemory(ctx context.Context, id string) error {
	if !m.enabled {
		return nil
	}

	if err := m.collection.Delete(ctx, map[string]string{"id": id}, nil); err != nil {
		return fmt.Errorf("failed to delete memory: %w", err)
	}

	logger.Debug("[RAG] Deleted memory: %s", id)
	return nil
}

// ClearAll clears all memories
func (m *RAGMemory) ClearAll(ctx context.Context) error {
	if !m.enabled {
		return nil
	}

	if err := m.collection.Delete(ctx, nil, nil); err != nil {
		return fmt.Errorf("failed to clear all memories: %w", err)
	}

	logger.Debug("[RAG] Cleared all memories")
	return nil
}

// Close closes the RAG memory store
func (m *RAGMemory) Close() error {
	return nil
}

// splitIntoChunks splits text into chunks (by paragraphs, not just bytes)
func (m *RAGMemory) splitIntoChunks(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}

	paragraphs := strings.Split(text, "\n\n")
	chunks := make([]string, 0)
	currentChunk := ""

	for _, para := range paragraphs {
		para = strings.TrimSpace(para)
		if para == "" {
			continue
		}

		if len(currentChunk)+len(para)+2 <= maxChunkSize {
			if currentChunk != "" {
				currentChunk += "\n\n"
			}
			currentChunk += para
		} else {
			if currentChunk != "" {
				chunks = append(chunks, currentChunk)
			}
			if len(para) > maxChunkSize {
				sentences := strings.SplitAfter(para, "。")
				smallChunk := ""
				for _, sent := range sentences {
					if len(smallChunk)+len(sent) <= maxChunkSize {
						smallChunk += sent
					} else {
						if smallChunk != "" {
							chunks = append(chunks, smallChunk)
						}
						smallChunk = sent
					}
				}
				if smallChunk != "" {
					chunks = append(chunks, smallChunk)
				}
			} else {
				currentChunk = para
			}
		}
	}

	if currentChunk != "" {
		chunks = append(chunks, currentChunk)
	}

	if len(chunks) > maxChunks {
		chunks = chunks[:maxChunks]
	}

	return chunks
}

func (m *RAGMemory) resultToMemoryItem(res chromem.Result) MemoryItem {
	item := MemoryItem{
		Content: res.Content,
	}
	if id, ok := res.Metadata["id"]; ok {
		item.ID = id
	}
	if memType, ok := res.Metadata["type"]; ok {
		item.Type = MemoryType(memType)
	}
	if createdAt, ok := res.Metadata["created_at"]; ok {
		if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
			item.CreatedAt = t
		}
	}
	if updatedAt, ok := res.Metadata["updated_at"]; ok {
		if t, err := time.Parse(time.RFC3339, updatedAt); err == nil {
			item.UpdatedAt = t
		}
	}
	item.Metadata = make(map[string]string)
	for k, v := range res.Metadata {
		if k != "id" && k != "type" && k != "created_at" && k != "updated_at" && k != "chunk_idx" {
			item.Metadata[k] = v
		}
	}
	return item
}
