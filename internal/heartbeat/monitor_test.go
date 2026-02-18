package heartbeat

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func waitForCall(t *testing.T, calls <-chan struct{}, timeout time.Duration) {
	t.Helper()
	select {
	case <-calls:
	case <-time.After(timeout):
		t.Fatalf("timed out waiting for heartbeat callback")
	}
}

func waitForCallCount(t *testing.T, count *int32, target int32, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(count) >= target {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for heartbeat callback count >= %d (got %d)", target, atomic.LoadInt32(count))
}

func TestLocalMonitorStartDisabledSkipsLoop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m := &LocalMonitor{enabled: false, interval: 10 * time.Millisecond}
	var calls int32
	m.Start(ctx, func(runCtx context.Context) error {
		atomic.AddInt32(&calls, 1)
		return nil
	})

	time.Sleep(40 * time.Millisecond)
	if got := atomic.LoadInt32(&calls); got != 0 {
		t.Fatalf("expected no heartbeat callbacks when disabled, got %d", got)
	}
}

func TestLocalMonitorStartRunsCallbackOnInterval(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m := &LocalMonitor{enabled: true, interval: 10 * time.Millisecond}
	calls := make(chan struct{}, 4)
	m.Start(ctx, func(runCtx context.Context) error {
		calls <- struct{}{}
		return nil
	})

	waitForCall(t, calls, 200*time.Millisecond)
	waitForCall(t, calls, 200*time.Millisecond)
}

func TestLocalMonitorStartStopsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	m := &LocalMonitor{enabled: true, interval: 20 * time.Millisecond}
	calls := make(chan struct{}, 8)
	m.Start(ctx, func(runCtx context.Context) error {
		calls <- struct{}{}
		return nil
	})

	waitForCall(t, calls, 250*time.Millisecond)
	cancel()

	select {
	case <-calls:
		t.Fatalf("expected no heartbeat callbacks after cancellation")
	case <-time.After(80 * time.Millisecond):
	}
}

func TestLocalMonitorStartSkipsOverlappingTicks(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m := &LocalMonitor{enabled: true, interval: 10 * time.Millisecond}
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	var calls int32
	m.Start(ctx, func(runCtx context.Context) error {
		callNum := atomic.AddInt32(&calls, 1)
		if callNum == 1 {
			close(firstStarted)
			select {
			case <-releaseFirst:
			case <-runCtx.Done():
			}
		}
		return nil
	})

	waitForCall(t, firstStarted, 250*time.Millisecond)
	time.Sleep(45 * time.Millisecond)
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected overlap guard to skip ticks while first run is active, got %d runs", got)
	}

	close(releaseFirst)
	waitForCallCount(t, &calls, 2, 250*time.Millisecond)
}

func TestLocalMonitorStartContinuesAfterRunnerError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m := &LocalMonitor{enabled: true, interval: 10 * time.Millisecond}
	calls := make(chan struct{}, 4)
	var count int32
	m.Start(ctx, func(runCtx context.Context) error {
		callNum := atomic.AddInt32(&count, 1)
		calls <- struct{}{}
		if callNum == 1 {
			return errors.New("boom")
		}
		return nil
	})

	waitForCall(t, calls, 250*time.Millisecond)
	waitForCall(t, calls, 250*time.Millisecond)
}

func TestLocalMonitorStartWritesOverlapMessageToConfiguredLogger(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logs := make(chan string, 8)
	m := &LocalMonitor{
		enabled:  true,
		interval: 10 * time.Millisecond,
		logf: func(format string, args ...interface{}) {
			logs <- strings.TrimSpace(fmt.Sprintf(format, args...))
		},
	}
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	m.Start(ctx, func(runCtx context.Context) error {
		select {
		case <-firstStarted:
		default:
			close(firstStarted)
		}
		select {
		case <-releaseFirst:
		case <-runCtx.Done():
		}
		return nil
	})

	waitForCall(t, firstStarted, 250*time.Millisecond)
	time.Sleep(45 * time.Millisecond)
	close(releaseFirst)

	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		select {
		case message := <-logs:
			if strings.Contains(message, "heartbeat: skipped tick while previous run is active") {
				return
			}
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
	t.Fatalf("expected overlap message to be logged")
}
