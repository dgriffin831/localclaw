package tools

import (
	"context"
	"fmt"
	"strings"

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
	AgentID           string `json:"agentId,omitempty"`
	Schedule          string `json:"schedule"`
	SessionTarget     string `json:"sessionTarget"`
	WakeMode          string `json:"wakeMode"`
	Message           string `json:"message"`
	TimeoutSeconds    int    `json:"timeoutSeconds,omitempty"`
	CreatedAt         string `json:"createdAt,omitempty"`
	UpdatedAt         string `json:"updatedAt,omitempty"`
	LastRunAt         string `json:"lastRunAt,omitempty"`
	LastRunStatus     string `json:"lastRunStatus,omitempty"`
	LastRunError      string `json:"lastRunError,omitempty"`
	LastRunDurationMs int64  `json:"lastRunDurationMs,omitempty"`
}

type CronListRequest struct{}

type CronAddRequest struct {
	ID             string
	AgentID        string
	Schedule       string
	SessionTarget  string
	WakeMode       string
	Message        string
	TimeoutSeconds int
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
	return protocol.Tool{
		Name:        ToolLocalclawCronList,
		Description: "List configured local cron jobs. Use when you need job IDs or current schedules before run/remove/add actions.",
		InputSchema: map[string]interface{}{
			"type":        "object",
			"description": "No fields. Pass an empty object.",
			"examples":    []interface{}{map[string]interface{}{}},
		},
	}
}

func CronAddDefinition() protocol.Tool {
	return protocol.Tool{
		Name:        ToolLocalclawCronAdd,
		Description: "Add a local recurring prompt job. Use when you need scheduled agent prompts (not shell commands).",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"id":              schemaStringField("Optional stable job ID (max 128 chars). Omit to auto-generate.", "hourly-review"),
				"agent_id":        schemaStringField("Optional agent ID for execution context (max 128 chars).", "default"),
				"schedule":        schemaStringField("Cron schedule: 5-field format or supported macro (@hourly, @daily, @weekly, @monthly, @yearly, @annually, @reboot).", "*/5 * * * *"),
				"session_target":  schemaEnumStringField("Session target. default=shared session, isolated=job-specific session (default behavior).", []string{cron.SessionTargetDefault, cron.SessionTargetIsolated}, cron.SessionTargetIsolated),
				"wake_mode":       schemaEnumStringField("Wake mode. next-heartbeat queues for scheduler heartbeat; now attempts immediate wake.", []string{cron.WakeModeNextHeartbeat, cron.WakeModeNow}, cron.WakeModeNextHeartbeat),
				"message":         schemaStringField("Prompt text to execute on schedule; required and must be non-blank (max 8192 chars).", "Run a workspace health check and summarize blockers."),
				"timeout_seconds": schemaIntegerField("Per-run timeout in seconds; must be >= 0. Use 0 to keep scheduler default.", 60),
			},
			"required": []string{"schedule", "message"},
		},
	}
}

func CronRemoveDefinition() protocol.Tool {
	return protocol.Tool{
		Name:        ToolLocalclawCronRemove,
		Description: "Remove a local cron job by ID. Use when you want to stop a recurring job permanently.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"id": schemaStringField("Existing cron job ID to remove.", "hourly-review"),
			},
			"required": []string{"id"},
		},
	}
}

