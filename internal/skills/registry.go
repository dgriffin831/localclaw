package skills

import "context"

// Registry tracks locally available skills.
type Registry interface {
	Load(ctx context.Context) error
	List(ctx context.Context) ([]string, error)
}

type LocalRegistry struct{}

func NewLocalRegistry() *LocalRegistry {
	return &LocalRegistry{}
}

func (r *LocalRegistry) Load(ctx context.Context) error {
	return nil
}

func (r *LocalRegistry) List(ctx context.Context) ([]string, error) {
	return []string{}, nil
}
