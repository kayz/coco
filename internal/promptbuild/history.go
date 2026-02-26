package promptbuild

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/kayz/coco/internal/logger"
)

type historyMessage struct {
	Role      string
	Content   string
	CreatedAt time.Time
}

func (b *Builder) buildHistory(req BuildRequest) (string, error) {
	spec := req.History
	limit := spec.Limit
	if limit <= 0 {
		limit = req.MaxHistory
	}
	if limit <= 0 {
		limit = 200
	}

	dbPath := b.resolvePath(b.cfg.SQLitePath)
	if _, err := os.Stat(dbPath); err != nil {
		logger.Warn("History database not found, skipping: %s", dbPath)
		return "", nil
	}

	messages, err := loadHistory(dbPath, spec, limit)
	if err != nil {
		return "", err
	}
	if len(messages) == 0 {
		return "", nil
	}

	var out strings.Builder
	for i, msg := range messages {
		if i > 0 {
			out.WriteString("\n\n")
		}
		role := roleLabel(msg.Role)
		out.WriteString(role)
		out.WriteString(":\n")
		out.WriteString(strings.TrimSpace(msg.Content))
	}
	return out.String(), nil
}

func loadHistory(dbPath string, spec HistorySpec, limit int) ([]historyMessage, error) {
	dsn := dbPath
	if !strings.HasPrefix(dsn, "file:") {
		dsn = "file:" + filepath.ToSlash(dbPath) + "?mode=ro&_busy_timeout=5000"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	defer db.Close()

	var convID int64
	if spec.ConversationID > 0 {
		convID = spec.ConversationID
	} else if spec.Platform != "" && spec.ChannelID != "" && spec.UserID != "" {
		row := db.QueryRow(`SELECT id FROM conversations WHERE platform = ? AND channel_id = ? AND user_id = ?`,
			spec.Platform, spec.ChannelID, spec.UserID)
		if err := row.Scan(&convID); err != nil {
			if err == sql.ErrNoRows {
				return nil, nil
			}
			return nil, fmt.Errorf("load conversation: %w", err)
		}
	} else {
		return nil, nil
	}

	rows, err := db.Query(`
		SELECT role, content, created_at
		FROM messages
		WHERE conversation_id = ?
		ORDER BY created_at DESC
		LIMIT ?
	`, convID, limit)
	if err != nil {
		return nil, fmt.Errorf("load messages: %w", err)
	}
	defer rows.Close()

	var reversed []historyMessage
	for rows.Next() {
		var role string
		var content sql.NullString
		var createdAt string
		if err := rows.Scan(&role, &content, &createdAt); err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}
		var ts time.Time
		if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
			ts = t
		}
		reversed = append(reversed, historyMessage{
			Role:      role,
			Content:   content.String,
			CreatedAt: ts,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate messages: %w", err)
	}

	// reverse to chronological order
	for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
		reversed[i], reversed[j] = reversed[j], reversed[i]
	}
	return reversed, nil
}

func roleLabel(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "user":
		return "User"
	case "assistant":
		return "Assistant"
	case "tool":
		return "Tool"
	case "system":
		return "System"
	default:
		r := strings.TrimSpace(role)
		if r == "" {
			return "Unknown"
		}
		r = strings.ToLower(r)
		return strings.ToUpper(r[:1]) + r[1:]
	}
}
