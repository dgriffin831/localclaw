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
	Fallback             string
	Local                LocalEmbeddingConfig
	EnableFTS            bool
	EnableVector         bool
	EnableEmbeddingCache bool
	HybridEnabled        bool
	VectorWeight         float64
	KeywordWeight        float64
	CandidateMultiplier  int
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

// SearchOptions controls query ranking and filtering behavior.
type SearchOptions struct {
	MaxResults int
	MinScore   float64
	SessionKey string
}

// SearchResult is one memory chunk match.
type SearchResult struct {
	Path      string
	StartLine int
	EndLine   int
	Score     float64
	Snippet   string
	Source    string
}

// GetOptions controls line-sliced file reads for memory_get semantics.
type GetOptions struct {
	FromLine int
	Lines    int
}

// GetResult is a safe memory file read response.
type GetResult struct {
	Path      string
	StartLine int
	EndLine   int
	Content   string
	Source    string
}

// IndexManager defines the SQLite-backed memory indexing behavior.
type IndexManager interface {
	Open(ctx context.Context) error
	Close() error
	InstallSchema(ctx context.Context) error
	Sync(ctx context.Context, force bool) (SyncResult, error)
	Status(ctx context.Context) (IndexStatus, error)
	Search(ctx context.Context, query string, opts SearchOptions) ([]SearchResult, error)
	Get(ctx context.Context, path string, opts GetOptions) (GetResult, error)
}
