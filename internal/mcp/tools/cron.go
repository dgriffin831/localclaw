package tools

import (
	"context"
	"fmt"
	"strconv"
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
	ID        string `json:"id"`
	Schedule  string `json:"schedule"`
	Command   string `json:"command"`
	CreatedAt string `json:"createdAt,omitempty"`
	UpdatedAt string `json:"updatedAt,omitempty"`
	LastRunAt string `json:"lastRunAt,omitempty"`
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
	if err := validateCronSchedule(schedule); err != nil {
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
	return CronRunResult{ID: res.ID, TriggeredAt: res.TriggeredAt}, nil
}

func fromCronEntry(entry cron.Entry) CronJob {
	return CronJob{
		ID:        entry.ID,
		Schedule:  entry.Schedule,
		Command:   entry.Command,
		CreatedAt: entry.CreatedAt,
		UpdatedAt: entry.UpdatedAt,
		LastRunAt: entry.LastRunAt,
	}
}

func validateCronSchedule(schedule string) error {
	value := strings.TrimSpace(schedule)
	if value == "" {
		return fmt.Errorf("schedule is required")
	}
	if strings.HasPrefix(value, "@") {
		switch value {
		case "@yearly", "@annually", "@monthly", "@weekly", "@daily", "@hourly", "@reboot":
			return nil
		default:
			return fmt.Errorf("schedule %q is not a supported macro", value)
		}
	}
	parts := strings.Fields(value)
	if len(parts) != 5 {
		return fmt.Errorf("schedule must use 5-field cron format")
	}
	ranges := [][2]int{
		{0, 59}, // minute
		{0, 23}, // hour
		{1, 31}, // day of month
		{1, 12}, // month
		{0, 7},  // day of week
	}
	for i, part := range parts {
		if err := validateCronField(part, ranges[i][0], ranges[i][1]); err != nil {
			return fmt.Errorf("invalid cron field %d: %w", i+1, err)
		}
	}
	return nil
}

func validateCronField(field string, min, max int) error {
	value := strings.TrimSpace(field)
	if value == "" {
		return fmt.Errorf("field cannot be blank")
	}
	for _, token := range strings.Split(value, ",") {
		if err := validateCronToken(strings.TrimSpace(token), min, max); err != nil {
			return err
		}
	}
	return nil
}

func validateCronToken(token string, min, max int) error {
	if token == "" {
		return fmt.Errorf("empty token")
	}
	if token == "*" {
		return nil
	}

	base := token
	step := 0
	if strings.Contains(token, "/") {
		parts := strings.Split(token, "/")
		if len(parts) != 2 {
			return fmt.Errorf("invalid step syntax %q", token)
		}
		base = strings.TrimSpace(parts[0])
		if base == "" {
			return fmt.Errorf("invalid step base in %q", token)
		}
		parsedStep, err := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err != nil || parsedStep <= 0 {
			return fmt.Errorf("step must be a positive integer in %q", token)
		}
		step = parsedStep
	}

	if base == "*" {
		return nil
	}
	if strings.Contains(base, "-") {
		parts := strings.Split(base, "-")
		if len(parts) != 2 {
			return fmt.Errorf("invalid range %q", token)
		}
		start, err := parseCronInt(strings.TrimSpace(parts[0]), min, max)
		if err != nil {
			return err
		}
		end, err := parseCronInt(strings.TrimSpace(parts[1]), min, max)
		if err != nil {
			return err
		}
		if start > end {
			return fmt.Errorf("range start %d exceeds end %d in %q", start, end, token)
		}
		if step > 0 && step > (end-start+1) {
			return fmt.Errorf("step %d is too large for range %q", step, token)
		}
		return nil
	}

	if _, err := parseCronInt(base, min, max); err != nil {
		return err
	}
	return nil
}

func parseCronInt(value string, min, max int) (int, error) {
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("value %q must be an integer", value)
	}
	if parsed < min || parsed > max {
		return 0, fmt.Errorf("value %d is out of range [%d,%d]", parsed, min, max)
	}
	return parsed, nil
}
