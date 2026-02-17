package tools

import (
	"context"
	"fmt"

	"github.com/dgriffin831/localclaw/internal/cron"
	"github.com/dgriffin831/localclaw/internal/mcp/protocol"
	"github.com/dgriffin831/localclaw/internal/runtime"
)

const (
	ToolLocalclawCronList   = "localclaw_cron_list"
	ToolLocalclawCronAdd    = "localclaw_cron_add"
	ToolLocalclawCronRemove = "localclaw_cron_remove"
	ToolLocalclawCronRun    = "localclaw_cron_run"
)

type CronJob struct {
	ID                string `json:"id"`
	Schedule          string `json:"schedule"`
	Command           string `json:"command"`
	CreatedAt         string `json:"createdAt,omitempty"`
	UpdatedAt         string `json:"updatedAt,omitempty"`
	LastRunAt         string `json:"lastRunAt,omitempty"`
	LastRunStatus     string `json:"lastRunStatus,omitempty"`
	LastRunExitCode   *int   `json:"lastRunExitCode,omitempty"`
	LastRunError      string `json:"lastRunError,omitempty"`
	LastRunDurationMs int64  `json:"lastRunDurationMs,omitempty"`
}

type CronListRequest struct{}

type CronAddRequest struct {
	ID       string
	Schedule string
	Command  string
}

type CronRemoveRequest struct {
	ID string
}

type CronRunRequest struct {
	ID string
}

type CronRunResult struct {
	ID          string `json:"id"`
	TriggeredAt string `json:"triggeredAt"`
	Status      string `json:"status"`
	ExitCode    *int   `json:"exitCode,omitempty"`
	Error       string `json:"error,omitempty"`
}

type CronBackend interface {
	List(ctx context.Context, req CronListRequest) ([]CronJob, error)
	Add(ctx context.Context, req CronAddRequest) (CronJob, error)
	Remove(ctx context.Context, req CronRemoveRequest) (bool, error)
	Run(ctx context.Context, req CronRunRequest) (CronRunResult, error)
}

type CronListTool struct{ backend CronBackend }
type CronAddTool struct{ backend CronBackend }
type CronRemoveTool struct{ backend CronBackend }
type CronRunTool struct{ backend CronBackend }

func NewCronListTool(backend CronBackend) CronListTool     { return CronListTool{backend: backend} }
func NewCronAddTool(backend CronBackend) CronAddTool       { return CronAddTool{backend: backend} }
func NewCronRemoveTool(backend CronBackend) CronRemoveTool { return CronRemoveTool{backend: backend} }
func NewCronRunTool(backend CronBackend) CronRunTool       { return CronRunTool{backend: backend} }

func CronListDefinition() protocol.Tool {
	return protocol.Tool{Name: ToolLocalclawCronList, Description: "List local cron jobs", InputSchema: map[string]interface{}{"type": "object"}}
}

func CronAddDefinition() protocol.Tool {
	return protocol.Tool{
		Name:        ToolLocalclawCronAdd,
		Description: "Add a local cron job",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"id":       map[string]interface{}{"type": "string"},
				"schedule": map[string]interface{}{"type": "string"},
				"command":  map[string]interface{}{"type": "string"},
			},
			"required": []string{"schedule", "command"},
		},
	}
}

func CronRemoveDefinition() protocol.Tool {
	return protocol.Tool{
		Name:        ToolLocalclawCronRemove,
		Description: "Remove a local cron job",
		InputSchema: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{"id": map[string]interface{}{"type": "string"}},
			"required":   []string{"id"},
		},
	}
}

func CronRunDefinition() protocol.Tool {
	return protocol.Tool{
		Name:        ToolLocalclawCronRun,
		Description: "Trigger a local cron job immediately",
		InputSchema: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{"id": map[string]interface{}{"type": "string"}},
			"required":   []string{"id"},
		},
	}
}

func (t CronListTool) Call(ctx context.Context, args map[string]interface{}) protocol.CallToolResult {
	jobs, err := t.backend.List(ctx, CronListRequest{})
	if err != nil {
		return errorResult(fmt.Errorf("cron_list failed: %w", err))
	}
	return protocol.CallToolResult{StructuredContent: map[string]interface{}{"ok": true, "jobs": jobs, "count": len(jobs)}}
}

