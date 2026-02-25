package agent

import (
	"encoding/json"
	"fmt"
	"strings"

	cronpkg "github.com/kayz/coco/internal/cron"
)

// executeCronCreate creates a new scheduled task
func (a *Agent) executeCronCreate(args map[string]any) string {
	if a.cronScheduler == nil {
		return "Error: cron scheduler not available"
	}

	// Enforce: only ONE cron_create per user request
	a.cronCreatedCount++
	if a.cronCreatedCount > 1 {
		return "Error: You already created a cron job for this request. Only ONE cron job per user request is allowed. If you need varied/random content each time, use the 'prompt' parameter instead of creating multiple 'message' jobs."
	}

	name, _ := args["name"].(string)
	schedule, _ := args["schedule"].(string)
	message, _ := args["message"].(string)
	tool, _ := args["tool"].(string)
	prompt, _ := args["prompt"].(string)
	tag, _ := args["tag"].(string)

	if name == "" {
		return "Error: name is required"
	}
	if schedule == "" {
		return "Error: schedule is required"
	}

	// Auto-upgrade: if AI sent 'message' but no 'prompt' or 'tool',
	// wrap the message in a generation instruction so AI creates fresh content each time
	if message != "" && prompt == "" && tool == "" {
		prompt = fmt.Sprintf("用户想要定期收到类似以下风格的内容，请每次生成一条全新的、独特的、不重复的内容：\n%s", message)
		message = ""
	}

	var job *cronpkg.Job
	var err error

	// Prompt-based job: run full AI conversation on schedule
	if prompt != "" {
		if tag != "" {
			job, err = a.cronScheduler.AddJobWithPromptAndTag(
				name, tag, schedule, prompt,
				a.currentMsg.Platform, a.currentMsg.ChannelID, a.currentMsg.UserID,
			)
		} else {
			job, err = a.cronScheduler.AddJobWithPrompt(
				name, schedule, prompt,
				a.currentMsg.Platform, a.currentMsg.ChannelID, a.currentMsg.UserID,
			)
		}
		if err != nil {
			return fmt.Sprintf("Error creating scheduled task: %v", err)
		}
		return fmt.Sprintf("Scheduled AI task created:\n- ID: %s\n- Name: %s\n- Schedule: %s\n- Tag: %s\n- Prompt: %s", job.ID, job.Name, job.Schedule, job.Tag, job.Prompt)
	}

	// Message-based job
	if message != "" {
		if tag != "" {
			job, err = a.cronScheduler.AddJobWithMessageAndTag(
				name, tag, schedule, message,
				a.currentMsg.Platform, a.currentMsg.ChannelID, a.currentMsg.UserID,
			)
		} else {
			job, err = a.cronScheduler.AddJobWithMessage(
				name, schedule, message,
				a.currentMsg.Platform, a.currentMsg.ChannelID, a.currentMsg.UserID,
			)
		}
		if err != nil {
			return fmt.Sprintf("Error creating scheduled task: %v", err)
		}
		return fmt.Sprintf("Scheduled task created:\n- ID: %s\n- Name: %s\n- Schedule: %s\n- Tag: %s\n- Message: %s", job.ID, job.Name, job.Schedule, job.Tag, job.Message)
	}

	// Tool-based job
	if tool != "" {
		var arguments map[string]any
		if rawArgs, ok := args["arguments"]; ok {
			switch v := rawArgs.(type) {
			case map[string]any:
				arguments = v
			case string:
				// Try to parse JSON string
				if err := json.Unmarshal([]byte(v), &arguments); err != nil {
					return fmt.Sprintf("Error: invalid arguments JSON: %v", err)
				}
			}
		}
		if tag != "" {
			job, err = a.cronScheduler.AddJobWithTag(name, tag, schedule, tool, arguments)
		} else {
			job, err = a.cronScheduler.AddJob(name, schedule, tool, arguments)
		}
		if err != nil {
			return fmt.Sprintf("Error creating scheduled task: %v", err)
		}
		return fmt.Sprintf("Scheduled task created:\n- ID: %s\n- Name: %s\n- Schedule: %s\n- Tag: %s\n- Tool: %s", job.ID, job.Name, job.Schedule, job.Tag, job.Tool)
	}

	return "Error: either 'prompt', 'message', or 'tool' is required"
}

// executeCronList lists all scheduled tasks, optionally filtered by tag
func (a *Agent) executeCronList(args map[string]any) string {
	if a.cronScheduler == nil {
		return "Error: cron scheduler not available"
	}

	var jobs []*cronpkg.Job
	tag, _ := args["tag"].(string)
	
	if tag != "" {
		jobs = a.cronScheduler.ListJobsByTag(tag)
	} else {
		jobs = a.cronScheduler.ListJobs()
	}
	
	if len(jobs) == 0 {
		if tag != "" {
			return fmt.Sprintf("No scheduled tasks with tag '%s'.", tag)
		}
		return "No scheduled tasks."
	}

	var sb strings.Builder
	if tag != "" {
		sb.WriteString(fmt.Sprintf("Scheduled tasks with tag '%s' (%d):\n\n", tag, len(jobs)))
	} else {
		sb.WriteString(fmt.Sprintf("Scheduled tasks (%d):\n\n", len(jobs)))
	}
	
	for _, job := range jobs {
		status := "enabled"
		if !job.Enabled {
			status = "paused"
		}

		sb.WriteString(fmt.Sprintf("- ID: %s\n  Name: %s\n  Schedule: %s\n  Tag: %s\n  Status: %s\n", job.ID, job.Name, job.Schedule, job.Tag, status))
		if job.Prompt != "" {
			sb.WriteString(fmt.Sprintf("  Prompt: %s\n", job.Prompt))
		}
		if job.Message != "" {
			sb.WriteString(fmt.Sprintf("  Message: %s\n", job.Message))
		}
		if job.Tool != "" {
			sb.WriteString(fmt.Sprintf("  Tool: %s\n", job.Tool))
		}
		if job.LastRun != nil {
			sb.WriteString(fmt.Sprintf("  Last run: %s\n", job.LastRun.Format("2006-01-02 15:04:05")))
		}
		if job.LastError != "" {
			sb.WriteString(fmt.Sprintf("  Last error: %s\n", job.LastError))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// executeCronDelete deletes a scheduled task
func (a *Agent) executeCronDelete(args map[string]any) string {
	if a.cronScheduler == nil {
		return "Error: cron scheduler not available"
	}

	id, _ := args["id"].(string)
	if id == "" {
		return "Error: id is required"
	}

	if err := a.cronScheduler.RemoveJob(id); err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	return fmt.Sprintf("Scheduled task %s deleted.", id)
}

// executeCronPause pauses a scheduled task
func (a *Agent) executeCronPause(args map[string]any) string {
	if a.cronScheduler == nil {
		return "Error: cron scheduler not available"
	}

	id, _ := args["id"].(string)
	if id == "" {
		return "Error: id is required"
	}

	if err := a.cronScheduler.PauseJob(id); err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	return fmt.Sprintf("Scheduled task %s paused.", id)
}

// executeCronResume resumes a paused scheduled task
func (a *Agent) executeCronResume(args map[string]any) string {
	if a.cronScheduler == nil {
		return "Error: cron scheduler not available"
	}

	id, _ := args["id"].(string)
	if id == "" {
		return "Error: id is required"
	}

	if err := a.cronScheduler.ResumeJob(id); err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	return fmt.Sprintf("Scheduled task %s resumed.", id)
}
