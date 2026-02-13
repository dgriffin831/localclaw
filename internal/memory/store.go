package memory

import "context"

// Store defines local memory persistence behavior.
type Store interface {
	Init(ctx context.Context) error
	Save(ctx context.Context, key string, value string) error
	Load(ctx context.Context, key string) (string, error)
}

type LocalStore struct {
	path string
}

func NewLocalStore(path string) *LocalStore {
	return &LocalStore{path: path}
}

func (s *LocalStore) Init(ctx context.Context) error {
	return nil
}

func (s *LocalStore) Save(ctx context.Context, key string, value string) error {
	return nil
}

func (s *LocalStore) Load(ctx context.Context, key string) (string, error) {
	return "", nil
}
