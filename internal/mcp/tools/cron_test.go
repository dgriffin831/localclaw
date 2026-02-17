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

	res := h.Call(context.Background(), map[string]interface{}{"schedule": "not cron", "message": "hello"})
	if !res.IsError {
		t.Fatalf("expected schedule validation error")
	}
}

func TestCronAddToolReturnsNormalizedBackendErrors(t *testing.T) {
	h := NewCronAddTool(stubCronBackend{addFn: func(ctx context.Context, req CronAddRequest) (CronJob, error) {
		return CronJob{}, errors.New("boom")
	}})

	res := h.Call(context.Background(), map[string]interface{}{"schedule": "*/5 * * * *", "message": "hello"})
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
		return CronJob{ID: "job-1", Schedule: req.Schedule, SessionTarget: req.SessionTarget, Message: req.Message, TimeoutSeconds: req.TimeoutSeconds}, nil
	}})

	res := h.Call(context.Background(), map[string]interface{}{
		"schedule":        "*/5 * * * *",
		"session_target":  "default",
		"message":         "hello",
		"timeout_seconds": 30,
	})
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
	res := h.Call(context.Background(), map[string]interface{}{"schedule": "60 * * * *", "message": "hello"})
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
	res := h.Call(context.Background(), map[string]interface{}{"schedule": "*/0 * * * *", "message": "hello"})
	if !res.IsError {
		t.Fatalf("expected step validation error")
	}
	if !strings.Contains(res.StructuredContent["error"].(string), "positive integer") {
		t.Fatalf("unexpected error: %v", res.StructuredContent["error"])
	}
}

func TestCronAddToolAcceptsRebootMacro(t *testing.T) {
	called := false
	h := NewCronAddTool(stubCronBackend{addFn: func(ctx context.Context, req CronAddRequest) (CronJob, error) {
		called = true
		if req.Schedule != "@reboot" {
			t.Fatalf("expected @reboot schedule, got %q", req.Schedule)
		}
		return CronJob{ID: "job-1", Schedule: req.Schedule, Message: req.Message}, nil
	}})
	res := h.Call(context.Background(), map[string]interface{}{"schedule": "@reboot", "message": "hello"})
	if res.IsError {
		t.Fatalf("expected @reboot macro to be accepted: %v", res.StructuredContent["error"])
	}
	if !called {
		t.Fatalf("expected backend add to be called")
	}
}

func TestCronAddToolRequiresMessage(t *testing.T) {
	h := NewCronAddTool(stubCronBackend{addFn: func(ctx context.Context, req CronAddRequest) (CronJob, error) {
		return CronJob{}, nil
	}})
	res := h.Call(context.Background(), map[string]interface{}{"schedule": "*/5 * * * *"})
	if !res.IsError {
		t.Fatalf("expected message validation error")
	}
	if !strings.Contains(res.StructuredContent["error"].(string), "message is required") {
		t.Fatalf("unexpected error: %v", res.StructuredContent["error"])
	}
}

func TestCronAddToolRejectsNegativeTimeout(t *testing.T) {
	h := NewCronAddTool(stubCronBackend{addFn: func(ctx context.Context, req CronAddRequest) (CronJob, error) {
		return CronJob{}, nil
	}})
	res := h.Call(context.Background(), map[string]interface{}{"schedule": "*/5 * * * *", "message": "hello", "timeout_seconds": -1})
	if !res.IsError {
		t.Fatalf("expected timeout validation error")
	}
	if !strings.Contains(res.StructuredContent["error"].(string), "timeout_seconds must be >= 0") {
		t.Fatalf("unexpected error: %v", res.StructuredContent["error"])
	}
}

func TestCronAddToolAcceptsLegacyPayloadMessage(t *testing.T) {
	called := false
	h := NewCronAddTool(stubCronBackend{addFn: func(ctx context.Context, req CronAddRequest) (CronJob, error) {
		called = true
		if req.Message != "test from cron" {
			t.Fatalf("expected legacy payload text mapped to message, got %q", req.Message)
		}
		return CronJob{ID: "job-1", Schedule: req.Schedule, Message: req.Message}, nil
	}})
	res := h.Call(context.Background(), map[string]interface{}{
		"schedule": "*/5 * * * *",
		"payload": map[string]interface{}{
			"kind": "signal",
			"text": "test from cron",
		},
	})
	if res.IsError {
		t.Fatalf("expected legacy payload call to succeed: %v", res.StructuredContent["error"])
	}
	if !called {
		t.Fatalf("expected backend add to be called")
	}
}
