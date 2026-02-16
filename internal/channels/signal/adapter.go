package signal

import "context"

// Client is the Signal channel boundary.
type Client interface {
	Send(ctx context.Context, text string) error
}

type LocalAdapter struct{}

func NewLocalAdapter() *LocalAdapter {
	return &LocalAdapter{}
}

func (a *LocalAdapter) Send(ctx context.Context, text string) error {
	// TODO: Implement real Signal delivery when runtime channel dispatch is wired.
	return nil
}
