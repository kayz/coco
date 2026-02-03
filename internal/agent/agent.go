package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"runtime"
	"strings"

	"github.com/pltanton/lingti-bot/internal/router"
)

// Agent processes messages using AI providers and tools
type Agent struct {
	provider Provider
}

// Config holds agent configuration
type Config struct {
	Provider string // "claude" or "deepseek" (default: "claude")
	APIKey   string
	BaseURL  string // Custom API base URL (optional)
	Model    string // Model name (optional, uses provider default)
}

// New creates a new Agent with the specified provider
func New(cfg Config) (*Agent, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("API key is required")
	}

	provider, err := createProvider(cfg)
	if err != nil {
		return nil, err
	}

	return &Agent{
		provider: provider,
	}, nil
}

// createProvider creates the appropriate AI provider based on config
func createProvider(cfg Config) (Provider, error) {
	switch strings.ToLower(cfg.Provider) {
	case "deepseek":
		return NewDeepSeekProvider(DeepSeekConfig{
			APIKey:  cfg.APIKey,
			BaseURL: cfg.BaseURL,
			Model:   cfg.Model,
		})
	case "kimi", "moonshot":
		return NewKimiProvider(KimiConfig{
			APIKey:  cfg.APIKey,
			BaseURL: cfg.BaseURL,
			Model:   cfg.Model,
		})
	case "claude", "anthropic", "":
		return NewClaudeProvider(ClaudeConfig{
			APIKey:  cfg.APIKey,
			BaseURL: cfg.BaseURL,
			Model:   cfg.Model,
		})
	default:
		return nil, fmt.Errorf("unknown provider: %s (supported: claude, deepseek, kimi)", cfg.Provider)
	}
}

// handleBuiltinCommand handles special commands without calling AI
func (a *Agent) handleBuiltinCommand(msg router.Message) (router.Response, bool) {
	text := strings.TrimSpace(strings.ToLower(msg.Text))

	switch text {
	case "/whoami", "whoami", "我是谁", "我的id":
		return router.Response{
			Text: fmt.Sprintf("用户信息:\n- 用户ID: %s\n- 用户名: %s\n- 平台: %s\n- 频道ID: %s",
				msg.UserID, msg.Username, msg.Platform, msg.ChannelID),
		}, true
	case "/help", "help", "帮助":
		return router.Response{
			Text: "可用命令:\n- /whoami - 查看你的用户信息\n- /help - 显示帮助\n\n你也可以直接用自然语言和我对话！",
		}, true
	}

	return router.Response{}, false
}

// HandleMessage processes a message and returns a response
func (a *Agent) HandleMessage(ctx context.Context, msg router.Message) (router.Response, error) {
	log.Printf("[Agent] Processing message from %s: %s (provider: %s)", msg.Username, msg.Text, a.provider.Name())

	// Handle built-in commands
	if resp, handled := a.handleBuiltinCommand(msg); handled {
		return resp, nil
	}

	// Build the tools list
	tools := a.buildToolsList()

	// Create initial messages
	messages := []Message{
		{
			Role:    "user",
			Content: msg.Text,
		},
	}

	// Get system info for context
	homeDir, _ := os.UserHomeDir()
	if homeDir == "" {
		homeDir = "~"
	}

	// System prompt with actual paths
	systemPrompt := fmt.Sprintf(`You are a helpful AI assistant running on the user's computer.

## System Environment
- Operating System: %s
- Architecture: %s
- Home Directory: %s
- Desktop: %s/Desktop
- Documents: %s/Documents
- Downloads: %s/Downloads

## Available Tools
- File operations: list, read, write, search files. Use ~ for home directory (e.g., ~/Desktop)
- Calendar: list events, create events, search events (macOS)
- Reminders: list, add, complete reminders (macOS)
- Notes: list, read, create, search notes (macOS)
- Weather: get current weather and forecast
- Web: search the web, fetch URL content, open URLs in browser
- Clipboard: read and write clipboard content
- Screenshot: capture screen
- Music: control Spotify/Apple Music playback
- Notifications: send system notifications
- System info: CPU, memory, disk usage
- Shell commands: execute commands (be careful!)
- Process management: list, info, kill processes

## Important
- When users mention "桌面" or "Desktop", use path: ~/Desktop
- When users mention "下载" or "Downloads", use path: ~/Downloads
- When users mention "文档" or "Documents", use path: ~/Documents
- Always use ~ prefix for home directory paths, the system will expand it automatically
- Be concise in your responses
- Confirm before destructive actions (delete, kill process)
- When users ask to open a website or URL, use the open_url tool`, runtime.GOOS, runtime.GOARCH, homeDir, homeDir, homeDir, homeDir)

	// Call AI provider
	resp, err := a.provider.Chat(ctx, ChatRequest{
		Messages:     messages,
		SystemPrompt: systemPrompt,
		Tools:        tools,
		MaxTokens:    4096,
	})
	if err != nil {
		return router.Response{}, fmt.Errorf("AI error: %w", err)
	}

	// Handle tool use if needed
	for resp.FinishReason == "tool_use" {
		// Process tool calls
		toolResults := a.processToolCalls(ctx, resp.ToolCalls)

		// Add assistant response with tool calls
		messages = append(messages, Message{
			Role:      "assistant",
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		})

		// Add tool results
		for _, result := range toolResults {
			messages = append(messages, Message{
				Role:       "user",
				ToolResult: &result,
			})
		}

		// Continue the conversation
		resp, err = a.provider.Chat(ctx, ChatRequest{
			Messages:     messages,
			SystemPrompt: systemPrompt,
			Tools:        tools,
			MaxTokens:    4096,
		})
		if err != nil {
			return router.Response{}, fmt.Errorf("AI error: %w", err)
		}
	}

	return router.Response{Text: resp.Content}, nil
}

