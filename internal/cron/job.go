package cron

import (
	"time"

	"github.com/robfig/cron/v3"
)

// Job represents a scheduled task
type Job struct {
	ID        string         `json:"id"`                  // Unique identifier
	Name      string         `json:"name"`                // Human-readable name
	Tag       string         `json:"tag,omitempty"`       // Job tag: "user-schedule" or "assistant-task"
	Schedule  string         `json:"schedule"`            // Cron expression
	Tool      string         `json:"tool,omitempty"`      // MCP tool to execute
	Arguments map[string]any `json:"arguments,omitempty"` // Tool arguments
	Message   string         `json:"message,omitempty"`   // Direct message to send (no tool execution)
	Prompt    string         `json:"prompt,omitempty"`    // AI prompt to execute (full conversation with tools)
	Platform  string         `json:"platform,omitempty"`  // Target platform ("slack", "wecom", etc.)
	ChannelID string         `json:"channel_id,omitempty"` // Target channel/user to send to
	UserID    string         `json:"user_id,omitempty"`   // User who created the job
	Enabled   bool                   `json:"enabled"`             // Whether job is active
	CreatedAt time.Time              `json:"created_at"`          // Job creation timestamp
	LastRun   *time.Time             `json:"last_run,omitempty"`  // Last execution timestamp
	LastError string                 `json:"last_error,omitempty"` // Last error message

	// Runtime fields (not persisted)
	EntryID cron.EntryID `json:"-"` // Cron scheduler entry ID
}

// Clone creates a deep copy of the job
func (j *Job) Clone() *Job {
	clone := &Job{
		ID:        j.ID,
		Name:      j.Name,
		Tag:       j.Tag,
		Schedule:  j.Schedule,
		Tool:      j.Tool,
		Message:   j.Message,
		Prompt:    j.Prompt,
		Platform:  j.Platform,
		ChannelID: j.ChannelID,
		UserID:    j.UserID,
		Enabled:   j.Enabled,
		CreatedAt: j.CreatedAt,
		LastError: j.LastError,
		EntryID:   j.EntryID,
	}

	if j.LastRun != nil {
		lastRun := *j.LastRun
		clone.LastRun = &lastRun
	}

	if j.Arguments != nil {
		clone.Arguments = make(map[string]any, len(j.Arguments))
		for k, v := range j.Arguments {
			clone.Arguments[k] = v
		}
	}

	return clone
}
