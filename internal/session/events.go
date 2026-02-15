package session

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// TranscriptUpdate describes one transcript append/write delta.
type TranscriptUpdate struct {
	AgentID        string
	SessionID      string
	TranscriptPath string
	DeltaBytes     int
	DeltaMessages  int
	OccurredAt     time.Time
}

// TranscriptEventHandler consumes transcript update events.
type TranscriptEventHandler interface {
	HandleTranscriptUpdate(ctx context.Context, update TranscriptUpdate) error
}

// TranscriptEventBus fan-outs transcript updates to subscribers.
type TranscriptEventBus struct {
	mu       sync.RWMutex
	handlers []TranscriptEventHandler
}

func NewTranscriptEventBus() *TranscriptEventBus {
	return &TranscriptEventBus{}
}

func (b *TranscriptEventBus) Subscribe(handler TranscriptEventHandler) {
	if handler == nil {
		return
	}
	b.mu.Lock()
	b.handlers = append(b.handlers, handler)
	b.mu.Unlock()
}

func (b *TranscriptEventBus) Publish(ctx context.Context, update TranscriptUpdate) error {
	b.mu.RLock()
	handlers := append([]TranscriptEventHandler(nil), b.handlers...)
	b.mu.RUnlock()

	var errs []error
	for _, handler := range handlers {
		if handler == nil {
			continue
		}
		func() {
			defer func() {
				if rec := recover(); rec != nil {
					errs = append(errs, fmt.Errorf("transcript event handler panic: %v", rec))
				}
			}()
			if err := handler.HandleTranscriptUpdate(ctx, update); err != nil {
				errs = append(errs, err)
			}
		}()
	}
	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("transcript event publish had %d error(s): %v", len(errs), errs[0])
}
