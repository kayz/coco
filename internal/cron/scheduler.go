package cron

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/robfig/cron/v3"
)

// ToolExecutor interface for executing MCP tools
type ToolExecutor interface {
	ExecuteTool(ctx context.Context, toolName string, arguments map[string]any) (any, error)
}

// PromptExecutor interface for running full AI conversations
type PromptExecutor interface {
	ExecutePrompt(ctx context.Context, platform, channelID, userID, prompt string) (string, error)
}

// ChatNotifier interface for sending messages to chat
type ChatNotifier interface {
	NotifyChat(message string) error
	NotifyChatUser(platform, channelID, userID, message string) error
}

// Scheduler manages scheduled jobs
type Scheduler struct {
	cron           *cron.Cron
	store          *Store
	toolExecutor   ToolExecutor
	promptExecutor PromptExecutor
	chatNotifier   ChatNotifier
	jobs           map[string]*Job
	mu             sync.RWMutex
}

// NewScheduler creates a new scheduler
func NewScheduler(store *Store, toolExecutor ToolExecutor, promptExecutor PromptExecutor, chatNotifier ChatNotifier) *Scheduler {
	return &Scheduler{
		cron:           cron.New(cron.WithSeconds()), // Support second-level precision
		store:          store,
		toolExecutor:   toolExecutor,
		promptExecutor: promptExecutor,
		chatNotifier:   chatNotifier,
		jobs:           make(map[string]*Job),
	}
}

// normalizeCron prepends "0 " to standard 5-field cron expressions
// so they work with the 6-field (with seconds) parser.
func normalizeCron(schedule string) string {
	if len(strings.Fields(schedule)) == 5 {
		return "0 " + schedule
	}
	return schedule
}

// Start loads jobs from storage and starts the scheduler
func (s *Scheduler) Start() error {
	// Load jobs from disk
	jobs, err := s.store.Load()
	if err != nil {
		return fmt.Errorf("failed to load jobs: %w", err)
	}

	// Schedule enabled jobs
	for _, job := range jobs {
		s.jobs[job.ID] = job
		if job.Enabled {
			if err := s.scheduleJob(job); err != nil {
				log.Printf("[CRON] Failed to schedule job %s (%s): %v", job.ID, job.Name, err)
			}
		}
	}

	// Start the cron scheduler
	s.cron.Start()
	log.Printf("[CRON] Scheduler started with %d jobs (%d enabled)", len(s.jobs), s.countEnabled())

	return nil
}

// Stop stops the scheduler and closes the store
func (s *Scheduler) Stop() error {
	// Stop the cron scheduler
	ctx := s.cron.Stop()
	<-ctx.Done()

	// Close the database
	if err := s.store.Close(); err != nil {
		return fmt.Errorf("failed to close store: %w", err)
	}

	log.Printf("[CRON] Scheduler stopped")
	return nil
}

// AddJob adds a new tool-based job to the scheduler
func (s *Scheduler) AddJob(name, schedule, tool string, arguments map[string]any) (*Job, error) {
	return s.addJob(&Job{
		Name:      name,
		Schedule:  schedule,
		Tool:      tool,
		Arguments: arguments,
	})
}

// AddJobWithMessage adds a new message-based job that sends text to a chat user
func (s *Scheduler) AddJobWithMessage(name, schedule, message, platform, channelID, userID string) (*Job, error) {
	return s.addJob(&Job{
		Name:      name,
		Schedule:  schedule,
		Message:   message,
		Platform:  platform,
		ChannelID: channelID,
		UserID:    userID,
	})
}

// AddJobWithPrompt adds a new prompt-based job that runs a full AI conversation
func (s *Scheduler) AddJobWithPrompt(name, schedule, prompt, platform, channelID, userID string) (*Job, error) {
	return s.addJob(&Job{
		Name:      name,
		Schedule:  schedule,
		Prompt:    prompt,
		Platform:  platform,
		ChannelID: channelID,
		UserID:    userID,
	})
}

// AddJobWithTag adds a new tool-based job with a tag
func (s *Scheduler) AddJobWithTag(name, tag, schedule, tool string, arguments map[string]any) (*Job, error) {
	return s.addJob(&Job{
		Name:      name,
		Tag:       tag,
		Schedule:  schedule,
		Tool:      tool,
		Arguments: arguments,
	})
}

// AddJobWithMessageAndTag adds a new message-based job with a tag
func (s *Scheduler) AddJobWithMessageAndTag(name, tag, schedule, message, platform, channelID, userID string) (*Job, error) {
	return s.addJob(&Job{
		Name:      name,
		Tag:       tag,
		Schedule:  schedule,
		Message:   message,
		Platform:  platform,
		ChannelID: channelID,
		UserID:    userID,
	})
}

