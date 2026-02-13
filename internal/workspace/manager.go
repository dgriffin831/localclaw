package workspace

import "context"

// Manager controls local workspace operations.
type Manager interface {
	Init(ctx context.Context) error
	Root() string
}

type LocalManager struct {
	root string
}

func NewLocalManager(root string) *LocalManager {
	return &LocalManager{root: root}
}

func (m *LocalManager) Init(ctx context.Context) error {
	return nil
}

func (m *LocalManager) Root() string {
	return m.root
}
