package memory

import "context"

const (
	defaultChunkTokens  = 400
	defaultChunkOverlap = 40
)

// IndexManagerConfig controls SQLite memory index behavior.
type IndexManagerConfig struct {
	DBPath               string
	WorkspaceRoot        string
	ExtraPaths           []string
	ChunkTokens          int
	ChunkOverlap         int
	Provider             string
	Model                string
	EnableFTS            bool
	EnableEmbeddingCache bool
}

// IndexStatus is a lightweight snapshot of indexed memory state.
type IndexStatus struct {
	DBPath                string
	FileCount             int
	ChunkCount            int
	FTSEnabled            bool
	EmbeddingCacheEnabled bool
}

// SyncResult captures indexing work from one sync pass.
type SyncResult struct {
	ScannedFiles  int
	IndexedFiles  int
	SkippedFiles  int
	RemovedFiles  int
	IndexedChunks int
}

// IndexManager defines the SQLite-backed memory indexing behavior.
type IndexManager interface {
	Open(ctx context.Context) error
	Close() error
	InstallSchema(ctx context.Context) error
	Sync(ctx context.Context, force bool) (SyncResult, error)
	Status(ctx context.Context) (IndexStatus, error)
}