// AddJobWithPromptAndTag adds a new prompt-based job with a tag
func (s *Scheduler) AddJobWithPromptAndTag(name, tag, schedule, prompt, platform, channelID, userID string) (*Job, error) {
	return s.addJob(&Job{
		Name:      name,
		Tag:       tag,
		Type:      "prompt",
		Schedule:  schedule,
		Prompt:    prompt,
		Platform:  platform,
		ChannelID: channelID,
		UserID:    userID,
	})
}

// AddExternalJob creates a scheduled external-agent HTTP invocation job.
func (s *Scheduler) AddExternalJob(name, tag, schedule, endpoint, authHeader string, relayMode bool, arguments map[string]any, platform, channelID, userID string) (*Job, error) {
	return s.addJob(&Job{
		Name:       name,
		Tag:        tag,
		Type:       "external",
		Schedule:   schedule,
		Endpoint:   endpoint,
		AuthHeader: authHeader,
		RelayMode:  relayMode,
		Source:     "external-agent",
		Arguments:  arguments,
		Platform:   platform,
		ChannelID:  channelID,
		UserID:     userID,
	})
}

// ListJobsByTag returns jobs filtered by tag
func (s *Scheduler) ListJobsByTag(tag string) []*Job {
	s.mu.RLock()
	defer s.mu.RUnlock()

	jobs := make([]*Job, 0)
	for _, job := range s.jobs {
		if tag == "" || job.Tag == tag {
			jobs = append(jobs, job.Clone())
		}
	}

	return jobs
}

// addJob validates and schedules a job
func (s *Scheduler) addJob(job *Job) (*Job, error) {
	// Normalize 5-field cron to 6-field (our cron instance uses WithSeconds)
	job.Schedule = normalizeCron(job.Schedule)

	// Validate cron expression using the 6-field (with seconds) parser
	parser := cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
	if _, err := parser.Parse(job.Schedule); err != nil {
		return nil, fmt.Errorf("invalid cron expression: %w", err)
	}

	job.ID = uuid.New().String()
	job.Enabled = true
	job.CreatedAt = time.Now()
	if strings.TrimSpace(job.Type) == "" {
		switch {
		case strings.TrimSpace(job.Endpoint) != "":
			job.Type = "external"
		case strings.TrimSpace(job.Prompt) != "":
			job.Type = "prompt"
		case strings.TrimSpace(job.Message) != "":
			job.Type = "message"
		default:
			job.Type = "tool"
		}
	}
	if strings.TrimSpace(job.Source) == "" && job.Type == "external" {
		job.Source = "external-agent"
	}

	// Add to jobs map
	s.mu.Lock()
	s.jobs[job.ID] = job
	s.mu.Unlock()

	// Schedule the job
	if err := s.scheduleJob(job); err != nil {
		s.mu.Lock()
		delete(s.jobs, job.ID)
		s.mu.Unlock()
		return nil, fmt.Errorf("failed to schedule job: %w", err)
	}

	// Save to database
	if err := s.store.SaveJob(job); err != nil {
		log.Printf("[CRON] Failed to save job: %v", err)
	}

	log.Printf("[CRON] Job created: %s (%s) - schedule: %s, tool: %s", job.ID, job.Name, job.Schedule, job.Tool)
	return job, nil
}

// RemoveJob removes a job from the scheduler
func (s *Scheduler) RemoveJob(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	job, exists := s.jobs[id]
	if !exists {
		return fmt.Errorf("job not found: %s", id)
	}

	// Remove from cron scheduler if it has an entry
	if job.EntryID != 0 {
		s.cron.Remove(job.EntryID)
	}

	// Remove from jobs map
	delete(s.jobs, id)

	// Delete from database
	if err := s.store.DeleteJob(id); err != nil {
		log.Printf("[CRON] Failed to delete job: %v", err)
	}

	log.Printf("[CRON] Job removed: %s (%s)", job.ID, job.Name)
	return nil
}

// PauseJob pauses a job
func (s *Scheduler) PauseJob(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	job, exists := s.jobs[id]
	if !exists {
		return fmt.Errorf("job not found: %s", id)
	}

	if !job.Enabled {
		return fmt.Errorf("job is already paused")
	}

	// Remove from cron scheduler
	if job.EntryID != 0 {
		s.cron.Remove(job.EntryID)
		job.EntryID = 0
	}

	job.Enabled = false

	// Save to database
	if err := s.store.SaveJob(job); err != nil {
		log.Printf("[CRON] Failed to save job: %v", err)
	}

	log.Printf("[CRON] Job paused: %s (%s)", job.ID, job.Name)
	return nil
}