func (t CronAddTool) Call(ctx context.Context, args map[string]interface{}) protocol.CallToolResult {
	id, err := optionalStringArg(args, "id")
	if err != nil {
		return errorResult(err)
	}
	schedule, err := requiredStringArg(args, "schedule")
	if err != nil {
		return errorResult(err)
	}
	command, err := requiredStringArg(args, "command")
	if err != nil {
		return errorResult(err)
	}
	if len(id) > 128 {
		return errorResult(fmt.Errorf("id cannot exceed 128 characters"))
	}
	if len(command) > 2048 {
		return errorResult(fmt.Errorf("command cannot exceed 2048 characters"))
	}
	if err := cron.ValidateSchedule(schedule); err != nil {
		return errorResult(err)
	}
	job, runErr := t.backend.Add(ctx, CronAddRequest{ID: id, Schedule: schedule, Command: command})
	if runErr != nil {
		return errorResult(fmt.Errorf("cron_add failed: %w", runErr))
	}
	return protocol.CallToolResult{StructuredContent: map[string]interface{}{"ok": true, "job": job}}
}

func (t CronRemoveTool) Call(ctx context.Context, args map[string]interface{}) protocol.CallToolResult {
	id, err := requiredStringArg(args, "id")
	if err != nil {
		return errorResult(err)
	}
	removed, runErr := t.backend.Remove(ctx, CronRemoveRequest{ID: id})
	if runErr != nil {
		return errorResult(fmt.Errorf("cron_remove failed: %w", runErr))
	}
	return protocol.CallToolResult{StructuredContent: map[string]interface{}{"ok": true, "removed": removed, "id": id}}
}

func (t CronRunTool) Call(ctx context.Context, args map[string]interface{}) protocol.CallToolResult {
	id, err := requiredStringArg(args, "id")
	if err != nil {
		return errorResult(err)
	}
	result, runErr := t.backend.Run(ctx, CronRunRequest{ID: id})
	if runErr != nil {
		return errorResult(fmt.Errorf("cron_run failed: %w", runErr))
	}
	return protocol.CallToolResult{StructuredContent: map[string]interface{}{"ok": true, "run": result}}
}

type RuntimeCronBackend struct {
	App *runtime.App
}

func (b RuntimeCronBackend) List(ctx context.Context, req CronListRequest) ([]CronJob, error) {
	entries, err := b.App.MCPCronList(ctx)
	if err != nil {
		return nil, err
	}
	jobs := make([]CronJob, 0, len(entries))
	for _, entry := range entries {
		jobs = append(jobs, fromCronEntry(entry))
	}
	return jobs, nil
}

func (b RuntimeCronBackend) Add(ctx context.Context, req CronAddRequest) (CronJob, error) {
	entry, err := b.App.MCPCronAdd(ctx, req.ID, req.Schedule, req.Command)
	if err != nil {
		return CronJob{}, err
	}
	return fromCronEntry(entry), nil
}

func (b RuntimeCronBackend) Remove(ctx context.Context, req CronRemoveRequest) (bool, error) {
	return b.App.MCPCronRemove(ctx, req.ID)
}

func (b RuntimeCronBackend) Run(ctx context.Context, req CronRunRequest) (CronRunResult, error) {
	res, err := b.App.MCPCronRun(ctx, req.ID)
	if err != nil {
		return CronRunResult{}, err
	}
	return CronRunResult{ID: res.ID, TriggeredAt: res.TriggeredAt, Status: res.Status, ExitCode: res.ExitCode, Error: res.Error}, nil
}

func fromCronEntry(entry cron.Entry) CronJob {
	return CronJob{
		ID:                entry.ID,
		Schedule:          entry.Schedule,
		Command:           entry.Command,
		CreatedAt:         entry.CreatedAt,
		UpdatedAt:         entry.UpdatedAt,
		LastRunAt:         entry.LastRunAt,
		LastRunStatus:     entry.LastRunStatus,
		LastRunExitCode:   entry.LastRunExitCode,
		LastRunError:      entry.LastRunError,
		LastRunDurationMs: entry.LastRunDurationMs,
	}
}
