package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	cronpkg "github.com/kayz/coco/internal/cron"
	"github.com/kayz/coco/internal/logger"
	"github.com/kayz/coco/internal/router"
	"gopkg.in/yaml.v3"
)

const heartbeatJobTag = "heartbeat"

type heartbeatSpec struct {
	Enabled  bool            `yaml:"enabled"`
	Interval string          `yaml:"interval"`
	Tasks    []heartbeatTask `yaml:"tasks"`
	Checks   []heartbeatTask `yaml:"checks"`
}

type heartbeatTask struct {
	Name     string `yaml:"name"`
	Schedule string `yaml:"schedule"`
	Prompt   string `yaml:"prompt"`
	Notify   string `yaml:"notify"` // never (default) | always | on_change | auto
}

func (a *Agent) ensureHeartbeatJobsForConversation(msg router.Message) {
	if a == nil || a.cronScheduler == nil {
		return
	}
	if strings.TrimSpace(msg.Platform) == "" || strings.TrimSpace(msg.ChannelID) == "" || strings.TrimSpace(msg.UserID) == "" {
		return
	}

	spec, err := loadHeartbeatSpec()
	if err != nil {
		logger.Warn("[HEARTBEAT] Failed to load HEARTBEAT.md: %v", err)
		return
	}
	if spec == nil || !spec.Enabled || len(spec.Tasks) == 0 {
		return
	}

	existing := a.cronScheduler.ListJobsByTag(heartbeatJobTag)
	for idx, task := range spec.Tasks {
		name := strings.TrimSpace(task.Name)
		if name == "" {
			name = fmt.Sprintf("task-%d", idx+1)
		}
		schedule := strings.TrimSpace(task.Schedule)
		prompt := strings.TrimSpace(task.Prompt)
		if schedule == "" || prompt == "" {
			continue
		}

		jobName := heartbeatJobName(msg.UserID, name)
		platform, channelID, userID := resolveHeartbeatTarget(task, msg)
		if heartbeatJobExists(existing, jobName, platform, channelID, userID) {
			continue
		}

		_, err := a.cronScheduler.AddJobWithPromptAndTag(
			jobName,
			heartbeatJobTag,
			schedule,
			decorateHeartbeatPrompt(prompt, task.Notify),
			platform,
			channelID,
			userID,
		)
		if err != nil {
			logger.Warn("[HEARTBEAT] Failed to create heartbeat job %s: %v", jobName, err)
			continue
		}
		logger.Info("[HEARTBEAT] Heartbeat job created: %s (%s)", jobName, schedule)
	}
}

func heartbeatJobExists(jobs []*cronpkg.Job, name, platform, channelID, userID string) bool {
	for _, j := range jobs {
		if j.Name == name && j.Platform == platform && j.ChannelID == channelID && j.UserID == userID {
			return true
		}
	}
	return false
}

func resolveHeartbeatTarget(task heartbeatTask, msg router.Message) (platform, channelID, userID string) {
	_ = normalizeHeartbeatNotify(task.Notify)
	// Heartbeat always runs against current user/session context.
	// Whether to proactively notify is decided in scheduler by notify mode.
	return msg.Platform, msg.ChannelID, msg.UserID
}

func normalizeHeartbeatNotify(notify string) string {
	notify = strings.ToLower(strings.TrimSpace(notify))
	switch notify {
	case "always", "on_change", "auto", "never":
		return notify
	default:
		return "never"
	}
}

func decorateHeartbeatPrompt(prompt, notify string) string {
	notify = normalizeHeartbeatNotify(notify)
	return fmt.Sprintf("[HEARTBEAT_NOTIFY=%s]\n%s", notify, strings.TrimSpace(prompt))
}

func heartbeatJobName(userID, taskName string) string {
	return "heartbeat:" + sanitizeHeartbeatToken(userID) + ":" + sanitizeHeartbeatToken(taskName)
}

func sanitizeHeartbeatToken(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return "x"
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "x"
	}
	return out
}

func loadHeartbeatSpec() (*heartbeatSpec, error) {
	path := filepath.Join(getWorkspaceDir(), "HEARTBEAT.md")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	fm, _ := splitMarkdownFrontMatter(string(data))
	if strings.TrimSpace(fm) == "" {
		return nil, nil
	}

	spec := &heartbeatSpec{Enabled: true}
	if err := yaml.Unmarshal([]byte(fm), spec); err != nil {
		return nil, err
	}
	if !spec.Enabled {
		return spec, nil
	}

	if len(spec.Tasks) == 0 && len(spec.Checks) > 0 {
		spec.Tasks = spec.Checks
	}

	filtered := make([]heartbeatTask, 0, len(spec.Tasks))
	for _, task := range spec.Tasks {
		task.Schedule = normalizeHeartbeatSchedule(strings.TrimSpace(task.Schedule), strings.TrimSpace(spec.Interval))
		if strings.TrimSpace(task.Schedule) == "" || strings.TrimSpace(task.Prompt) == "" {
			continue
		}
		filtered = append(filtered, task)
	}
	spec.Tasks = filtered
	return spec, nil
}

func normalizeHeartbeatSchedule(taskSchedule, interval string) string {
	if taskSchedule != "" {
		return taskSchedule
	}
	interval = strings.TrimSpace(interval)
	if interval == "" {
		return ""
	}
	if strings.HasPrefix(interval, "@every ") || strings.HasPrefix(interval, "@daily") || strings.HasPrefix(interval, "@hourly") {
		return interval
	}
	return "@every " + interval
}

func splitMarkdownFrontMatter(content string) (frontMatter string, body string) {
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	normalized = strings.TrimSpace(normalized)
	if !strings.HasPrefix(normalized, "---\n") {
		return "", strings.TrimSpace(content)
	}
	rest := normalized[len("---\n"):]
	idx := strings.Index(rest, "\n---\n")
	if idx < 0 {
		return "", strings.TrimSpace(content)
	}
	return strings.TrimSpace(rest[:idx]), strings.TrimSpace(rest[idx+len("\n---\n"):])
}