// ResumeJob resumes a paused job
func (s *Scheduler) ResumeJob(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	job, exists := s.jobs[id]
	if !exists {
		return fmt.Errorf("job not found: %s", id)
	}

	if job.Enabled {
		return fmt.Errorf("job is already running")
	}

	job.Enabled = true

	// Schedule the job
	s.mu.Unlock()
	err := s.scheduleJob(job)
	s.mu.Lock()

	if err != nil {
		job.Enabled = false
		return fmt.Errorf("failed to schedule job: %w", err)
	}

	// Save to database
	if err := s.store.SaveJob(job); err != nil {
		log.Printf("[CRON] Failed to save job: %v", err)
	}

	log.Printf("[CRON] Job resumed: %s (%s)", job.ID, job.Name)
	return nil
}

// ListJobs returns all jobs
func (s *Scheduler) ListJobs() []*Job {
	s.mu.RLock()
	defer s.mu.RUnlock()

	jobs := make([]*Job, 0, len(s.jobs))
	for _, job := range s.jobs {
		jobs = append(jobs, job.Clone())
	}

	return jobs
}

// scheduleJob schedules a job in the cron scheduler
func (s *Scheduler) scheduleJob(job *Job) error {
	entryID, err := s.cron.AddFunc(job.Schedule, func() {
		s.executeJob(job)
	})
	if err != nil {
		return err
	}

	job.EntryID = entryID
	return nil
}

