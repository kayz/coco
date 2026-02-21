package persist

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// Store handles persistence of conversation history and daily reports using SQLite
type Store struct {
	db *sql.DB
	mu sync.RWMutex
}

// NewStore creates a new SQLite-backed persistence store at the given path
func NewStore(path string) (*Store, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory: %w", err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to set WAL mode: %w", err)
	}

	s := &Store{db: db}

	if err := s.init(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize database: %w", err)
	}

	return s, nil
}

// init creates the necessary tables if they don't exist
func (s *Store) init() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS conversations (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			platform    TEXT NOT NULL,
			channel_id  TEXT NOT NULL,
			user_id     TEXT NOT NULL,
			created_at  TEXT NOT NULL,
			updated_at  TEXT NOT NULL,
			is_active   INTEGER NOT NULL DEFAULT 1,
			UNIQUE(platform, channel_id, user_id)
		);

		CREATE TABLE IF NOT EXISTS messages (
			id               INTEGER PRIMARY KEY AUTOINCREMENT,
			conversation_id  INTEGER NOT NULL,
			role             TEXT NOT NULL,
			content          TEXT,
			tool_calls       TEXT,
			tool_result      TEXT,
			created_at       TEXT NOT NULL,
			FOREIGN KEY (conversation_id) REFERENCES conversations(id)
		);

		CREATE TABLE IF NOT EXISTS daily_reports (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			date        TEXT NOT NULL,
			user_id     TEXT NOT NULL,
			content     TEXT,
			summary     TEXT,
			tasks       TEXT,
			calendars   TEXT,
			created_at  TEXT NOT NULL,
			UNIQUE(date, user_id)
		);

		CREATE INDEX IF NOT EXISTS idx_messages_conversation ON messages(conversation_id);
		CREATE INDEX IF NOT EXISTS idx_messages_created ON messages(created_at);
		CREATE INDEX IF NOT EXISTS idx_dailyreport_date ON daily_reports(date);
		CREATE INDEX IF NOT EXISTS idx_dailyreport_user ON daily_reports(user_id);
	`)
	return err
}

// GetOrCreateConversation gets an existing conversation or creates a new one
func (s *Store) GetOrCreateConversation(platform, channelID, userID string) (*Conversation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	nowStr := now.Format(time.RFC3339)

	conv, err := s.getConversationInternal(platform, channelID, userID)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}

	if conv != nil {
		return conv, nil
	}

	result, err := s.db.Exec(`
		INSERT INTO conversations (platform, channel_id, user_id, created_at, updated_at, is_active)
		VALUES (?, ?, ?, ?, ?, 1)
	`, platform, channelID, userID, nowStr, nowStr)
	if err != nil {
		return nil, err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}

	return &Conversation{
		ID:        id,
		Platform:  platform,
		ChannelID: channelID,
		UserID:    userID,
		CreatedAt: now,
		UpdatedAt: now,
		IsActive:  true,
	}, nil
}

func (s *Store) getConversationInternal(platform, channelID, userID string) (*Conversation, error) {
	row := s.db.QueryRow(`
		SELECT id, platform, channel_id, user_id, created_at, updated_at, is_active
		FROM conversations
		WHERE platform = ? AND channel_id = ? AND user_id = ?
	`, platform, channelID, userID)

	var conv Conversation
	var createdAt, updatedAt string
	var isActive int

	err := row.Scan(&conv.ID, &conv.Platform, &conv.ChannelID, &conv.UserID, &createdAt, &updatedAt, &isActive)
	if err != nil {
		return nil, err
	}

	if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
		conv.CreatedAt = t
	}
	if t, err := time.Parse(time.RFC3339, updatedAt); err == nil {
		conv.UpdatedAt = t
	}
	conv.IsActive = isActive != 0

	messages, err := s.getMessagesInternal(conv.ID)
	if err == nil {
		conv.Messages = messages
	}

	return &conv, nil
}

func (s *Store) getMessagesInternal(conversationID int64) ([]Message, error) {
	rows, err := s.db.Query(`
		SELECT id, role, content, tool_calls, tool_result, created_at
		FROM messages
		WHERE conversation_id = ?
		ORDER BY created_at ASC
	`, conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []Message
	for rows.Next() {
		var msg Message
		var toolCalls, toolResult sql.NullString
		var createdAt string

		err := rows.Scan(&msg.ID, &msg.Role, &msg.Content, &toolCalls, &toolResult, &createdAt)
		if err != nil {
			return nil, err
		}

		if toolCalls.Valid {
			_ = fromJSON(toolCalls.String, &msg.ToolCalls)
		}
		if toolResult.Valid {
			var tr ToolResult
			if fromJSON(toolResult.String, &tr) == nil {
				msg.ToolResult = &tr
			}
		}
		if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
			msg.CreatedAt = t
		}

		messages = append(messages, msg)
	}

	return messages, rows.Err()
}

// LoadAllActiveConversations loads all active conversations with their messages
func (s *Store) LoadAllActiveConversations() ([]*Conversation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(`
		SELECT id, platform, channel_id, user_id, created_at, updated_at, is_active
		FROM conversations
		WHERE is_active = 1
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var conversations []*Conversation
	for rows.Next() {
		var conv Conversation
		var createdAt, updatedAt string
		var isActive int

		err := rows.Scan(&conv.ID, &conv.Platform, &conv.ChannelID, &conv.UserID, &createdAt, &updatedAt, &isActive)
		if err != nil {
			return nil, err
		}

		if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
			conv.CreatedAt = t
		}
		if t, err := time.Parse(time.RFC3339, updatedAt); err == nil {
			conv.UpdatedAt = t
		}
		conv.IsActive = isActive != 0

		messages, err := s.getMessagesInternal(conv.ID)
		if err == nil {
			conv.Messages = messages
		}

		conversations = append(conversations, &conv)
	}

	return conversations, rows.Err()
}

