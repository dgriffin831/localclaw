package cron

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Scheduler is an in-process local scheduler only.
type Scheduler interface {
	Start(ctx context.Context) error
	List(ctx context.Context) ([]Entry, error)
	Add(ctx context.Context, req AddRequest) (Entry, error)
	Remove(ctx context.Context, id string) (bool, error)
	Run(ctx context.Context, id string) (RunResult, error)
}

type Entry struct {
	ID        string `json:"id"`
	Schedule  string `json:"schedule"`
	Command   string `json:"command"`
	CreatedAt string `json:"createdAt,omitempty"`
	UpdatedAt string `json:"updatedAt,omitempty"`
	LastRunAt string `json:"lastRunAt,omitempty"`
}

type AddRequest struct {
	ID       string
	Schedule string
	Command  string
}

type RunResult struct {
	ID          string `json:"id"`
	TriggeredAt string `json:"triggeredAt"`
}

type InProcessScheduler struct {
	enabled bool
	now     func() time.Time

	mu   sync.RWMutex
	jobs map[string]Entry
}

func NewInProcessScheduler(enabled bool) *InProcessScheduler {
	return &InProcessScheduler{enabled: enabled, now: time.Now, jobs: map[string]Entry{}}
}

func (s *InProcessScheduler) Start(ctx context.Context) error {
	// TODO: Implement background scheduling loop that parses cron expressions and executes due jobs; Start is currently a lifecycle placeholder.
	return nil
}

func (s *InProcessScheduler) List(ctx context.Context) ([]Entry, error) {
	if !s.enabled {
		return nil, errors.New("cron scheduler is disabled")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]Entry, 0, len(s.jobs))
	for _, entry := range s.jobs {
		items = append(items, entry)
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].ID < items[j].ID
	})
	return items, nil
}

func (s *InProcessScheduler) Add(ctx context.Context, req AddRequest) (Entry, error) {
	if !s.enabled {
		return Entry{}, errors.New("cron scheduler is disabled")
	}
	id := strings.TrimSpace(req.ID)
	if id == "" {
		id = fmt.Sprintf("cron-%d", s.now().UTC().UnixNano())
	}
	schedule := strings.TrimSpace(req.Schedule)
	command := strings.TrimSpace(req.Command)
	if schedule == "" {
		return Entry{}, errors.New("schedule is required")
	}
	if command == "" {
		return Entry{}, errors.New("command is required")
	}
	now := s.now().UTC().Format(time.RFC3339Nano)

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.jobs[id]; exists {
		return Entry{}, fmt.Errorf("cron job %q already exists", id)
	}
	entry := Entry{
		ID:        id,
		Schedule:  schedule,
		Command:   command,
		CreatedAt: now,
		UpdatedAt: now,
	}
	s.jobs[id] = entry
	return entry, nil
}

func (s *InProcessScheduler) Remove(ctx context.Context, id string) (bool, error) {
	if !s.enabled {
		return false, errors.New("cron scheduler is disabled")
	}
	normalized := strings.TrimSpace(id)
	if normalized == "" {
		return false, errors.New("id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.jobs[normalized]; !exists {
		return false, nil
	}
	delete(s.jobs, normalized)
	return true, nil
}

func (s *InProcessScheduler) Run(ctx context.Context, id string) (RunResult, error) {
	if !s.enabled {
		return RunResult{}, errors.New("cron scheduler is disabled")
	}
	normalized := strings.TrimSpace(id)
	if normalized == "" {
		return RunResult{}, errors.New("id is required")
	}
	now := s.now().UTC().Format(time.RFC3339Nano)
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, exists := s.jobs[normalized]
	if !exists {
		return RunResult{}, fmt.Errorf("cron job %q not found", normalized)
	}
	entry.LastRunAt = now
	entry.UpdatedAt = now
	s.jobs[normalized] = entry
	return RunResult{ID: normalized, TriggeredAt: now}, nil
}
