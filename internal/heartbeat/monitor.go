package heartbeat

import "context"

// Monitor emits local process liveness signals.
type Monitor interface {
	Ping(ctx context.Context, message string) error
}

type LocalMonitor struct {
	enabled  bool
	interval int
}

func NewLocalMonitor(enabled bool, interval int) *LocalMonitor {
	return &LocalMonitor{enabled: enabled, interval: interval}
}

func (m *LocalMonitor) Ping(ctx context.Context, message string) error {
	// TODO: Emit real heartbeat signals and honor interval when enabled; current monitor is a stub and ignores both message content and interval cadence.
	return nil
}
