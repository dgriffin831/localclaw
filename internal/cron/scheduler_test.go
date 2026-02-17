package cron

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestInProcessSchedulerRecurringExecutionFiresForDueJobs(t *testing.T) {
	stateRoot := t.TempDir()

	s := NewInProcessSchedulerWithSettings(Settings{
		Enabled:    true,
		StateRoot:  stateRoot,
		RunTimeout: 2 * time.Second,
	})
	var runMu sync.Mutex
	runCount := 0
	s.executor = func(ctx context.Context, entry Entry) RunOutcome {
		runMu.Lock()
		runCount++
		runMu.Unlock()
		return RunOutcome{Status: RunStatusSuccess}
	}

	var nowMu sync.Mutex
	nowValue := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	s.now = func() time.Time {
		nowMu.Lock()
		defer nowMu.Unlock()
		return nowValue
	}

	if _, err := s.Add(context.Background(), AddRequest{ID: "job-1", Schedule: "* * * * *", Message: "job-1"}); err != nil {
		t.Fatalf("add job: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("start scheduler: %v", err)
	}
	s.mu.RLock()
	initialJob := s.jobs["job-1"]
	s.mu.RUnlock()
	if initialJob == nil {
		t.Fatalf("expected job-1 to be loaded")
	}
	if initialJob.nextRun.IsZero() {
		t.Fatalf("expected job-1 next run to be scheduled")
	}

	nowMu.Lock()
	nowValue = nowValue.Truncate(time.Minute).Add(time.Minute).Add(time.Second)
	nowMu.Unlock()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		s.notifyLoop()
		runMu.Lock()
		count := runCount
		runMu.Unlock()
		if count >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	runMu.Lock()
	currentRunCount := runCount
	runMu.Unlock()
	if currentRunCount < 1 {
		s.mu.RLock()
		job := s.jobs["job-1"]
		s.mu.RUnlock()
		nowMu.Lock()
		currentNow := nowValue
		nowMu.Unlock()
		t.Fatalf("expected first recurring run, got runCount=%d running=%v nextRun=%v lastRunAt=%q now=%v", currentRunCount, job.running, job.nextRun, job.entry.LastRunAt, currentNow)
	}

	nowMu.Lock()
	nowValue = nowValue.Add(2 * time.Minute)
	nowMu.Unlock()
	waitForCondition(t, 3*time.Second, func() bool {
		s.notifyLoop()
		runMu.Lock()
		defer runMu.Unlock()
		return runCount >= 2
	})
}

func TestInProcessSchedulerPersistenceReloadsJobsAcrossRestart(t *testing.T) {
	stateRoot := t.TempDir()

	s1 := NewInProcessSchedulerWithSettings(Settings{Enabled: true, StateRoot: stateRoot})
	if _, err := s1.Add(context.Background(), AddRequest{ID: "job-1", Schedule: "*/5 * * * *", Message: "hello"}); err != nil {
		t.Fatalf("add job: %v", err)
	}

	s2 := NewInProcessSchedulerWithSettings(Settings{Enabled: true, StateRoot: stateRoot})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := s2.Start(ctx); err != nil {
		t.Fatalf("start second scheduler: %v", err)
	}

	items, err := s2.List(context.Background())
	if err != nil {
		t.Fatalf("list jobs: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 persisted job, got %d", len(items))
	}
	if items[0].ID != "job-1" {
		t.Fatalf("expected job-1, got %+v", items[0])
	}
	if items[0].Message != "hello" {
		t.Fatalf("expected message to persist, got %+v", items[0])
	}
}

func TestInProcessSchedulerRunRemoveValidationAndNotFound(t *testing.T) {
	s := NewInProcessSchedulerWithSettings(Settings{Enabled: true, StateRoot: t.TempDir()})

	if _, err := s.Run(context.Background(), " "); err == nil || !strings.Contains(err.Error(), "id is required") {
		t.Fatalf("expected run id validation error, got %v", err)
	}
	if _, err := s.Remove(context.Background(), " "); err == nil || !strings.Contains(err.Error(), "id is required") {
		t.Fatalf("expected remove id validation error, got %v", err)
	}
	if _, err := s.Run(context.Background(), "missing"); err == nil || !strings.Contains(err.Error(), `cron job "missing" not found`) {
		t.Fatalf("expected run not found error, got %v", err)
	}
	if _, err := s.Remove(context.Background(), "missing"); err == nil || !strings.Contains(err.Error(), `cron job "missing" not found`) {
		t.Fatalf("expected remove not found error, got %v", err)
	}
}

func TestInProcessSchedulerRunRecordsFailureAndTimeoutMetadata(t *testing.T) {
	s := NewInProcessSchedulerWithSettings(Settings{
		Enabled:    true,
		StateRoot:  t.TempDir(),
		RunTimeout: 500 * time.Millisecond,
		Executor: func(ctx context.Context, entry Entry) RunOutcome {
			switch entry.ID {
			case "fail":
				return RunOutcome{Status: RunStatusError, Error: "run failed"}
			case "slow":
				<-ctx.Done()
				return RunOutcome{}
			default:
				return RunOutcome{Status: RunStatusSuccess}
			}
		},
	})

	if _, err := s.Add(context.Background(), AddRequest{ID: "fail", Schedule: "*/5 * * * *", Message: "fail"}); err != nil {
		t.Fatalf("add fail job: %v", err)
	}
	if _, err := s.Add(context.Background(), AddRequest{ID: "slow", Schedule: "*/5 * * * *", Message: "slow"}); err != nil {
		t.Fatalf("add slow job: %v", err)
	}
	if _, err := s.Add(context.Background(), AddRequest{ID: "ok", Schedule: "*/5 * * * *", Message: "ok"}); err != nil {
		t.Fatalf("add ok job: %v", err)
	}

	failResult, err := s.Run(context.Background(), "fail")
	if err != nil {
		t.Fatalf("run fail job returned api error: %v", err)
	}
	if failResult.Status != RunStatusError {
		t.Fatalf("expected fail status=%q, got %+v", RunStatusError, failResult)
	}

	timeoutResult, err := s.Run(context.Background(), "slow")
	if err != nil {
		t.Fatalf("run slow job returned api error: %v", err)
	}
	if timeoutResult.Status != RunStatusTimeout {
		t.Fatalf("expected timeout status=%q, got %+v", RunStatusTimeout, timeoutResult)
	}

	okResult, err := s.Run(context.Background(), "ok")
	if err != nil {
		t.Fatalf("run ok job: %v", err)
	}
	if okResult.Status != RunStatusSuccess {
		t.Fatalf("expected success status=%q, got %+v", RunStatusSuccess, okResult)
	}

	items, err := s.List(context.Background())
	if err != nil {
		t.Fatalf("list after runs: %v", err)
	}
	indexed := map[string]Entry{}
	for _, item := range items {
		indexed[item.ID] = item
	}
	if indexed["fail"].LastRunStatus != RunStatusError {
		t.Fatalf("expected fail metadata status=%q, got %+v", RunStatusError, indexed["fail"])
	}
	if indexed["fail"].LastRunError != "run failed" {
		t.Fatalf("expected fail metadata error, got %+v", indexed["fail"])
	}
	if indexed["slow"].LastRunStatus != RunStatusTimeout {
		t.Fatalf("expected slow metadata status=%q, got %+v", RunStatusTimeout, indexed["slow"])
	}
}

func TestInProcessSchedulerRemoveWhileRunningCancelsAndUnschedules(t *testing.T) {
	s := NewInProcessSchedulerWithSettings(Settings{
		Enabled:    true,
		StateRoot:  t.TempDir(),
		RunTimeout: 5 * time.Second,
		Executor: func(ctx context.Context, entry Entry) RunOutcome {
			<-ctx.Done()
			return RunOutcome{}
		},
	})

	if _, err := s.Add(context.Background(), AddRequest{ID: "job-1", Schedule: "*/5 * * * *", Message: "block"}); err != nil {
		t.Fatalf("add job: %v", err)
	}

	resultCh := make(chan RunResult, 1)
	errCh := make(chan error, 1)
	go func() {
		result, err := s.Run(context.Background(), "job-1")
		if err != nil {
			errCh <- err
			return
		}
		resultCh <- result
	}()

	waitForCondition(t, 2*time.Second, func() bool {
		s.mu.RLock()
		defer s.mu.RUnlock()
		job, ok := s.jobs["job-1"]
		if !ok {
			return false
		}
		return job.running
	})

	removed, err := s.Remove(context.Background(), "job-1")
	if err != nil {
		t.Fatalf("remove running job: %v", err)
	}
	if !removed {
		t.Fatalf("expected remove to report removed=true")
	}

	select {
	case err := <-errCh:
		t.Fatalf("run returned error after cancellation: %v", err)
	case result := <-resultCh:
		if result.Status != RunStatusCanceled {
			t.Fatalf("expected canceled status, got %+v", result)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for run result")
	}

	items, err := s.List(context.Background())
	if err != nil {
		t.Fatalf("list jobs: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("expected no jobs after removal, got %+v", items)
	}
}

func TestInProcessSchedulerRebootRunsOncePerStartForPersistedJobs(t *testing.T) {
	stateRoot := t.TempDir()
	var runMu sync.Mutex
	runCount := 0

	s1 := NewInProcessSchedulerWithSettings(Settings{Enabled: true, StateRoot: stateRoot})
	s1.executor = func(ctx context.Context, entry Entry) RunOutcome {
		runMu.Lock()
		runCount++
		runMu.Unlock()
		return RunOutcome{Status: RunStatusSuccess}
	}
	if _, err := s1.Add(context.Background(), AddRequest{ID: "boot", Schedule: "@reboot", Message: "reboot"}); err != nil {
		t.Fatalf("add reboot job: %v", err)
	}

	ctx1, cancel1 := context.WithCancel(context.Background())
	if err := s1.Start(ctx1); err != nil {
		t.Fatalf("start first scheduler: %v", err)
	}
	waitForCondition(t, 3*time.Second, func() bool {
		s1.notifyLoop()
		runMu.Lock()
		defer runMu.Unlock()
		return runCount >= 1
	})

	// Wake loop again; @reboot should not repeat during a single scheduler lifetime.
	s1.notifyLoop()
	time.Sleep(50 * time.Millisecond)
	runMu.Lock()
	if runCount != 1 {
		t.Fatalf("expected one run during first scheduler start, got %d", runCount)
	}
	runMu.Unlock()
	cancel1()

	s2 := NewInProcessSchedulerWithSettings(Settings{Enabled: true, StateRoot: stateRoot})
	s2.executor = s1.executor
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	if err := s2.Start(ctx2); err != nil {
		t.Fatalf("start second scheduler: %v", err)
	}
	waitForCondition(t, 3*time.Second, func() bool {
		s2.notifyLoop()
		runMu.Lock()
		defer runMu.Unlock()
		return runCount >= 2
	})
}

func TestInProcessSchedulerSessionTargetDefaultsToIsolated(t *testing.T) {
	s := NewInProcessSchedulerWithSettings(Settings{Enabled: true, StateRoot: t.TempDir()})
	entry, err := s.Add(context.Background(), AddRequest{ID: "job-1", Schedule: "*/5 * * * *", Message: "hello"})
	if err != nil {
		t.Fatalf("add job: %v", err)
	}
	if entry.SessionTarget != SessionTargetIsolated {
		t.Fatalf("expected default session target %q, got %q", SessionTargetIsolated, entry.SessionTarget)
	}
}

func TestInProcessSchedulerValidateSessionTarget(t *testing.T) {
	s := NewInProcessSchedulerWithSettings(Settings{Enabled: true, StateRoot: t.TempDir()})
	_, err := s.Add(context.Background(), AddRequest{ID: "job-1", Schedule: "*/5 * * * *", SessionTarget: "unknown", Message: "hello"})
	if err == nil || !strings.Contains(err.Error(), "sessionTarget must be one of") {
		t.Fatalf("expected sessionTarget validation error, got %v", err)
	}
}

func TestValidateSchedule(t *testing.T) {
	if err := ValidateSchedule("*/5 * * * *"); err != nil {
		t.Fatalf("expected valid cron schedule: %v", err)
	}
	if err := ValidateSchedule("@reboot"); err != nil {
		t.Fatalf("expected @reboot to be valid: %v", err)
	}
	if err := ValidateSchedule("not cron"); err == nil {
		t.Fatalf("expected invalid schedule error")
	}
}

func waitForCondition(t *testing.T, timeout time.Duration, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("condition was not met within %s", timeout)
}
