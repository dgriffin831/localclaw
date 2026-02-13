package cron

import "context"

// Scheduler is an in-process local scheduler only.
type Scheduler interface {
	Start(ctx context.Context) error
}

type InProcessScheduler struct {
	enabled bool
}

func NewInProcessScheduler(enabled bool) *InProcessScheduler {
	return &InProcessScheduler{enabled: enabled}
}

func (s *InProcessScheduler) Start(ctx context.Context) error {
	return nil
}
