package agent

import (
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/kayz/coco/internal/persist"
)

// ConversationMemory stores conversation history per user/channel
type ConversationMemory struct {
	conversations map[string]*Conversation
	mu            sync.RWMutex
	store         *persist.Store
	maxMessages   int
}

// Conversation holds messages for a single conversation
type Conversation struct {
	ID         int64
	Messages   []Message
	UpdatedAt  time.Time
}

// NewMemory creates a new conversation memory store
func NewMemory(store *persist.Store, maxMessages int) *ConversationMemory {
	if maxMessages <= 0 {
		maxMessages = 200
	}

	m := &ConversationMemory{
		conversations: make(map[string]*Conversation),
		store:         store,
		maxMessages:   maxMessages,
	}

	m.LoadFromStore()
	return m
}

// LoadFromStore loads all active conversations from the persistent store
func (m *ConversationMemory) LoadFromStore() {
	if m.store == nil {
		return
	}

	convs, err := m.store.LoadAllActiveConversations()
	if err != nil {
		log.Printf("[MEMORY] Failed to load conversations from store: %v", err)
		return
	}

	for _, pc := range convs {
		key := persist.ConversationKey(pc.Platform, pc.ChannelID, pc.UserID)
		msgs := make([]Message, 0, len(pc.Messages))
		for _, pm := range pc.Messages {
			msgs = append(msgs, m.convertPersistMessage(pm))
		}

		m.conversations[key] = &Conversation{
			ID:        pc.ID,
			Messages:  msgs,
			UpdatedAt: pc.UpdatedAt,
		}
	}

	log.Printf("[MEMORY] Loaded %d conversations from store", len(m.conversations))
}

// GetHistory returns the conversation history for a key (user+channel)
func (m *ConversationMemory) GetHistory(key string) []Message {
	m.mu.RLock()
	defer m.mu.RUnlock()

	conv, ok := m.conversations[key]
	if !ok {
		return nil
	}

	messages := make([]Message, len(conv.Messages))
	copy(messages, conv.Messages)
	return messages
}

// AddMessage adds a message to the conversation history
func (m *ConversationMemory) AddMessage(key string, msg Message) {
	m.mu.Lock()
	defer m.mu.Unlock()

	conv, ok := m.conversations[key]
	if !ok {
		platform, channelID, userID := persist.ParseConversationKey(key)
		pc, err := m.store.GetOrCreateConversation(platform, channelID, userID)
		if err != nil {
			log.Printf("[MEMORY] Failed to get/create conversation: %v", err)
			return
		}
		conv = &Conversation{
			ID:        pc.ID,
			Messages:  make([]Message, 0),
			UpdatedAt: time.Now(),
		}
		m.conversations[key] = conv
	}

	conv.Messages = append(conv.Messages, msg)
	conv.UpdatedAt = time.Now()

	if len(conv.Messages) > m.maxMessages {
		startIdx := len(conv.Messages) - m.maxMessages
		if startIdx%2 != 0 {
			startIdx++
		}
		conv.Messages = conv.Messages[startIdx:]
	}

	if m.store != nil {
		pm := m.convertToPersistMessage(msg)
		if err := m.store.AddMessage(conv.ID, pm); err != nil {
			log.Printf("[MEMORY] Failed to persist message: %v", err)
		}
	}
}

// AddExchange adds both user and assistant messages
func (m *ConversationMemory) AddExchange(key string, userMsg, assistantMsg Message) {
	m.mu.Lock()
	defer m.mu.Unlock()

	conv, ok := m.conversations[key]
	if !ok {
		platform, channelID, userID := persist.ParseConversationKey(key)
		pc, err := m.store.GetOrCreateConversation(platform, channelID, userID)
		if err != nil {
			log.Printf("[MEMORY] Failed to get/create conversation: %v", err)
			return
		}
		conv = &Conversation{
			ID:        pc.ID,
			Messages:  make([]Message, 0),
			UpdatedAt: time.Now(),
		}
		m.conversations[key] = conv
	}

	conv.Messages = append(conv.Messages, userMsg, assistantMsg)
	conv.UpdatedAt = time.Now()

	if len(conv.Messages) > m.maxMessages {
		startIdx := len(conv.Messages) - m.maxMessages
		if startIdx%2 != 0 {
			startIdx++
		}
		conv.Messages = conv.Messages[startIdx:]
	}

	if m.store != nil {
		pUserMsg := m.convertToPersistMessage(userMsg)
		pAssistMsg := m.convertToPersistMessage(assistantMsg)
		if err := m.store.AddMessage(conv.ID, pUserMsg); err != nil {
			log.Printf("[MEMORY] Failed to persist user message: %v", err)
		}
		if err := m.store.AddMessage(conv.ID, pAssistMsg); err != nil {
			log.Printf("[MEMORY] Failed to persist assistant message: %v", err)
		}
	}
}

// Clear clears the conversation history for a key
func (m *ConversationMemory) Clear(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.conversations, key)
}

// ClearAll clears all conversation histories
func (m *ConversationMemory) ClearAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.conversations = make(map[string]*Conversation)
}

func (m *ConversationMemory) convertPersistMessage(pm persist.Message) Message {
	return Message{
		Role:       pm.Role,
		Content:    pm.Content,
		ToolCalls:  m.convertToolCalls(pm.ToolCalls),
	}
}

func (m *ConversationMemory) convertToPersistMessage(msg Message) persist.Message {
	return persist.Message{
		Role:       msg.Role,
		Content:    msg.Content,
		ToolCalls:  m.convertToPersistToolCalls(msg.ToolCalls),
	}
}

func (m *ConversationMemory) convertToolCalls(ptcs []persist.ToolCall) []ToolCall {
	if ptcs == nil {
		return nil
	}
	tcs := make([]ToolCall, 0, len(ptcs))
	for _, ptc := range ptcs {
		input, _ := json.Marshal(ptc.Input)
		tcs = append(tcs, ToolCall{
			ID:     ptc.ID,
			Name:   ptc.Name,
			Input:  input,
		})
	}
	return tcs
}

func (m *ConversationMemory) convertToPersistToolCalls(tcs []ToolCall) []persist.ToolCall {
	if tcs == nil {
		return nil
	}
	ptcs := make([]persist.ToolCall, 0, len(tcs))
	for _, tc := range tcs {
		var input map[string]interface{}
		_ = json.Unmarshal(tc.Input, &input)
		ptcs = append(ptcs, persist.ToolCall{
			ID:     tc.ID,
			Name:   tc.Name,
			Input:  input,
		})
	}
	return ptcs
}
