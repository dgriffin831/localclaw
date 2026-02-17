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

const (
	RunStatusSuccess  = "success"
	RunStatusError    = "error"
	RunStatusTimeout  = "timeout"
	RunStatusCanceled = "canceled"
)

const defaultCommandTimeout = 5 * time.Minute

var errSchedulerDisabled = errors.New("cron scheduler is disabled")

// Scheduler is an in-process local scheduler only.
type Scheduler interface {
	Start(ctx context.Context) error
	List(ctx context.Context) ([]Entry, error)
	Add(ctx context.Context, req AddRequest) (Entry, error)
	Remove(ctx context.Context, id string) (bool, error)
	Run(ctx context.Context, id string) (RunResult, error)
}

type Entry struct {
	ID                string `json:"id"`
	Schedule          string `json:"schedule"`
	Command           string `json:"command"`
	CreatedAt         string `json:"createdAt,omitempty"`
	UpdatedAt         string `json:"updatedAt,omitempty"`
	LastRunAt         string `json:"lastRunAt,omitempty"`
	LastRunStatus     string `json:"lastRunStatus,omitempty"`
	LastRunExitCode   *int   `json:"lastRunExitCode,omitempty"`
	LastRunError      string `json:"lastRunError,omitempty"`
	LastRunDurationMs int64  `json:"lastRunDurationMs,omitempty"`
}

type AddRequest struct {
	ID       string
	Schedule string
	Command  string
}

type RunResult struct {
	ID          string `json:"id"`
	TriggeredAt string `json:"triggeredAt"`
	Status      string `json:"status"`
	ExitCode    *int   `json:"exitCode,omitempty"`
	Error       string `json:"error,omitempty"`
}

type Settings struct {
	Enabled        bool
	StateRoot      string
	CommandTimeout time.Duration
}

type InProcessScheduler struct {
	enabled        bool
	storePath      string
	commandTimeout time.Duration

	now           func() time.Time
	commandRunner func(ctx context.Context, command string) commandRunOutcome

	mu      sync.RWMutex
	jobs    map[string]*scheduledJob
	started bool
	wakeCh  chan struct{}
}

type scheduledJob struct {
	entry    Entry
	schedule parsedSchedule
	nextRun  time.Time
	running  bool
	cancel   context.CancelFunc
	runSeq   uint64
}

type runInvocation struct {
	jobID     string
	runSeq    uint64
	ctx       context.Context
	cancel    context.CancelFunc
	command   string
	triggered time.Time
}

func NewInProcessScheduler(enabled bool) *InProcessScheduler {
	return NewInProcessSchedulerWithSettings(Settings{Enabled: enabled})
}

func NewInProcessSchedulerWithSettings(settings Settings) *InProcessScheduler {
	timeout := settings.CommandTimeout
	if timeout <= 0 {
		timeout = defaultCommandTimeout
	}
	return &InProcessScheduler{
		enabled:        settings.Enabled,
		storePath:      resolveStorePath(settings.StateRoot),
		commandTimeout: timeout,
		now:            time.Now,
		commandRunner:  runLocalCommand,
		jobs:           map[string]*scheduledJob{},
	}
}

func (s *InProcessScheduler) Start(ctx context.Context) error {
	if !s.enabled {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	now := s.now().UTC()
	loaded, err := loadEntries(s.storePath)
	if err != nil {
		return fmt.Errorf("load cron jobs: %w", err)
	}

	jobs := make(map[string]*scheduledJob, len(loaded))
	for _, entry := range loaded {
		job, jobErr := s.buildJob(entry, now, true)
		if jobErr != nil {
			return fmt.Errorf("load cron jobs: %w", jobErr)
		}
		if _, exists := jobs[job.entry.ID]; exists {
			return fmt.Errorf("load cron jobs: duplicate cron job %q", job.entry.ID)
		}
		jobs[job.entry.ID] = job
	}

	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return nil
	}
	s.jobs = jobs
	s.started = true
	s.wakeCh = make(chan struct{}, 1)
	s.mu.Unlock()

	go s.runLoop(ctx)
	s.notifyLoop()
	return nil
}

func (s *InProcessScheduler) List(ctx context.Context) ([]Entry, error) {
	if !s.enabled {
		return nil, errSchedulerDisabled
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	items := make([]Entry, 0, len(s.jobs))
	for _, job := range s.jobs {
		entry := job.entry
		entry.LastRunExitCode = cloneIntPtr(job.entry.LastRunExitCode)
		items = append(items, entry)
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].ID < items[j].ID
	})
	return items, nil
}

