package persist

import (
	"encoding/json"
	"time"
)

// Conversation represents a conversation with a user
type Conversation struct {
	ID         int64
	Platform   string
	ChannelID  string
	UserID     string
	CreatedAt  time.Time
	UpdatedAt  time.Time
	IsActive   bool
	Messages   []Message
}

// Message represents a single message in a conversation
type Message struct {
	ID          int64
	Role        string // "user" | "assistant" | "tool"
	Content     string
	ToolCalls   []ToolCall
	ToolResult  *ToolResult
	CreatedAt   time.Time
}

// ToolCall represents a tool call
type ToolCall struct {
	ID      string                 `json:"id"`
	Name    string                 `json:"name"`
	Input   map[string]interface{} `json:"input"`
}

// ToolResult represents the result of a tool call
type ToolResult struct {
	ToolCallID string      `json:"toolCallId"`
	Content    interface{} `json:"content"`
	IsError    bool        `json:"isError"`
}

// DailyReport represents a daily report
type DailyReport struct {
	ID          int64
	Date        string // YYYY-MM-DD
	UserID      string
	Content     string
	Summary     string
	Tasks       []TaskItem
	Calendars   []CalendarItem
	CreatedAt   time.Time
}

// TaskItem represents a task item in the report
type TaskItem struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Status      string `json:"status"` // "pending" | "in_progress" | "completed"
	Priority    string `json:"priority"` // "low" | "medium" | "high"
	DueDate     string `json:"dueDate,omitempty"`
}

// CalendarItem represents a calendar event item in the report
type CalendarItem struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	StartTime   string `json:"startTime"`
	EndTime     string `json:"endTime"`
	Location    string `json:"location,omitempty"`
}

// scanner interface for both *sql.Row and *sql.Rows
type scanner interface {
	Scan(dest ...any) error
}

// toJSON converts an object to JSON string
func toJSON(v interface{}) string {
	data, err := json.Marshal(v)
	if err != nil {
		return "[]"
	}
	return string(data)
}

// fromJSON parses JSON string into an object
func fromJSON(data string, v interface{}) error {
	if data == "" || data == "[]" || data == "null" {
		return nil
	}
	return json.Unmarshal([]byte(data), v)
}