// buildToolsList creates the tools list for the AI provider
func (a *Agent) buildToolsList() []Tool {
	return []Tool{
		// === FILE OPERATIONS ===
		{
			Name:        "file_read",
			Description: "Read the contents of a file. Use ~ for home directory.",
			InputSchema: jsonSchema(map[string]any{
				"type":       "object",
				"properties": map[string]any{"path": map[string]string{"type": "string", "description": "Path to the file (use ~ for home, e.g., ~/Desktop/file.txt)"}},
				"required":   []string{"path"},
			}),
		},
		{
			Name:        "file_list",
			Description: "List contents of a directory. Use ~/Desktop for desktop, ~/Downloads for downloads, etc.",
			InputSchema: jsonSchema(map[string]any{
				"type":       "object",
				"properties": map[string]any{"path": map[string]string{"type": "string", "description": "Directory path (use ~ for home, e.g., ~/Desktop)"}},
			}),
		},
		{
			Name:        "file_list_old",
			Description: "List files not modified for specified days. Use ~/Desktop for desktop, etc.",
			InputSchema: jsonSchema(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]string{"type": "string", "description": "Directory path (use ~ for home, e.g., ~/Desktop)"},
					"days": map[string]string{"type": "number", "description": "Minimum days since modification"},
				},
				"required": []string{"path"},
			}),
		},
		{
			Name:        "file_trash",
			Description: "Move files to Trash",
			InputSchema: jsonSchema(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"files": map[string]any{"type": "array", "items": map[string]string{"type": "string"}, "description": "File paths to trash"},
				},
				"required": []string{"files"},
			}),
		},

		// === CALENDAR ===
		{
			Name:        "calendar_today",
			Description: "Get today's calendar events",
			InputSchema: jsonSchema(map[string]any{"type": "object", "properties": map[string]any{}}),
		},
		{
			Name:        "calendar_list_events",
			Description: "List upcoming calendar events",
			InputSchema: jsonSchema(map[string]any{
				"type":       "object",
				"properties": map[string]any{"days": map[string]string{"type": "number", "description": "Days ahead (default 7)"}},
			}),
		},
		{
			Name:        "calendar_create_event",
			Description: "Create a new calendar event",
			InputSchema: jsonSchema(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"title":      map[string]string{"type": "string", "description": "Event title"},
					"start_time": map[string]string{"type": "string", "description": "Start time (YYYY-MM-DD HH:MM)"},
					"duration":   map[string]string{"type": "number", "description": "Duration in minutes (default 60)"},
					"calendar":   map[string]string{"type": "string", "description": "Calendar name (optional)"},
					"location":   map[string]string{"type": "string", "description": "Event location (optional)"},
					"notes":      map[string]string{"type": "string", "description": "Event notes (optional)"},
				},
				"required": []string{"title", "start_time"},
			}),
		},
		{
			Name:        "calendar_search",
			Description: "Search calendar events by keyword",
			InputSchema: jsonSchema(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"keyword": map[string]string{"type": "string", "description": "Search keyword"},
					"days":    map[string]string{"type": "number", "description": "Days to search (default 30)"},
				},
				"required": []string{"keyword"},
			}),
		},
		{
			Name:        "calendar_delete",
			Description: "Delete a calendar event by title",
			InputSchema: jsonSchema(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"title":    map[string]string{"type": "string", "description": "Event title to delete"},
					"calendar": map[string]string{"type": "string", "description": "Calendar name (optional)"},
					"date":     map[string]string{"type": "string", "description": "Date (YYYY-MM-DD) to narrow search (optional)"},
				},
				"required": []string{"title"},
			}),
		},

		// === REMINDERS ===
		{
			Name:        "reminders_list",
			Description: "List all pending reminders",
			InputSchema: jsonSchema(map[string]any{"type": "object", "properties": map[string]any{}}),
		},
		{
			Name:        "reminders_add",
			Description: "Create a new reminder",
			InputSchema: jsonSchema(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"title": map[string]string{"type": "string", "description": "Reminder title"},
					"list":  map[string]string{"type": "string", "description": "Reminder list name (default: Reminders)"},
					"due":   map[string]string{"type": "string", "description": "Due date (YYYY-MM-DD or YYYY-MM-DD HH:MM)"},
					"notes": map[string]string{"type": "string", "description": "Additional notes"},
				},
				"required": []string{"title"},
			}),
		},
		{
			Name:        "reminders_complete",
			Description: "Mark a reminder as complete",
			InputSchema: jsonSchema(map[string]any{
				"type":       "object",
				"properties": map[string]any{"title": map[string]string{"type": "string", "description": "Reminder title"}},
				"required":   []string{"title"},
			}),
		},
		{
			Name:        "reminders_delete",
			Description: "Delete a reminder",
			InputSchema: jsonSchema(map[string]any{
				"type":       "object",
				"properties": map[string]any{"title": map[string]string{"type": "string", "description": "Reminder title"}},
				"required":   []string{"title"},
			}),
		},

		// === NOTES ===
		{
			Name:        "notes_list",
			Description: "List notes in a folder",
			InputSchema: jsonSchema(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"folder": map[string]string{"type": "string", "description": "Folder name (default: Notes)"},
					"limit":  map[string]string{"type": "number", "description": "Max notes to show (default 20)"},
				},
			}),
		},
		{
			Name:        "notes_read",
			Description: "Read a note's content",
			InputSchema: jsonSchema(map[string]any{
				"type":       "object",
				"properties": map[string]any{"title": map[string]string{"type": "string", "description": "Note title"}},
				"required":   []string{"title"},
			}),
		},
		{
			Name:        "notes_create",
			Description: "Create a new note",
			InputSchema: jsonSchema(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"title":  map[string]string{"type": "string", "description": "Note title"},
					"body":   map[string]string{"type": "string", "description": "Note content"},
					"folder": map[string]string{"type": "string", "description": "Folder name (default: Notes)"},
				},
				"required": []string{"title"},
			}),
		},
		{
			Name:        "notes_search",
			Description: "Search notes by keyword",
			InputSchema: jsonSchema(map[string]any{
				"type":       "object",
				"properties": map[string]any{"keyword": map[string]string{"type": "string", "description": "Search keyword"}},
				"required":   []string{"keyword"},
			}),
		},

		// === WEATHER ===
		{
			Name:        "weather_current",
			Description: "Get current weather for a location",
			InputSchema: jsonSchema(map[string]any{
				"type":       "object",
				"properties": map[string]any{"location": map[string]string{"type": "string", "description": "City name or location (e.g., 'London', 'Tokyo')"}},
			}),
		},
		{
			Name:        "weather_forecast",
			Description: "Get weather forecast for a location",
			InputSchema: jsonSchema(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"location": map[string]string{"type": "string", "description": "City name or location"},
					"days":     map[string]string{"type": "number", "description": "Days to forecast (1-3)"},
				},
			}),
		},

		// === WEB ===
		{
			Name:        "web_search",
			Description: "Search the web using DuckDuckGo",
			InputSchema: jsonSchema(map[string]any{
				"type":       "object",
				"properties": map[string]any{"query": map[string]string{"type": "string", "description": "Search query"}},
				"required":   []string{"query"},
			}),
		},
		{
			Name:        "web_fetch",
			Description: "Fetch content from a URL",
			InputSchema: jsonSchema(map[string]any{
				"type":       "object",
				"properties": map[string]any{"url": map[string]string{"type": "string", "description": "URL to fetch"}},
				"required":   []string{"url"},
			}),
		},
		{
			Name:        "open_url",
			Description: "Open a URL in the default web browser",
			InputSchema: jsonSchema(map[string]any{
				"type":       "object",
				"properties": map[string]any{"url": map[string]string{"type": "string", "description": "URL to open"}},
				"required":   []string{"url"},
			}),
		},

		// === CLIPBOARD ===
		{
			Name:        "clipboard_read",
			Description: "Read content from the clipboard",
			InputSchema: jsonSchema(map[string]any{"type": "object", "properties": map[string]any{}}),
		},
		{
			Name:        "clipboard_write",
			Description: "Write content to the clipboard",
			InputSchema: jsonSchema(map[string]any{
				"type":       "object",
				"properties": map[string]any{"content": map[string]string{"type": "string", "description": "Content to copy"}},
				"required":   []string{"content"},
			}),
		},

		// === NOTIFICATIONS ===
		{
			Name:        "notification_send",
			Description: "Send a system notification",
			InputSchema: jsonSchema(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"title":    map[string]string{"type": "string", "description": "Notification title"},
					"message":  map[string]string{"type": "string", "description": "Notification message"},
					"subtitle": map[string]string{"type": "string", "description": "Subtitle (macOS only)"},
				},
				"required": []string{"title"},
			}),
		},

		// === SCREENSHOT ===
		{
			Name:        "screenshot",
			Description: "Capture a screenshot",
			InputSchema: jsonSchema(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]string{"type": "string", "description": "Save path (default: Desktop)"},
					"type": map[string]string{"type": "string", "description": "Type: fullscreen, window, or selection"},
				},
			}),
		},

		// === MUSIC ===
		{
			Name:        "music_play",
			Description: "Start or resume music playback",
			InputSchema: jsonSchema(map[string]any{"type": "object", "properties": map[string]any{}}),
		},
		{
			Name:        "music_pause",
			Description: "Pause music playback",
			InputSchema: jsonSchema(map[string]any{"type": "object", "properties": map[string]any{}}),
		},
		{
			Name:        "music_next",
			Description: "Skip to the next track",
			InputSchema: jsonSchema(map[string]any{"type": "object", "properties": map[string]any{}}),
		},
		{
			Name:        "music_previous",
			Description: "Go to the previous track",
			InputSchema: jsonSchema(map[string]any{"type": "object", "properties": map[string]any{}}),
		},
		{
			Name:        "music_now_playing",
			Description: "Get currently playing track info",
			InputSchema: jsonSchema(map[string]any{"type": "object", "properties": map[string]any{}}),
		},
		{
			Name:        "music_volume",
			Description: "Set music volume (0-100)",
			InputSchema: jsonSchema(map[string]any{
				"type":       "object",
				"properties": map[string]any{"volume": map[string]string{"type": "number", "description": "Volume level 0-100"}},
				"required":   []string{"volume"},
			}),
		},
		{
			Name:        "music_search",
			Description: "Search and play music in Spotify",
			InputSchema: jsonSchema(map[string]any{
				"type":       "object",
				"properties": map[string]any{"query": map[string]string{"type": "string", "description": "Search query (song, artist, album)"}},
				"required":   []string{"query"},
			}),
		},

		// === SYSTEM ===
		{
			Name:        "system_info",
			Description: "Get system information (CPU, memory, OS)",
			InputSchema: jsonSchema(map[string]any{"type": "object", "properties": map[string]any{}}),
		},
		{
			Name:        "shell_execute",
			Description: "Execute a shell command",
			InputSchema: jsonSchema(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"command": map[string]string{"type": "string", "description": "Command to execute"},
					"timeout": map[string]string{"type": "number", "description": "Timeout in seconds"},
				},
				"required": []string{"command"},
			}),
		},
		{
			Name:        "process_list",
			Description: "List running processes",
			InputSchema: jsonSchema(map[string]any{
				"type":       "object",
				"properties": map[string]any{"filter": map[string]string{"type": "string", "description": "Filter by name"}},
			}),
		},
	}
}