func (s *InProcessScheduler) Add(ctx context.Context, req AddRequest) (Entry, error) {
	if !s.enabled {
		return Entry{}, errSchedulerDisabled
	}
	normalizedSchedule, err := NormalizeSchedule(req.Schedule)
	if err != nil {
		return Entry{}, err
	}
	command := strings.TrimSpace(req.Command)
	if command == "" {
		return Entry{}, errors.New("command is required")
	}

	now := s.now().UTC()
	nowStamp := now.Format(time.RFC3339Nano)

	entry := Entry{
		ID:        strings.TrimSpace(req.ID),
		Schedule:  normalizedSchedule,
		Command:   command,
		CreatedAt: nowStamp,
		UpdatedAt: nowStamp,
	}

	s.mu.Lock()
	if entry.ID == "" {
		entry.ID = s.generateIDLocked()
	}
	if _, exists := s.jobs[entry.ID]; exists {
		s.mu.Unlock()
		return Entry{}, fmt.Errorf("cron job %q already exists", entry.ID)
	}

	job, err := s.buildJob(entry, now, s.started)
	if err != nil {
		s.mu.Unlock()
		return Entry{}, err
	}
	s.jobs[entry.ID] = job
	if persistErr := s.persistLocked(); persistErr != nil {
		delete(s.jobs, entry.ID)
		s.mu.Unlock()
		return Entry{}, persistErr
	}
	s.mu.Unlock()

	s.notifyLoop()
	return entry, nil
}

func (s *InProcessScheduler) Remove(ctx context.Context, id string) (bool, error) {
	if !s.enabled {
		return false, errSchedulerDisabled
	}
	normalized := strings.TrimSpace(id)
	if normalized == "" {
		return false, errors.New("id is required")
	}

	s.mu.Lock()
	job, exists := s.jobs[normalized]
	if !exists {
		s.mu.Unlock()
		return false, fmt.Errorf("cron job %q not found", normalized)
	}
	if job.cancel != nil {
		job.cancel()
	}
	delete(s.jobs, normalized)
	persistErr := s.persistLocked()
	s.mu.Unlock()
	if persistErr != nil {
		return true, persistErr
	}

	s.notifyLoop()
	return true, nil
}

func (s *InProcessScheduler) Run(ctx context.Context, id string) (RunResult, error) {
	if !s.enabled {
		return RunResult{}, errSchedulerDisabled
	}
	normalized := strings.TrimSpace(id)
	if normalized == "" {
		return RunResult{}, errors.New("id is required")
	}
	invocation, err := s.prepareInvocation(ctx, normalized)
	if err != nil {
		return RunResult{}, err
	}
	return s.executeInvocation(invocation)
}

func (s *InProcessScheduler) runLoop(ctx context.Context) {
	for {
		invocations, wait := s.collectDueInvocations(ctx)
		for _, invocation := range invocations {
			current := invocation
			go func() {
				_, _ = s.executeInvocation(current)
			}()
		}

		if wait < 0 {
			select {
			case <-ctx.Done():
				s.shutdownLoop()
				return
			case <-s.loopWakeChannel():
			}
			continue
		}

		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			s.shutdownLoop()
			return
		case <-s.loopWakeChannel():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		case <-timer.C:
		}
	}
}

func (s *InProcessScheduler) shutdownLoop() {
	s.mu.Lock()
	for _, job := range s.jobs {
		if job.cancel != nil {
			job.cancel()
		}
	}
	s.started = false
	s.wakeCh = nil
	s.mu.Unlock()
}

