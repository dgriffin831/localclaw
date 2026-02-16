package cron

import (
	"context"
	"testing"
	"time"
)

func TestInProcessSchedulerAddListRemoveRun(t *testing.T) {
	s := NewInProcessScheduler(true)
	s.now = func() time.Time { return time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC) }

	added, err := s.Add(context.Background(), AddRequest{ID: "job-1", Schedule: "*/5 * * * *", Command: "echo hi"})
	if err != nil {
		t.Fatalf("Add error: %v", err)
	}
	if added.ID != "job-1" {
		t.Fatalf("unexpected id: %s", added.ID)
	}

	items, err := s.List(context.Background())
	if err != nil {
		t.Fatalf("List error: %v", err)
	}
	if len(items) != 1 || items[0].ID != "job-1" {
		t.Fatalf("unexpected list items: %+v", items)
	}

	run, err := s.Run(context.Background(), "job-1")
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if run.ID != "job-1" {
		t.Fatalf("unexpected run id: %s", run.ID)
	}

	removed, err := s.Remove(context.Background(), "job-1")
	if err != nil {
		t.Fatalf("Remove error: %v", err)
	}
	if !removed {
		t.Fatalf("expected removed=true")
	}
}

func TestInProcessSchedulerValidationAndDisabled(t *testing.T) {
	s := NewInProcessScheduler(true)
	if _, err := s.Add(context.Background(), AddRequest{Schedule: "", Command: "echo hi"}); err == nil {
		t.Fatalf("expected schedule validation error")
	}
	if _, err := s.Add(context.Background(), AddRequest{Schedule: "* * * * *", Command: ""}); err == nil {
		t.Fatalf("expected command validation error")
	}
	if _, err := s.Remove(context.Background(), " "); err == nil {
		t.Fatalf("expected id validation error")
	}

	disabled := NewInProcessScheduler(false)
	if _, err := disabled.List(context.Background()); err == nil {
		t.Fatalf("expected disabled error")
	}
}