// AddMessage adds a message to a conversation
func (s *Store) AddMessage(conversationID int64, msg Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().Format(time.RFC3339)

	_, err := s.db.Exec(`
		INSERT INTO messages (conversation_id, role, content, tool_calls, tool_result, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, conversationID, msg.Role, msg.Content, toJSON(msg.ToolCalls), toJSON(msg.ToolResult), now)
	if err != nil {
		return err
	}

	_, err = s.db.Exec(`
		UPDATE conversations SET updated_at = ? WHERE id = ?
	`, now, conversationID)
	return err
}

// SaveDailyReport saves or updates a daily report
func (s *Store) SaveDailyReport(report *DailyReport) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().Format(time.RFC3339)

	_, err := s.db.Exec(`
		INSERT INTO daily_reports (date, user_id, content, summary, tasks, calendars, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(date, user_id) DO UPDATE SET
			content=excluded.content, summary=excluded.summary,
			tasks=excluded.tasks, calendars=excluded.calendars, created_at=excluded.created_at
	`, report.Date, report.UserID, report.Content, report.Summary,
		toJSON(report.Tasks), toJSON(report.Calendars), now)
	return err
}

// GetDailyReport gets a daily report for a specific date
func (s *Store) GetDailyReport(date, userID string) (*DailyReport, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	row := s.db.QueryRow(`
		SELECT id, date, user_id, content, summary, tasks, calendars, created_at
		FROM daily_reports
		WHERE date = ? AND user_id = ?
	`, date, userID)

	var report DailyReport
	var tasks, calendars sql.NullString
	var createdAt string

	err := row.Scan(&report.ID, &report.Date, &report.UserID, &report.Content, &report.Summary,
		&tasks, &calendars, &createdAt)
	if err != nil {
		return nil, err
	}

	if tasks.Valid {
		_ = fromJSON(tasks.String, &report.Tasks)
	}
	if calendars.Valid {
		_ = fromJSON(calendars.String, &report.Calendars)
	}
	if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
		report.CreatedAt = t
	}

	return &report, nil
}

// GetLatestDailyReport gets the latest daily report
func (s *Store) GetLatestDailyReport(userID string) (*DailyReport, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	row := s.db.QueryRow(`
		SELECT id, date, user_id, content, summary, tasks, calendars, created_at
		FROM daily_reports
		WHERE user_id = ?
		ORDER BY date DESC
		LIMIT 1
	`, userID)

	var report DailyReport
	var tasks, calendars sql.NullString
	var createdAt string

	err := row.Scan(&report.ID, &report.Date, &report.UserID, &report.Content, &report.Summary,
		&tasks, &calendars, &createdAt)
	if err != nil {
		return nil, err
	}

	if tasks.Valid {
		_ = fromJSON(tasks.String, &report.Tasks)
	}
	if calendars.Valid {
		_ = fromJSON(calendars.String, &report.Calendars)
	}
	if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
		report.CreatedAt = t
	}

	return &report, nil
}

// ListDailyReports lists all daily reports for a user
func (s *Store) ListDailyReports(userID string, limit int) ([]*DailyReport, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if limit <= 0 {
		limit = 30
	}

	rows, err := s.db.Query(`
		SELECT id, date, user_id, content, summary, tasks, calendars, created_at
		FROM daily_reports
		WHERE user_id = ?
		ORDER BY date DESC
		LIMIT ?
	`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var reports []*DailyReport
	for rows.Next() {
		var report DailyReport
		var tasks, calendars sql.NullString
		var createdAt string

		err := rows.Scan(&report.ID, &report.Date, &report.UserID, &report.Content, &report.Summary,
			&tasks, &calendars, &createdAt)
		if err != nil {
			return nil, err
		}

		if tasks.Valid {
			_ = fromJSON(tasks.String, &report.Tasks)
		}
		if calendars.Valid {
			_ = fromJSON(calendars.String, &report.Calendars)
		}
		if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
			report.CreatedAt = t
		}

		reports = append(reports, &report)
	}

	return reports, rows.Err()
}

// GetConversationSummary gets a summary of a conversation
func (s *Store) GetConversationSummary(conversationID int64) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	row := s.db.QueryRow(`
		SELECT COUNT(*) as msg_count, MIN(created_at) as first_msg, MAX(created_at) as last_msg
		FROM messages
		WHERE conversation_id = ?
	`, conversationID)

	var msgCount int
	var firstMsg, lastMsg sql.NullString

	err := row.Scan(&msgCount, &firstMsg, &lastMsg)
	if err != nil {
		return "", err
	}

	if msgCount == 0 {
		return "无消息", nil
	}

	summary := fmt.Sprintf("消息数: %d", msgCount)
	if firstMsg.Valid {
		if t, err := time.Parse(time.RFC3339, firstMsg.String); err == nil {
			summary += fmt.Sprintf(", 首条: %s", t.Format("2006-01-02"))
		}
	}
	if lastMsg.Valid {
		if t, err := time.Parse(time.RFC3339, lastMsg.String); err == nil {
			summary += fmt.Sprintf(", 最新: %s", t.Format("2006-01-02 15:04"))
		}
	}

	return summary, nil
}

// SearchMessages searches messages by keyword
func (s *Store) SearchMessages(userID, keyword string, limit int) ([]Message, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if limit <= 0 {
		limit = 50
	}

	rows, err := s.db.Query(`
		SELECT m.id, m.role, m.content, m.tool_calls, m.tool_result, m.created_at
		FROM messages m
		JOIN conversations c ON m.conversation_id = c.id
		WHERE c.user_id = ? AND m.content LIKE ?
		ORDER BY m.created_at DESC
		LIMIT ?
	`, userID, "%"+keyword+"%", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []Message
	for rows.Next() {
		var msg Message
		var toolCalls, toolResult sql.NullString
		var createdAt string

		err := rows.Scan(&msg.ID, &msg.Role, &msg.Content, &toolCalls, &toolResult, &createdAt)
		if err != nil {
			return nil, err
		}

		if toolCalls.Valid {
			_ = fromJSON(toolCalls.String, &msg.ToolCalls)
		}
		if toolResult.Valid {
			var tr ToolResult
			if fromJSON(toolResult.String, &tr) == nil {
				msg.ToolResult = &tr
			}
		}
		if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
			msg.CreatedAt = t
		}

		messages = append(messages, msg)
	}

	return messages, rows.Err()
}

// Close closes the database connection
func (s *Store) Close() error {
	return s.db.Close()
}

// ConversationKey generates a unique key for a conversation
func ConversationKey(platform, channelID, userID string) string {
	return platform + ":" + channelID + ":" + userID
}

// ParseConversationKey parses a conversation key into its components
func ParseConversationKey(key string) (platform, channelID, userID string) {
	parts := splitN(key, ":", 3)
	if len(parts) >= 3 {
		return parts[0], parts[1], parts[2]
	}
	return "", "", ""
}

func splitN(s string, sep string, n int) []string {
	var result []string
	for i := 0; i < n-1; i++ {
		idx := indexOf(s, sep)
		if idx == -1 {
			break
		}
		result = append(result, s[:idx])
		s = s[idx+len(sep):]
	}
	if s != "" {
		result = append(result, s)
	}
	return result
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

// GetYesterdayDate returns yesterday's date in YYYY-MM-DD format
func GetYesterdayDate() string {
	return time.Now().AddDate(0, 0, -1).Format("2006-01-02")
}

// GetTodayDate returns today's date in YYYY-MM-DD format
func GetTodayDate() string {
	return time.Now().Format("2006-01-02")
}

// Log logs a message
func Log(format string, v ...interface{}) {
	log.Printf("[PERSIST] "+format, v...)
}