func (s *InProcessScheduler) collectDueInvocations(parent context.Context) ([]runInvocation, time.Duration) {
	now := s.now().UTC()

	s.mu.Lock()
	ids := make([]string, 0)
	for id, job := range s.jobs {
		if job.running || job.nextRun.IsZero() {
			continue
		}
		if !job.nextRun.After(now) {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)

	invocations := make([]runInvocation, 0, len(ids))
	for _, id := range ids {
		invocation, err := s.prepareInvocationLocked(parent, id)
		if err != nil {
			continue
		}
		invocations = append(invocations, invocation)
	}

	nextWake := time.Time{}
	for _, job := range s.jobs {
		if job.running || job.nextRun.IsZero() {
			continue
		}
		if nextWake.IsZero() || job.nextRun.Before(nextWake) {
			nextWake = job.nextRun
		}
	}
	s.mu.Unlock()

	if nextWake.IsZero() {
		return invocations, -1
	}
	if !nextWake.After(now) {
		return invocations, 0
	}
	return invocations, nextWake.Sub(now)
}

func (s *InProcessScheduler) prepareInvocation(parent context.Context, id string) (runInvocation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.prepareInvocationLocked(parent, id)
}

func (s *InProcessScheduler) prepareInvocationLocked(parent context.Context, id string) (runInvocation, error) {
	job, exists := s.jobs[id]
	if !exists {
		return runInvocation{}, fmt.Errorf("cron job %q not found", id)
	}
	if job.running {
		return runInvocation{}, fmt.Errorf("cron job %q is already running", id)
	}
	if parent == nil {
		parent = context.Background()
	}
	runCtx, cancel := context.WithTimeout(parent, s.commandTimeout)
	now := s.now().UTC()
	job.running = true
	job.cancel = cancel
	job.runSeq++
	if job.schedule.kind == scheduleKindReboot {
		job.nextRun = time.Time{}
	}
	job.entry.UpdatedAt = now.Format(time.RFC3339Nano)

	return runInvocation{
		jobID:     id,
		runSeq:    job.runSeq,
		ctx:       runCtx,
		cancel:    cancel,
		command:   job.entry.Command,
		triggered: now,
	}, nil
}

func (s *InProcessScheduler) executeInvocation(invocation runInvocation) (RunResult, error) {
	outcome := s.commandRunner(invocation.ctx, invocation.command)
	invocation.cancel()

	if outcome.status == "" {
		outcome.status = RunStatusError
	}
	if outcome.status == RunStatusSuccess && outcome.exitCode == nil {
		zero := 0
		outcome.exitCode = &zero
	}

	finished := s.now().UTC()
	duration := finished.Sub(invocation.triggered)
	if duration < 0 {
		duration = 0
	}

	result := RunResult{
		ID:          invocation.jobID,
		TriggeredAt: invocation.triggered.Format(time.RFC3339Nano),
		Status:      outcome.status,
		ExitCode:    cloneIntPtr(outcome.exitCode),
		Error:       strings.TrimSpace(outcome.errorMessage),
	}

	var persistErr error
	s.mu.Lock()
	job, exists := s.jobs[invocation.jobID]
	if exists && job.runSeq == invocation.runSeq {
		job.running = false
		job.cancel = nil
		job.entry.LastRunAt = result.TriggeredAt
		job.entry.UpdatedAt = finished.Format(time.RFC3339Nano)
		job.entry.LastRunStatus = result.Status
		job.entry.LastRunExitCode = cloneIntPtr(result.ExitCode)
		job.entry.LastRunError = result.Error
		job.entry.LastRunDurationMs = duration.Milliseconds()
		if job.schedule.kind == scheduleKindReboot {
			job.nextRun = time.Time{}
		} else if next, ok := job.schedule.next(finished); ok {
			job.nextRun = next
		} else {
			job.nextRun = time.Time{}
		}
		persistErr = s.persistLocked()
	}
	s.mu.Unlock()

	s.notifyLoop()
	return result, persistErr
}

func (s *InProcessScheduler) buildJob(entry Entry, now time.Time, activateReboot bool) (*scheduledJob, error) {
	id := strings.TrimSpace(entry.ID)
	if id == "" {
		return nil, errors.New("id is required")
	}
	normalizedSchedule, schedule, err := normalizeAndParseSchedule(entry.Schedule)
	if err != nil {
		return nil, err
	}
	command := strings.TrimSpace(entry.Command)
	if command == "" {
		return nil, errors.New("command is required")
	}
	createdAt := strings.TrimSpace(entry.CreatedAt)
	updatedAt := strings.TrimSpace(entry.UpdatedAt)
	nowStamp := now.Format(time.RFC3339Nano)
	if createdAt == "" {
		createdAt = nowStamp
	}
	if updatedAt == "" {
		updatedAt = createdAt
	}

	normalized := Entry{
		ID:                id,
		Schedule:          normalizedSchedule,
		Command:           command,
		CreatedAt:         createdAt,
		UpdatedAt:         updatedAt,
		LastRunAt:         strings.TrimSpace(entry.LastRunAt),
		LastRunStatus:     strings.TrimSpace(entry.LastRunStatus),
		LastRunExitCode:   cloneIntPtr(entry.LastRunExitCode),
		LastRunError:      strings.TrimSpace(entry.LastRunError),
		LastRunDurationMs: entry.LastRunDurationMs,
	}

	job := &scheduledJob{entry: normalized, schedule: schedule}
	if schedule.kind == scheduleKindReboot {
		if activateReboot {
			job.nextRun = now
		}
		return job, nil
	}
	if next, ok := schedule.next(now); ok {
		job.nextRun = next
	}
	return job, nil
}

func (s *InProcessScheduler) persistLocked() error {
	entries := make([]Entry, 0, len(s.jobs))
	for _, job := range s.jobs {
		entry := job.entry
		entry.LastRunExitCode = cloneIntPtr(job.entry.LastRunExitCode)
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].ID < entries[j].ID
	})
	if err := saveEntries(s.storePath, entries); err != nil {
		return fmt.Errorf("persist cron jobs: %w", err)
	}
	return nil
}

func (s *InProcessScheduler) generateIDLocked() string {
	base := s.now().UTC().UnixNano()
	for idx := 0; idx < 1000; idx++ {
		candidate := fmt.Sprintf("cron-%d", base+int64(idx))
		if _, exists := s.jobs[candidate]; !exists {
			return candidate
		}
	}
	return fmt.Sprintf("cron-%d", time.Now().UTC().UnixNano())
}

func (s *InProcessScheduler) notifyLoop() {
	wake := s.loopWakeChannel()
	if wake == nil {
		return
	}
	select {
	case wake <- struct{}{}:
	default:
	}
}

func (s *InProcessScheduler) loopWakeChannel() chan struct{} {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.wakeCh
}

func cloneIntPtr(value *int) *int {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
