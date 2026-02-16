package memory

import (
	"context"
	"time"
)

const (
	defaultChunkTokens  = 400
	defaultChunkOverlap = 40
)

// IndexManagerConfig controls SQLite memory index behavior.
type IndexManagerConfig struct {
	DBPath               string
	WorkspaceRoot        string
	SessionsRoot         string
	Sources              []string
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
	EmbeddingCacheMax    int
	SessionDeltaBytes    int
	SessionDeltaMessages int
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

// AutoSyncConfig controls background watch/interval sync behavior.
type AutoSyncConfig struct {
	Watch             bool
	WatchDebounce     time.Duration
	SessionDebounce   time.Duration
	WatchPollInterval time.Duration
	Interval          time.Duration
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

// GrepOptions controls grep-style retrieval for exact or regex matches.
type GrepOptions struct {
	Mode          string
	CaseSensitive bool
	Word          bool
	MaxMatches    int
	ContextLines  int
	PathGlob      []string
	Source        string
}

// GrepMatch captures one matching line and optional surrounding context.
type GrepMatch struct {
	Path   string   `json:"path"`
	Line   int      `json:"line"`
	Start  int      `json:"start,omitempty"`
	End    int      `json:"end,omitempty"`
	Text   string   `json:"text"`
	Before []string `json:"before,omitempty"`
	After  []string `json:"after,omitempty"`
	Source string   `json:"source"`
}

// GrepResult is a bounded, deterministically ordered grep response payload.
type GrepResult struct {
	Count   int         `json:"count"`
	Matches []GrepMatch `json:"matches"`
}

// IndexManager defines the SQLite-backed memory indexing behavior.
type IndexManager interface {
	Open(ctx context.Context) error
	Close() error
	InstallSchema(ctx context.Context) error
	Sync(ctx context.Context, force bool) (SyncResult, error)
	StartAutoSync(ctx context.Context, cfg AutoSyncConfig) error
	StopAutoSync() error
	Status(ctx context.Context) (IndexStatus, error)
	Search(ctx context.Context, query string, opts SearchOptions) ([]SearchResult, error)
	Get(ctx context.Context, path string, opts GetOptions) (GetResult, error)
	Grep(ctx context.Context, query string, opts GrepOptions) (GrepResult, error)
}
