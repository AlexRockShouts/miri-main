package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"miri-main/src/internal/tasks"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/google/uuid"
)

type TaskGateway interface {
	AddTask(t *tasks.Task) error
	DeleteTask(id string) error
	ListTasks() ([]*tasks.Task, error)
	GetTask(id string) (*tasks.Task, error)
	InstallSkill(ctx context.Context, name string) (string, error)
	ChannelSendFile(channel, device, filePath, caption string) error
}

type TaskManagerToolWrapper struct {
	gw        TaskGateway
	sessionID string
}

func NewTaskManagerTool(gw TaskGateway, sessionID string) *TaskManagerToolWrapper {
	return &TaskManagerToolWrapper{gw: gw, sessionID: sessionID}
}

func (t *TaskManagerToolWrapper) GetInfo() *schema.ToolInfo {
	return &schema.ToolInfo{
		Name: "task_manager",
		Desc: "Manage recurring tasks. You can add, delete, update, and list tasks. Tasks run based on a cron expression and execute a prompt. You can take the context of the current chat for the task description.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"action": {
				Type:     schema.String,
				Desc:     "The action to perform: 'add', 'delete', 'list', 'update'",
				Required: true,
			},
			"id": {
				Type:     schema.String,
				Desc:     "The ID of the task (required for delete, update)",
				Required: false,
			},
			"name": {
				Type:     schema.String,
				Desc:     "The name of the task",
				Required: false,
			},
			"cron": {
				Type:     schema.String,
				Desc:     "The cron expression for recurring execution (e.g., '0 0 * * * *' for every hour)",
				Required: false,
			},
			"prompt": {
				Type:     schema.String,
				Desc:     "The prompt to execute when the task runs",
				Required: false,
			},
			"needed_skills": {
				Type:     schema.Array,
				Desc:     "List of skill names required for this task",
				Required: false,
			},
			"active": {
				Type:     schema.Boolean,
				Desc:     "Whether the task is active",
				Required: false,
			},
		}),
	}
}

func (t *TaskManagerToolWrapper) Info(_ context.Context) (*schema.ToolInfo, error) {
	return t.GetInfo(), nil
}

func (t *TaskManagerToolWrapper) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	var args struct {
		Action       string   `json:"action"`
		ID           string   `json:"id"`
		Name         string   `json:"name"`
		Cron         string   `json:"cron"`
		Prompt       string   `json:"prompt"`
		NeededSkills []string `json:"needed_skills"`
		Active       *bool    `json:"active"`
	}
	if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
		return "", err
	}

	switch args.Action {
	case "add":
		if args.Cron == "" || args.Prompt == "" || args.Name == "" {
			return "", fmt.Errorf("name, cron, and prompt are required for add")
		}
		task := &tasks.Task{
			ID:             uuid.New().String()[:8],
			Name:           args.Name,
			CronExpression: args.Cron,
			Prompt:         args.Prompt,
			Active:         true,
			NeededSkills:   args.NeededSkills,
			Created:        time.Now(),
			Updated:        time.Now(),
			ReportSession:  t.sessionID,
		}
		if args.Active != nil {
			task.Active = *args.Active
		}

		// Pre-install skills if specified
		for _, s := range task.NeededSkills {
			_, _ = t.gw.InstallSkill(ctx, s)
		}

		if err := t.gw.AddTask(task); err != nil {
			return "", err
		}
		return fmt.Sprintf("Task added successfully with ID: %s", task.ID), nil

	case "list":
		taskList, err := t.gw.ListTasks()
		if err != nil {
			return "", err
		}
		b, _ := json.MarshalIndent(taskList, "", "  ")
		return string(b), nil

	case "delete":
		if args.ID == "" {
			return "", fmt.Errorf("id is required for delete")
		}
		if err := t.gw.DeleteTask(args.ID); err != nil {
			return "", err
		}
		return "Task deleted successfully", nil

	case "update":
		if args.ID == "" {
			return "", fmt.Errorf("id is required for update")
		}
		task, err := t.gw.GetTask(args.ID)
		if err != nil {
			return "", err
		}
		if args.Name != "" {
			task.Name = args.Name
		}
		if args.Cron != "" {
			task.CronExpression = args.Cron
		}
		if args.Prompt != "" {
			task.Prompt = args.Prompt
		}
		if args.NeededSkills != nil {
			task.NeededSkills = args.NeededSkills
			// Pre-install new skills
			for _, s := range task.NeededSkills {
				_, _ = t.gw.InstallSkill(ctx, s)
			}
		}
		if args.Active != nil {
			task.Active = *args.Active
		}
		task.Updated = time.Now()

		if err := t.gw.AddTask(task); err != nil {
			return "", err
		}
		return "Task updated successfully", nil

	default:
		return "", fmt.Errorf("invalid action: %s", args.Action)
	}
}