// executeJob executes a job
func (s *Scheduler) executeJob(job *Job) {
	now := time.Now()

	// External-agent job: call external endpoint with JSON payload.
	if job.Type == "external" || job.Endpoint != "" {
		log.Printf("[CRON] Running external job: %s (%s) -> %s", job.ID, job.Name, job.Endpoint)

		s.mu.Lock()
		job.LastRun = &now
		s.mu.Unlock()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		text, err := s.executeExternalJob(ctx, job)
		if err != nil {
			s.mu.Lock()
			job.LastError = err.Error()
			s.mu.Unlock()
			log.Printf("[CRON] External job failed: %s (%s) - error: %v", job.ID, job.Name, err)
			if s.chatNotifier != nil && job.Platform != "" && job.ChannelID != "" {
				s.chatNotifier.NotifyChatUser(job.Platform, job.ChannelID, job.UserID,
					fmt.Sprintf("⚠️ External job '%s' failed: %v", job.Name, err))
			}
		} else {
			s.mu.Lock()
			job.LastError = ""
			s.mu.Unlock()
			log.Printf("[CRON] External job completed: %s (%s)", job.ID, job.Name)
			if s.chatNotifier != nil && job.Platform != "" && job.ChannelID != "" && strings.TrimSpace(text) != "" {
				s.chatNotifier.NotifyChatUser(job.Platform, job.ChannelID, job.UserID, text)
			}
		}

		if err := s.store.SaveJob(job); err != nil {
			log.Printf("[CRON] Failed to save job: %v", err)
		}
		return
	}

	// Message-based job: send message directly to user
	if job.Message != "" {
		log.Printf("[CRON] Sending message for job: %s (%s)", job.ID, job.Name)

		s.mu.Lock()
		job.LastRun = &now
		s.mu.Unlock()

		if s.chatNotifier != nil && job.Platform != "" && job.ChannelID != "" {
			if err := s.chatNotifier.NotifyChatUser(job.Platform, job.ChannelID, job.UserID, job.Message); err != nil {
				s.mu.Lock()
				job.LastError = err.Error()
				s.mu.Unlock()
				log.Printf("[CRON] Job failed to send message: %s (%s) - error: %v", job.ID, job.Name, err)
			} else {
				s.mu.Lock()
				job.LastError = ""
				s.mu.Unlock()
				log.Printf("[CRON] Job message sent: %s (%s)", job.ID, job.Name)
			}
		} else {
			log.Printf("[CRON] Job %s has no chat target, logging message: %s", job.ID, job.Message)
			if s.chatNotifier != nil {
				s.chatNotifier.NotifyChat(fmt.Sprintf("[%s] %s", job.Name, job.Message))
			}
		}

		if err := s.store.SaveJob(job); err != nil {
			log.Printf("[CRON] Failed to save job: %v", err)
		}
		return
	}

	// Prompt-based job: run full AI conversation
	if job.Prompt != "" {
		log.Printf("[CRON] Running AI prompt for job: %s (%s)", job.ID, job.Name)

		s.mu.Lock()
		job.LastRun = &now
		s.mu.Unlock()

		if s.promptExecutor == nil {
			s.mu.Lock()
			job.LastError = "prompt executor not available"
			s.mu.Unlock()
			log.Printf("[CRON] Job failed: %s (%s) - prompt executor not available", job.ID, job.Name)
			if err := s.store.SaveJob(job); err != nil {
				log.Printf("[CRON] Failed to save job: %v", err)
			}
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		promptToRun := job.Prompt
		heartbeatNotifyMode := ""
		if job.Tag == "heartbeat" {
			heartbeatNotifyMode, promptToRun = parseHeartbeatPromptMeta(job.Prompt)
			if heartbeatNotifyMode == "auto" {
				promptToRun = buildHeartbeatAutoPrompt(promptToRun)
			}
		}

		result, err := s.promptExecutor.ExecutePrompt(ctx, job.Platform, job.ChannelID, job.UserID, promptToRun)
		if err != nil {
			s.mu.Lock()
			job.LastError = err.Error()
			s.mu.Unlock()
			log.Printf("[CRON] Job prompt failed: %s (%s) - error: %v", job.ID, job.Name, err)

			if s.chatNotifier != nil && job.Platform != "" && job.ChannelID != "" {
				s.chatNotifier.NotifyChatUser(job.Platform, job.ChannelID, job.UserID,
					fmt.Sprintf("⚠️ Scheduled AI task '%s' failed: %v", job.Name, err))
			}
		} else {
			s.mu.Lock()
			job.LastError = ""
			s.mu.Unlock()
			log.Printf("[CRON] Job prompt completed: %s (%s)", job.ID, job.Name)

			text := strings.TrimSpace(result)
			shouldNotify := s.chatNotifier != nil && job.Platform != "" && job.ChannelID != ""
			if job.Tag == "heartbeat" {
				shouldNotify, text = decideHeartbeatNotification(job, heartbeatNotifyMode, result)
			}
			if shouldNotify && text != "" {
				s.chatNotifier.NotifyChatUser(job.Platform, job.ChannelID, job.UserID, text)
			}
		}

		if err := s.store.SaveJob(job); err != nil {
			log.Printf("[CRON] Failed to save job: %v", err)
		}
		return
	}

	// Tool-based job: execute MCP tool
	log.Printf("[CRON] Executing job: %s (%s) - tool: %s", job.ID, job.Name, job.Tool)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	result, err := s.toolExecutor.ExecuteTool(ctx, job.Tool, job.Arguments)

	// Update job status
	s.mu.Lock()
	job.LastRun = &now
	if err != nil {
		job.LastError = err.Error()
		s.mu.Unlock()

		log.Printf("[CRON] Job failed: %s (%s) - error: %v", job.ID, job.Name, err)

		if s.chatNotifier != nil {
			errMsg := fmt.Sprintf("⚠️ Scheduled job '%s' failed: %v", job.Name, err)
			if job.Platform != "" && job.ChannelID != "" {
				s.chatNotifier.NotifyChatUser(job.Platform, job.ChannelID, job.UserID, errMsg)
			} else {
				s.chatNotifier.NotifyChat(errMsg)
			}
		}
	} else {
		job.LastError = ""
		s.mu.Unlock()

		resultStr := ""
		if result != nil {
			if resultJSON, err := json.Marshal(result); err == nil {
				resultStr = fmt.Sprintf(" - result: %s", string(resultJSON))
			}
		}
		log.Printf("[CRON] Job completed: %s (%s)%s", job.ID, job.Name, resultStr)
	}

	if err := s.store.SaveJob(job); err != nil {
		log.Printf("[CRON] Failed to save job: %v", err)
	}
}

func (s *Scheduler) executeExternalJob(ctx context.Context, job *Job) (string, error) {
	if strings.TrimSpace(job.Endpoint) == "" {
		return "", fmt.Errorf("external endpoint is required")
	}

	payload := map[string]any{
		"id":         job.ID,
		"name":       job.Name,
		"type":       "external",
		"tag":        job.Tag,
		"source":     job.Source,
		"schedule":   job.Schedule,
		"arguments":  job.Arguments,
		"platform":   job.Platform,
		"channel_id": job.ChannelID,
		"user_id":    job.UserID,
		"triggered":  time.Now().Format(time.RFC3339),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, job.Endpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Coco-Source", "external-agent")
	if strings.TrimSpace(job.AuthHeader) != "" {
		req.Header.Set("Authorization", job.AuthHeader)
	}

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("external endpoint returned status %d", resp.StatusCode)
	}

	var result struct {
		Text    string `json:"text"`
		Message string `json:"message"`
	}
	decoder := json.NewDecoder(resp.Body)
	if err := decoder.Decode(&result); err != nil {
		// If response is not JSON, best effort return empty text.
		return "", nil
	}

	text := strings.TrimSpace(result.Text)
	if text == "" {
		text = strings.TrimSpace(result.Message)
	}
	if text == "" {
		return "", nil
	}
	if job.RelayMode {
		return fmt.Sprintf("[external-agent] %s", text), nil
	}
	return text, nil
}

func parseHeartbeatPromptMeta(prompt string) (notifyMode string, cleanPrompt string) {
	notifyMode = "never"
	cleanPrompt = strings.TrimSpace(prompt)
	if cleanPrompt == "" {
		return notifyMode, cleanPrompt
	}
	line := cleanPrompt
	rest := ""
	if idx := strings.IndexByte(cleanPrompt, '\n'); idx >= 0 {
		line = strings.TrimSpace(cleanPrompt[:idx])
		rest = strings.TrimSpace(cleanPrompt[idx+1:])
	}
	const prefix = "[HEARTBEAT_NOTIFY="
	if strings.HasPrefix(line, prefix) && strings.HasSuffix(line, "]") {
		mode := strings.TrimSuffix(strings.TrimPrefix(line, prefix), "]")
		notifyMode = normalizeHeartbeatNotifyMode(mode)
		cleanPrompt = rest
	}
	return notifyMode, cleanPrompt
}

func normalizeHeartbeatNotifyMode(mode string) string {
	mode = strings.ToLower(strings.TrimSpace(mode))
	switch mode {
	case "never", "always", "on_change", "auto":
		return mode
	default:
		return "never"
	}
}

func buildHeartbeatAutoPrompt(prompt string) string {
	instruction := `你正在执行 HEARTBEAT 巡检。
请在第一行严格输出：HEARTBEAT_NOTIFY: yes 或 HEARTBEAT_NOTIFY: no
然后再输出巡检正文（可多行）。`
	return instruction + "\n\n" + strings.TrimSpace(prompt)
}

func decideHeartbeatNotification(job *Job, notifyMode string, rawResult string) (bool, string) {
	notifyMode = normalizeHeartbeatNotifyMode(notifyMode)
	text := strings.TrimSpace(rawResult)
	explicitAuto, body, hasDecision := parseHeartbeatAutoDecision(text)
	if hasDecision {
		text = body
	}

	hash := heartbeatResultHash(text)
	prevHash := heartbeatStoredHash(job.Source)
	if hash != "" {
		job.Source = heartbeatHashSource(hash)
	}

	switch notifyMode {
	case "always":
		return text != "", text
	case "never":
		return false, text
	case "on_change":
		// First run establishes baseline and does not notify.
		if prevHash == "" {
			return false, text
		}
		return hash != "" && hash != prevHash, text
	case "auto":
		if hasDecision {
			return explicitAuto && text != "", text
		}
		if prevHash == "" {
			return false, text
		}
		return hash != "" && hash != prevHash, text
	default:
		return false, text
	}
}

func parseHeartbeatAutoDecision(result string) (notify bool, body string, hasDecision bool) {
	result = strings.ReplaceAll(result, "\r\n", "\n")
	result = strings.TrimSpace(result)
	if result == "" {
		return false, "", false
	}
	first := result
	rest := ""
	if idx := strings.IndexByte(result, '\n'); idx >= 0 {
		first = strings.TrimSpace(result[:idx])
		rest = strings.TrimSpace(result[idx+1:])
	}
	if !strings.HasPrefix(strings.ToUpper(first), "HEARTBEAT_NOTIFY:") {
		return false, result, false
	}
	raw := strings.TrimSpace(first[len("HEARTBEAT_NOTIFY:"):])
	switch strings.ToLower(raw) {
	case "yes", "true", "1":
		return true, rest, true
	case "no", "false", "0":
		return false, rest, true
	default:
		return false, result, false
	}
}

func heartbeatResultHash(text string) string {
	normalized := strings.TrimSpace(strings.ReplaceAll(text, "\r\n", "\n"))
	if normalized == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(sum[:])
}

func heartbeatStoredHash(source string) string {
	const prefix = "heartbeat:last_hash="
	source = strings.TrimSpace(source)
	if !strings.HasPrefix(source, prefix) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(source, prefix))
}

func heartbeatHashSource(hash string) string {
	if strings.TrimSpace(hash) == "" {
		return ""
	}
	return "heartbeat:last_hash=" + hash
}

// countEnabled returns the number of enabled jobs
func (s *Scheduler) countEnabled() int {
	count := 0
	for _, job := range s.jobs {
		if job.Enabled {
			count++
		}
	}
	return count
}