// processToolCalls executes tool calls and returns results
func (a *Agent) processToolCalls(ctx context.Context, toolCalls []ToolCall) []ToolResult {
	results := make([]ToolResult, 0, len(toolCalls))

	for _, tc := range toolCalls {
		result := a.executeTool(ctx, tc.Name, tc.Input)
		results = append(results, ToolResult{
			ToolCallID: tc.ID,
			Content:    result,
			IsError:    false,
		})
	}

	return results
}

// executeTool runs a tool and returns the result
func (a *Agent) executeTool(ctx context.Context, name string, input json.RawMessage) string {
	log.Printf("[Agent] Executing tool: %s", name)

	// Parse input arguments
	var args map[string]any
	if err := json.Unmarshal(input, &args); err != nil {
		return fmt.Sprintf("Error parsing arguments: %v", err)
	}

	// Call tools directly
	return callToolDirect(ctx, name, args)
}

// callToolDirect calls a tool directly
func callToolDirect(ctx context.Context, name string, args map[string]any) string {
	switch name {
	// File operations
	case "file_list":
		path := "."
		if p, ok := args["path"].(string); ok {
			path = p
		}
		return executeFileList(ctx, path)
	case "file_list_old":
		path := "."
		days := 30
		if p, ok := args["path"].(string); ok {
			path = p
		}
		if d, ok := args["days"].(float64); ok {
			days = int(d)
		}
		return executeFileListOld(ctx, path, days)

	// Calendar
	case "calendar_today":
		return executeCalendarToday(ctx)
	case "calendar_list_events":
		days := 7
		if d, ok := args["days"].(float64); ok {
			days = int(d)
		}
		return executeCalendarListEvents(ctx, days)
	case "calendar_create_event":
		return executeCalendarCreate(ctx, args)
	case "calendar_search":
		return executeCalendarSearch(ctx, args)
	case "calendar_delete":
		return executeCalendarDelete(ctx, args)

	// Reminders
	case "reminders_list":
		return executeRemindersToday(ctx)
	case "reminders_add":
		return executeRemindersAdd(ctx, args)
	case "reminders_complete":
		title := ""
		if t, ok := args["title"].(string); ok {
			title = t
		}
		return executeRemindersComplete(ctx, title)
	case "reminders_delete":
		title := ""
		if t, ok := args["title"].(string); ok {
			title = t
		}
		return executeRemindersDelete(ctx, title)

	// Notes
	case "notes_list":
		return executeNotesList(ctx, args)
	case "notes_read":
		title := ""
		if t, ok := args["title"].(string); ok {
			title = t
		}
		return executeNotesRead(ctx, title)
	case "notes_create":
		return executeNotesCreate(ctx, args)
	case "notes_search":
		keyword := ""
		if k, ok := args["keyword"].(string); ok {
			keyword = k
		}
		return executeNotesSearch(ctx, keyword)

	// Weather
	case "weather_current":
		location := ""
		if l, ok := args["location"].(string); ok {
			location = l
		}
		return executeWeatherCurrent(ctx, location)
	case "weather_forecast":
		location := ""
		days := 3
		if l, ok := args["location"].(string); ok {
			location = l
		}
		if d, ok := args["days"].(float64); ok {
			days = int(d)
		}
		return executeWeatherForecast(ctx, location, days)

	// Web
	case "web_search":
		query := ""
		if q, ok := args["query"].(string); ok {
			query = q
		}
		return executeWebSearch(ctx, query)
	case "web_fetch":
		url := ""
		if u, ok := args["url"].(string); ok {
			url = u
		}
		return executeWebFetch(ctx, url)
	case "open_url":
		url := ""
		if u, ok := args["url"].(string); ok {
			url = u
		}
		return executeOpenURL(ctx, url)

	// Clipboard
	case "clipboard_read":
		return executeClipboardRead(ctx)
	case "clipboard_write":
		content := ""
		if c, ok := args["content"].(string); ok {
			content = c
		}
		return executeClipboardWrite(ctx, content)

	// Notification
	case "notification_send":
		return executeNotificationSend(ctx, args)

	// Screenshot
	case "screenshot":
		return executeScreenshot(ctx, args)

	// Music
	case "music_play":
		return executeMusicPlay(ctx)
	case "music_pause":
		return executeMusicPause(ctx)
	case "music_next":
		return executeMusicNext(ctx)
	case "music_previous":
		return executeMusicPrevious(ctx)
	case "music_now_playing":
		return executeMusicNowPlaying(ctx)
	case "music_volume":
		volume := 50.0
		if v, ok := args["volume"].(float64); ok {
			volume = v
		}
		return executeMusicVolume(ctx, volume)
	case "music_search":
		query := ""
		if q, ok := args["query"].(string); ok {
			query = q
		}
		return executeMusicSearch(ctx, query)

	// System
	case "system_info":
		return executeSystemInfo(ctx)
	case "shell_execute":
		cmd := ""
		if c, ok := args["command"].(string); ok {
			cmd = c
		}
		return executeShell(ctx, cmd)

	default:
		return fmt.Sprintf("Tool '%s' not implemented", name)
	}
}

func jsonSchema(schema map[string]any) json.RawMessage {
	data, _ := json.Marshal(schema)
	return data
}
