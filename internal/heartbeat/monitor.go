package heartbeat

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"
)

const (
	defaultTickInterval = 30 * time.Second
)

// Monitor emits local process liveness signals.
type Monitor interface {
	Ping(ctx context.Context, message string) error
	Start(ctx context.Context, run Runner)
}

// Runner executes one heartbeat tick.
type Runner func(ctx context.Context) error

type ticker interface {
	C() <-chan time.Time
	Stop()
}

type localTicker struct {
	t *time.Ticker
}

func (t *localTicker) C() <-chan time.Time {
	return t.t.C
}

func (t *localTicker) Stop() {
	t.t.Stop()
}

type LocalMonitor struct {
	enabled  bool
	interval time.Duration
	logf     func(format string, args ...interface{})

	mu      sync.Mutex
	started bool
	running bool

	newTicker func(interval time.Duration) ticker
}

func NewLocalMonitor(enabled bool, intervalSeconds int) *LocalMonitor {
	return NewLocalMonitorWithSettings(Settings{
		Enabled:         enabled,
		IntervalSeconds: intervalSeconds,
	})
}

type Settings struct {
	Enabled         bool
	IntervalSeconds int
	Logf            func(format string, args ...interface{})
}

func NewLocalMonitorWithSettings(settings Settings) *LocalMonitor {
	logf := settings.Logf
	if logf == nil {
		logf = log.Printf
	}
	interval := time.Duration(settings.IntervalSeconds) * time.Second
	if interval <= 0 {
		interval = defaultTickInterval
	}
	return &LocalMonitor{
		enabled:  settings.Enabled,
		interval: interval,
		logf:     logf,
		newTicker: func(interval time.Duration) ticker {
			return &localTicker{t: time.NewTicker(interval)}
		},
	}
}

func (m *LocalMonitor) Ping(ctx context.Context, message string) error {
	trimmed := strings.TrimSpace(message)
	if trimmed == "" {
		return nil
	}
	m.log("heartbeat: %s", trimmed)
	return nil
}

func (m *LocalMonitor) Start(ctx context.Context, run Runner) {
	if !m.enabled || run == nil || m.interval <= 0 {
		return
	}
	m.mu.Lock()
	if m.started {
		m.mu.Unlock()
		return
	}
	m.started = true
	m.mu.Unlock()

	newTicker := m.newTicker
	if newTicker == nil {
		newTicker = func(interval time.Duration) ticker {
			return &localTicker{t: time.NewTicker(interval)}
		}
	}
	t := newTicker(m.interval)
	go func() {
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C():
				if !m.startRun() {
					m.log("heartbeat: skipped tick while previous run is active")
					continue
				}
				go func() {
					defer m.endRun()
					if err := run(ctx); err != nil {
						m.log("heartbeat: run failed: %v", err)
					}
				}()
			}
		}
	}()
}

func (m *LocalMonitor) log(format string, args ...interface{}) {
	if m == nil || m.logf == nil {
		return
	}
	m.logf(format, args...)
}

func (m *LocalMonitor) startRun() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.running {
		return false
	}
	m.running = true
	return true
}

func (m *LocalMonitor) endRun() {
	m.mu.Lock()
	m.running = false
	m.mu.Unlock()
}