func CronRunDefinition() protocol.Tool {
	return protocol.Tool{
		Name:        ToolLocalclawCronRun,
		Description: "Trigger a local cron job immediately. Use when you need a one-off run for testing or urgent execution.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"id": schemaStringField("Existing cron job ID to trigger now.", "hourly-review"),
			},
			"required": []string{"id"},
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
	agentID, err := optionalStringArgWithAlias(args, "agent_id", "agentId")
	if err != nil {
		return errorResult(err)
	}
	schedule, err := requiredStringArg(args, "schedule")
	if err != nil {
		return errorResult(err)
	}
	sessionTarget, err := optionalStringArgWithAlias(args, "session_target", "sessionTarget")
	if err != nil {
		return errorResult(err)
	}
	wakeMode, err := optionalStringArgWithAlias(args, "wake_mode", "wakeMode")
	if err != nil {
		return errorResult(err)
	}
	message, err := optionalStringArgWithAlias(args, "message", "")
	if err != nil {
		return errorResult(err)
	}
	if message == "" {
		message, err = legacyPayloadMessage(args)
		if err != nil {
			return errorResult(err)
		}
	}
	if message == "" {
		return errorResult(fmt.Errorf("message is required"))
	}
	message = strings.TrimSpace(message)
	if message == "" {
		return errorResult(fmt.Errorf("message is required"))
	}
	timeoutSeconds, err := optionalIntArgWithAlias(args, "timeout_seconds", "timeoutSeconds")
	if err != nil {
		return errorResult(fmt.Errorf("timeout_seconds must be an integer"))
	}
	if timeoutSeconds == 0 {
		timeoutSeconds, err = legacyPayloadTimeout(args)
		if err != nil {
			return errorResult(err)
		}
	}

	if len(id) > 128 {
		return errorResult(fmt.Errorf("id cannot exceed 128 characters"))
	}
	if len(agentID) > 128 {
		return errorResult(fmt.Errorf("agent_id cannot exceed 128 characters"))
	}
	if len(message) > 8192 {
		return errorResult(fmt.Errorf("message cannot exceed 8192 characters"))
	}
	if timeoutSeconds < 0 {
		return errorResult(fmt.Errorf("timeout_seconds must be >= 0"))
	}
	if err := cron.ValidateSchedule(schedule); err != nil {
		return errorResult(err)
	}

	job, runErr := t.backend.Add(ctx, CronAddRequest{
		ID:             id,
		AgentID:        agentID,
		Schedule:       schedule,
		SessionTarget:  sessionTarget,
		WakeMode:       wakeMode,
		Message:        message,
		TimeoutSeconds: timeoutSeconds,
	})
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

func optionalStringArgWithAlias(args map[string]interface{}, primary, fallback string) (string, error) {
	if _, ok := args[primary]; ok {
		return optionalStringArg(args, primary)
	}
	if fallback != "" {
		if _, ok := args[fallback]; ok {
			return optionalStringArg(args, fallback)
		}
	}
	return "", nil
}

func optionalIntArgWithAlias(args map[string]interface{}, primary, fallback string) (int, error) {
	if _, ok := args[primary]; ok {
		return optionalIntArg(args, primary)
	}
	if fallback != "" {
		if _, ok := args[fallback]; ok {
			return optionalIntArg(args, fallback)
		}
	}
	return 0, nil
}

func legacyPayloadMessage(args map[string]interface{}) (string, error) {
	payloadRaw, ok := args["payload"]
	if !ok {
		return "", nil
	}
	payloadMap, ok := payloadRaw.(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("payload must be an object")
	}
	message, err := optionalStringArgWithAlias(payloadMap, "message", "")
	if err != nil {
		return "", fmt.Errorf("payload.message must be a string")
	}
	if strings.TrimSpace(message) != "" {
		return strings.TrimSpace(message), nil
	}
	text, err := optionalStringArgWithAlias(payloadMap, "text", "")
	if err != nil {
		return "", fmt.Errorf("payload.text must be a string")
	}
	return strings.TrimSpace(text), nil
}

func legacyPayloadTimeout(args map[string]interface{}) (int, error) {
	payloadRaw, ok := args["payload"]
	if !ok {
		return 0, nil
	}
	payloadMap, ok := payloadRaw.(map[string]interface{})
	if !ok {
		return 0, fmt.Errorf("payload must be an object")
	}
	timeoutSeconds, err := optionalIntArgWithAlias(payloadMap, "timeout_seconds", "timeoutSeconds")
	if err != nil {
		return 0, fmt.Errorf("payload timeout must be an integer")
	}
	return timeoutSeconds, nil
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
	entry, err := b.App.MCPCronAdd(ctx, cron.AddRequest{
		ID:             req.ID,
		AgentID:        req.AgentID,
		Schedule:       req.Schedule,
		SessionTarget:  req.SessionTarget,
		WakeMode:       req.WakeMode,
		Message:        req.Message,
		TimeoutSeconds: req.TimeoutSeconds,
	})
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
	return CronRunResult{ID: res.ID, TriggeredAt: res.TriggeredAt, Status: res.Status, Error: res.Error}, nil
}

func fromCronEntry(entry cron.Entry) CronJob {
	return CronJob{
		ID:                entry.ID,
		AgentID:           entry.AgentID,
		Schedule:          entry.Schedule,
		SessionTarget:     entry.SessionTarget,
		WakeMode:          entry.WakeMode,
		Message:           entry.Message,
		TimeoutSeconds:    entry.TimeoutSeconds,
		CreatedAt:         entry.CreatedAt,
		UpdatedAt:         entry.UpdatedAt,
		LastRunAt:         entry.LastRunAt,
		LastRunStatus:     entry.LastRunStatus,
		LastRunError:      entry.LastRunError,
		LastRunDurationMs: entry.LastRunDurationMs,
	}
}
