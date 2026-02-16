package tools

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type stubCronBackend struct {
	listFn   func(ctx context.Context, req CronListRequest) ([]CronJob, error)
	addFn    func(ctx context.Context, req CronAddRequest) (CronJob, error)
	removeFn func(ctx context.Context, req CronRemoveRequest) (bool, error)
	runFn    func(ctx context.Context, req CronRunRequest) (CronRunResult, error)
}

func (s stubCronBackend) List(ctx context.Context, req CronListRequest) ([]CronJob, error) {
	return s.listFn(ctx, req)
}

func (s stubCronBackend) Add(ctx context.Context, req CronAddRequest) (CronJob, error) {
	return s.addFn(ctx, req)
}

func (s stubCronBackend) Remove(ctx context.Context, req CronRemoveRequest) (bool, error) {
	return s.removeFn(ctx, req)
}

func (s stubCronBackend) Run(ctx context.Context, req CronRunRequest) (CronRunResult, error) {
	return s.runFn(ctx, req)
}

func TestCronAddToolRejectsInvalidSchedule(t *testing.T) {
	h := NewCronAddTool(stubCronBackend{addFn: func(ctx context.Context, req CronAddRequest) (CronJob, error) {
		return CronJob{}, nil
	}})

	res := h.Call(context.Background(), map[string]interface{}{"schedule": "not cron", "command": "echo hi"})
	if !res.IsError {
		t.Fatalf("expected schedule validation error")
	}
}

func TestCronAddToolReturnsNormalizedBackendErrors(t *testing.T) {
	h := NewCronAddTool(stubCronBackend{addFn: func(ctx context.Context, req CronAddRequest) (CronJob, error) {
		return CronJob{}, errors.New("boom")
	}})

	res := h.Call(context.Background(), map[string]interface{}{"schedule": "*/5 * * * *", "command": "echo hi"})
	if !res.IsError {
		t.Fatalf("expected error")
	}
	if !strings.Contains(res.StructuredContent["error"].(string), "cron_add failed") {
		t.Fatalf("unexpected error: %v", res.StructuredContent["error"])
	}
}

func TestCronRunToolRequiresID(t *testing.T) {
	h := NewCronRunTool(stubCronBackend{runFn: func(ctx context.Context, req CronRunRequest) (CronRunResult, error) {
		return CronRunResult{}, nil
	}})

	res := h.Call(context.Background(), map[string]interface{}{})
	if !res.IsError {
		t.Fatalf("expected validation error")
	}
}

func TestCronAddToolSuccess(t *testing.T) {
	h := NewCronAddTool(stubCronBackend{addFn: func(ctx context.Context, req CronAddRequest) (CronJob, error) {
		return CronJob{ID: "job-1", Schedule: req.Schedule, Command: req.Command}, nil
	}})

	res := h.Call(context.Background(), map[string]interface{}{"schedule": "*/5 * * * *", "command": "echo hi"})
	if res.IsError {
		t.Fatalf("expected success, got error: %v", res.StructuredContent["error"])
	}
	if ok, _ := res.StructuredContent["ok"].(bool); !ok {
		t.Fatalf("expected ok=true")
	}
}

func TestCronAddToolRejectsOutOfRangeScheduleField(t *testing.T) {
	h := NewCronAddTool(stubCronBackend{addFn: func(ctx context.Context, req CronAddRequest) (CronJob, error) {
		return CronJob{}, nil
	}})
	res := h.Call(context.Background(), map[string]interface{}{"schedule": "60 * * * *", "command": "echo hi"})
	if !res.IsError {
		t.Fatalf("expected schedule validation error")
	}
	if !strings.Contains(res.StructuredContent["error"].(string), "out of range") {
		t.Fatalf("unexpected error: %v", res.StructuredContent["error"])
	}
}

func TestCronAddToolRejectsZeroStep(t *testing.T) {
	h := NewCronAddTool(stubCronBackend{addFn: func(ctx context.Context, req CronAddRequest) (CronJob, error) {
		return CronJob{}, nil
	}})
	res := h.Call(context.Background(), map[string]interface{}{"schedule": "*/0 * * * *", "command": "echo hi"})
	if !res.IsError {
		t.Fatalf("expected step validation error")
	}
	if !strings.Contains(res.StructuredContent["error"].(string), "positive integer") {
		t.Fatalf("unexpected error: %v", res.StructuredContent["error"])
	}
}
