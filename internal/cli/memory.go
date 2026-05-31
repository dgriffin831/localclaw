package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dgriffin831/localclaw/internal/config"
	"github.com/dgriffin831/localclaw/internal/memory"
	"github.com/dgriffin831/localclaw/internal/runtime"
)

var errMissingMemorySubcommand = errors.New("memory subcommand is required")

type statusOutput struct {
	Command   string          `json:"command"`
	AgentID   string          `json:"agentId"`
	Workspace string          `json:"workspace"`
	StorePath string          `json:"storePath"`
	DBPath    string          `json:"dbPath"`
	Dirty     bool            `json:"dirty"`
	Index     indexSnapshot   `json:"index"`
	Features  featureSnapshot `json:"features"`
	Vector    vectorSnapshot  `json:"vector"`
	Sources   sourceSnapshot  `json:"sources"`
	Scan      scanSnapshot    `json:"scan"`
	Sync      *syncSnapshot   `json:"sync,omitempty"`
}

type indexOutput struct {
	Command   string        `json:"command"`
	AgentID   string        `json:"agentId"`
	Force     bool          `json:"force"`
	Workspace string        `json:"workspace"`
	StorePath string        `json:"storePath"`
	DBPath    string        `json:"dbPath"`
	Sync      syncSnapshot  `json:"sync"`
	Index     indexSnapshot `json:"index"`
}

type searchOutput struct {
	Command     string                `json:"command"`
	AgentID     string                `json:"agentId"`
	Query       string                `json:"query"`
	Mode        string                `json:"mode"`
	MaxResults  int                   `json:"maxResults"`
	MinScore    float64               `json:"minScore"`
	Warning     string                `json:"warning,omitempty"`
	ResultCount int                   `json:"resultCount"`
	Results     []memory.SearchResult `json:"results"`
}

type grepOutput struct {
	Command       string             `json:"command"`
	AgentID       string             `json:"agentId"`
	Query         string             `json:"query"`
	Mode          string             `json:"mode"`
	CaseSensitive bool               `json:"caseSensitive"`
	Word          bool               `json:"word"`
	MaxMatches    int                `json:"maxMatches"`
	ContextLines  int                `json:"contextLines"`
	PathGlob      []string           `json:"pathGlob,omitempty"`
	Source        string             `json:"source"`
	Count         int                `json:"count"`
	Matches       []memory.GrepMatch `json:"matches"`
}

type indexSnapshot struct {
	FileCount  int `json:"fileCount"`
	ChunkCount int `json:"chunkCount"`
}

type featureSnapshot struct {
	FTSEnabled    bool `json:"ftsEnabled"`
	VectorEnabled bool `json:"vectorEnabled"`
}

type vectorSnapshot struct {
	Ready            bool   `json:"ready"`
	ModelID          string `json:"modelId"`
	ModelPath        string `json:"modelPath"`
	ModelPresent     bool   `json:"modelPresent"`
	ModelSHA256      string `json:"modelSha256,omitempty"`
	LlamaServerFound bool   `json:"llamaServerFound"`
	VectorCount      int    `json:"vectorCount"`
	Dimension        int    `json:"dimension"`
	SearchMode       string `json:"searchMode"`
	LastError        string `json:"lastError,omitempty"`
}

type sourceSnapshot struct {
	Configured []string `json:"configured"`
	Memory     int      `json:"memory"`
	Extra      int      `json:"extra"`
}

type scanSnapshot struct {
	Deep         bool     `json:"deep"`
	ScannedFiles int      `json:"scannedFiles"`
	Issues       []string `json:"issues"`
}

type syncSnapshot struct {
	ScannedFiles   int    `json:"scannedFiles"`
	IndexedFiles   int    `json:"indexedFiles"`
	SkippedFiles   int    `json:"skippedFiles"`
	RemovedFiles   int    `json:"removedFiles"`
	IndexedChunks  int    `json:"indexedChunks"`
	IndexedVectors int    `json:"indexedVectors"`
	VectorError    string `json:"vectorError,omitempty"`
}

// RunMemoryCommand executes localclaw memory status/index/search/grep commands.
func RunMemoryCommand(ctx context.Context, cfg config.Config, app *runtime.App, args []string, stdout, stderr io.Writer) error {
	if stdout == nil {
		stdout = os.Stdout
	}
	if stderr == nil {
		stderr = os.Stderr
	}
	if len(args) == 0 {
		return errMissingMemorySubcommand
	}

	switch args[0] {
	case "status":
		return runMemoryStatus(ctx, cfg, app, args[1:], stdout, stderr)
	case "index":
		return runMemoryIndex(ctx, cfg, app, args[1:], stdout, stderr)
	case "search":
		return runMemorySearch(ctx, cfg, app, args[1:], stdout, stderr)
	case "grep":
		return runMemoryGrep(ctx, cfg, app, args[1:], stdout, stderr)
	case "model":
		return runMemoryModel(ctx, cfg, app, args[1:], stdout, stderr)
	default:
		return fmt.Errorf("unknown memory subcommand %q (supported: status, index, search, grep, model)", args[0])
	}
}

func runMemoryStatus(ctx context.Context, cfg config.Config, app *runtime.App, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("memory status", flag.ContinueOnError)
	fs.SetOutput(stderr)
	agentID := fs.String("agent", "", "agent id")
	deep := fs.Bool("deep", false, "include source scan diagnostics")
	reindex := fs.Bool("index", false, "sync index before reporting status")
	asJSON := fs.Bool("json", false, "emit JSON output")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(fs.Args()) != 0 {
		return fmt.Errorf("memory status does not accept positional arguments")
	}

	resolved, manager, scan, err := newMemoryCommandContext(ctx, cfg, app, *agentID, *deep)
	if err != nil {
		return err
	}
	defer manager.Close()

	var syncRes memory.SyncResult
	var didSync bool
	if *reindex {
		syncRes, err = manager.Sync(ctx, false)
		if err != nil {
			return fmt.Errorf("memory index sync: %w", err)
		}
		didSync = true
		scan.ScannedFiles = syncRes.ScannedFiles
	}

	status, err := manager.Status(ctx)
	if err != nil {
		return fmt.Errorf("memory status: %w", err)
	}

	out := statusOutput{
		Command:   "memory status",
		AgentID:   resolved.agentID,
		Workspace: resolved.workspacePath,
		StorePath: resolved.storePath,
		DBPath:    status.DBPath,
		Index: indexSnapshot{
			FileCount:  status.FileCount,
			ChunkCount: status.ChunkCount,
		},
		Features: featureSnapshot{
			FTSEnabled:    status.FTSEnabled,
			VectorEnabled: status.Vector.Enabled,
		},
		Vector: vectorSnapshotFromStatus(status.Vector),
		Sources: sourceSnapshot{
			Configured: append([]string{}, resolved.sources...),
			Memory:     scan.MemoryFiles,
			Extra:      scan.ExtraFiles,
		},
		Scan: scanSnapshot{
			Deep:         *deep,
			ScannedFiles: scan.ScannedFiles,
			Issues:       append([]string{}, scan.Issues...),
		},
	}
	if didSync {
		out.Sync = &syncSnapshot{
			ScannedFiles:   syncRes.ScannedFiles,
			IndexedFiles:   syncRes.IndexedFiles,
			SkippedFiles:   syncRes.SkippedFiles,
			RemovedFiles:   syncRes.RemovedFiles,
			IndexedChunks:  syncRes.IndexedChunks,
			IndexedVectors: syncRes.IndexedVectors,
			VectorError:    syncRes.VectorError,
		}
	}
	out.Dirty = scanDirty(out, didSync)

	if *asJSON {
		return writeJSON(stdout, out)
	}

	fmt.Fprintf(stdout, "agent: %s\n", out.AgentID)
	fmt.Fprintf(stdout, "workspace: %s\n", out.Workspace)
	fmt.Fprintf(stdout, "store: %s\n", out.StorePath)
	fmt.Fprintf(stdout, "db: %s\n", out.DBPath)
	fmt.Fprintf(stdout, "index: files=%d chunks=%d fts=%t\n", out.Index.FileCount, out.Index.ChunkCount, out.Features.FTSEnabled)
	fmt.Fprintf(stdout, "vector: enabled=%t ready=%t vectors=%d dimension=%d mode=%s\n", out.Features.VectorEnabled, out.Vector.Ready, out.Vector.VectorCount, out.Vector.Dimension, out.Vector.SearchMode)
	if out.Vector.LastError != "" {
		fmt.Fprintf(stdout, "vector warning: %s\n", out.Vector.LastError)
	}
	fmt.Fprintf(stdout, "sources: memory=%d extra=%d\n", out.Sources.Memory, out.Sources.Extra)
	fmt.Fprintf(stdout, "dirty: %t\n", out.Dirty)
	if out.Sync != nil {
		fmt.Fprintf(stdout, "sync: scanned=%d indexed=%d skipped=%d removed=%d chunks=%d\n", out.Sync.ScannedFiles, out.Sync.IndexedFiles, out.Sync.SkippedFiles, out.Sync.RemovedFiles, out.Sync.IndexedChunks)
	}
	if *deep {
		if len(out.Scan.Issues) == 0 {
			fmt.Fprintln(stdout, "source scan diagnostics: none")
		} else {
			fmt.Fprintln(stdout, "source scan diagnostics:")
			for _, issue := range out.Scan.Issues {
				fmt.Fprintf(stdout, "- %s\n", issue)
			}
		}
	}
	return nil
}

func runMemoryIndex(ctx context.Context, cfg config.Config, app *runtime.App, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("memory index", flag.ContinueOnError)
	fs.SetOutput(stderr)
	agentID := fs.String("agent", "", "agent id")
	force := fs.Bool("force", false, "force full reindex")
	asJSON := fs.Bool("json", false, "emit JSON output")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(fs.Args()) != 0 {
		return fmt.Errorf("memory index does not accept positional arguments")
	}

	resolved, manager, _, err := newMemoryCommandContext(ctx, cfg, app, *agentID, false)
	if err != nil {
		return err
	}
	defer manager.Close()

	syncRes, err := manager.Sync(ctx, *force)
	if err != nil {
		return fmt.Errorf("memory index sync: %w", err)
	}
	status, err := manager.Status(ctx)
	if err != nil {
		return fmt.Errorf("memory status: %w", err)
	}

	out := indexOutput{
		Command:   "memory index",
		AgentID:   resolved.agentID,
		Force:     *force,
		Workspace: resolved.workspacePath,
		StorePath: resolved.storePath,
		DBPath:    status.DBPath,
		Sync: syncSnapshot{
			ScannedFiles:   syncRes.ScannedFiles,
			IndexedFiles:   syncRes.IndexedFiles,
			SkippedFiles:   syncRes.SkippedFiles,
			RemovedFiles:   syncRes.RemovedFiles,
			IndexedChunks:  syncRes.IndexedChunks,
			IndexedVectors: syncRes.IndexedVectors,
			VectorError:    syncRes.VectorError,
		},
		Index: indexSnapshot{FileCount: status.FileCount, ChunkCount: status.ChunkCount},
	}

	if *asJSON {
		return writeJSON(stdout, out)
	}
	fmt.Fprintf(stdout, "agent: %s\n", out.AgentID)
	fmt.Fprintf(stdout, "workspace: %s\n", out.Workspace)
	fmt.Fprintf(stdout, "db: %s\n", out.DBPath)
	fmt.Fprintf(stdout, "sync: scanned=%d indexed=%d skipped=%d removed=%d chunks=%d vectors=%d\n", out.Sync.ScannedFiles, out.Sync.IndexedFiles, out.Sync.SkippedFiles, out.Sync.RemovedFiles, out.Sync.IndexedChunks, out.Sync.IndexedVectors)
	if out.Sync.VectorError != "" {
		fmt.Fprintf(stdout, "vector warning: %s\n", out.Sync.VectorError)
	}
	fmt.Fprintf(stdout, "index: files=%d chunks=%d\n", out.Index.FileCount, out.Index.ChunkCount)
	return nil
}

func runMemorySearch(ctx context.Context, cfg config.Config, app *runtime.App, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("memory search", flag.ContinueOnError)
	fs.SetOutput(stderr)
	agentID := fs.String("agent", "", "agent id")
	maxResults := fs.Int("max-results", 0, "max results")
	minScore := fs.Float64("min-score", 0, "minimum score")
	mode := fs.String("mode", "", "search mode: hybrid, keyword, or vector")
	asJSON := fs.Bool("json", false, "emit JSON output")
	flagArgs, query, err := splitSearchArgs(args)
	if err != nil {
		return err
	}
	if err := fs.Parse(flagArgs); err != nil {
		return err
	}
	if query == "" {
		return errors.New("memory search query is required")
	}

	resolved, manager, _, err := newMemoryCommandContext(ctx, cfg, app, *agentID, false)
	if err != nil {
		return err
	}
	defer manager.Close()

	searchOpts := memory.SearchOptions{MaxResults: *maxResults, MinScore: *minScore, Mode: *mode}
	if searchOpts.MaxResults <= 0 {
		searchOpts.MaxResults = resolved.queryMaxResults
	}

	results, err := manager.Search(ctx, query, searchOpts)
	if err != nil {
		return fmt.Errorf("memory search: %w", err)
	}

	out := searchOutput{
		Command:     "memory search",
		AgentID:     resolved.agentID,
		Query:       query,
		Mode:        normalizeSearchModeForOutput(searchOpts.Mode, resolved.vectorSearchMode),
		MaxResults:  searchOpts.MaxResults,
		MinScore:    searchOpts.MinScore,
		ResultCount: len(results),
		Results:     results,
	}
	if len(results) > 0 {
		out.Warning = results[0].Warning
	}
	if *asJSON {
		return writeJSON(stdout, out)
	}

	if len(results) == 0 {
		fmt.Fprintln(stdout, "no memory results")
		return nil
	}
	if out.Warning != "" {
		fmt.Fprintf(stdout, "warning: %s\n", out.Warning)
	}
	for i, res := range results {
		fmt.Fprintf(stdout, "%d. %s:%d score=%.4f source=%s\n", i+1, res.Path, res.StartLine, res.Score, res.Source)
		fmt.Fprintf(stdout, "   %s\n", strings.TrimSpace(res.Snippet))
	}
	return nil
}

func runMemoryGrep(ctx context.Context, cfg config.Config, app *runtime.App, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("memory grep", flag.ContinueOnError)
	fs.SetOutput(stderr)
	agentID := fs.String("agent", "", "agent id")
	mode := fs.String("mode", "", "match mode: literal or regex")
	caseSensitive := fs.Bool("case-sensitive", false, "case sensitive matching")
	word := fs.Bool("word", false, "match whole words only (literal mode)")
	maxMatches := fs.Int("max-matches", 0, "max matches")
	contextLines := fs.Int("context-lines", 0, "context lines before/after each match")
	source := fs.String("source", "", "source filter: memory or all")
	asJSON := fs.Bool("json", false, "emit JSON output")
	var pathGlob stringSliceFlag
	fs.Var(&pathGlob, "path-glob", "workspace-relative glob filter (repeatable)")

	flagArgs, query, err := splitGrepArgs(args)
	if err != nil {
		return err
	}
	if err := fs.Parse(flagArgs); err != nil {
		return err
	}
	if query == "" {
		return errors.New("memory grep query is required")
	}

	resolved, manager, _, err := newMemoryCommandContext(ctx, cfg, app, *agentID, false)
	if err != nil {
		return err
	}
	defer manager.Close()

	opts := memory.GrepOptions{
		Mode:          *mode,
		CaseSensitive: *caseSensitive,
		Word:          *word,
		MaxMatches:    *maxMatches,
		ContextLines:  *contextLines,
		PathGlob:      append([]string{}, pathGlob...),
		Source:        *source,
	}
	opts = normalizeCLIGrepOptions(opts)
	result, err := manager.Grep(ctx, query, opts)
	if err != nil {
		return fmt.Errorf("memory grep: %w", err)
	}

	out := grepOutput{
		Command:       "memory grep",
		AgentID:       resolved.agentID,
		Query:         query,
		Mode:          opts.Mode,
		CaseSensitive: opts.CaseSensitive,
		Word:          opts.Word,
		MaxMatches:    opts.MaxMatches,
		ContextLines:  opts.ContextLines,
		PathGlob:      append([]string{}, opts.PathGlob...),
		Source:        opts.Source,
		Count:         result.Count,
		Matches:       result.Matches,
	}
	if *asJSON {
		return writeJSON(stdout, out)
	}

	if len(out.Matches) == 0 {
		fmt.Fprintln(stdout, "no memory matches")
		return nil
	}
	for i, match := range out.Matches {
		fmt.Fprintf(stdout, "%d. %s:%d source=%s\n", i+1, match.Path, match.Line, match.Source)
		fmt.Fprintf(stdout, "   %s\n", strings.TrimSpace(match.Text))
	}
	return nil
}

type modelStatusOutput struct {
	Command string             `json:"command"`
	AgentID string             `json:"agentId"`
	Model   memory.ModelStatus `json:"model"`
}

type modelInstallOutput struct {
	Command string                    `json:"command"`
	AgentID string                    `json:"agentId"`
	Result  memory.ModelInstallResult `json:"result"`
}

func runMemoryModel(ctx context.Context, cfg config.Config, app *runtime.App, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("memory model subcommand is required")
	}
	switch args[0] {
	case "status":
		return runMemoryModelStatus(ctx, cfg, app, args[1:], stdout, stderr)
	case "install":
		return runMemoryModelInstall(ctx, cfg, app, args[1:], stdout, stderr)
	default:
		return fmt.Errorf("unknown memory model subcommand %q (supported: status, install)", args[0])
	}
}

func runMemoryModelStatus(ctx context.Context, cfg config.Config, app *runtime.App, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("memory model status", flag.ContinueOnError)
	fs.SetOutput(stderr)
	agentID := fs.String("agent", "", "agent id")
	asJSON := fs.Bool("json", false, "emit JSON output")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(fs.Args()) != 0 {
		return fmt.Errorf("memory model status does not accept positional arguments")
	}
	resolvedAgent := runtime.ResolveAgentID(*agentID)
	if _, err := app.ResolveWorkspacePath(resolvedAgent); err != nil {
		return fmt.Errorf("resolve workspace: %w", err)
	}
	memoryCfg := runtime.ResolveMemoryConfig(cfg, resolvedAgent)
	status := memory.VectorModelStatus(toMemoryVectorConfig(memoryCfg.Vector))
	out := modelStatusOutput{Command: "memory model status", AgentID: resolvedAgent, Model: status}
	if *asJSON {
		return writeJSON(stdout, out)
	}
	fmt.Fprintf(stdout, "agent: %s\n", out.AgentID)
	fmt.Fprintf(stdout, "model: %s\n", out.Model.Path)
	fmt.Fprintf(stdout, "exists: %t\n", out.Model.Exists)
	fmt.Fprintf(stdout, "verified: %t\n", out.Model.Verified)
	if out.Model.SHA256 != "" {
		fmt.Fprintf(stdout, "sha256: %s\n", out.Model.SHA256)
	}
	fmt.Fprintf(stdout, "expected_sha256: %s\n", out.Model.ExpectedSHA256)
	fmt.Fprintf(stdout, "llama-server: %t\n", out.Model.LlamaServerFound)
	fmt.Fprintf(stdout, "primary_url: %s\n", out.Model.PrimaryURL)
	fmt.Fprintf(stdout, "mirror_url: %s\n", out.Model.MirrorURL)
	return nil
}

func runMemoryModelInstall(ctx context.Context, cfg config.Config, app *runtime.App, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("memory model install", flag.ContinueOnError)
	fs.SetOutput(stderr)
	agentID := fs.String("agent", "", "agent id")
	source := fs.String("source", memory.ModelSourceAuto, "download source: auto, huggingface, or mirror")
	asJSON := fs.Bool("json", false, "emit JSON output")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(fs.Args()) != 0 {
		return fmt.Errorf("memory model install does not accept positional arguments")
	}
	resolvedAgent := runtime.ResolveAgentID(*agentID)
	if _, err := app.ResolveWorkspacePath(resolvedAgent); err != nil {
		return fmt.Errorf("resolve workspace: %w", err)
	}
	memoryCfg := runtime.ResolveMemoryConfig(cfg, resolvedAgent)
	result, err := memory.InstallVectorModel(ctx, toMemoryVectorConfig(memoryCfg.Vector), memory.ModelInstallOptions{Source: *source})
	if err != nil {
		return err
	}
	out := modelInstallOutput{Command: "memory model install", AgentID: resolvedAgent, Result: result}
	if *asJSON {
		return writeJSON(stdout, out)
	}
	fmt.Fprintf(stdout, "agent: %s\n", out.AgentID)
	fmt.Fprintf(stdout, "installed: %t\n", out.Result.Installed)
	fmt.Fprintf(stdout, "source: %s\n", out.Result.Source)
	fmt.Fprintf(stdout, "url: %s\n", out.Result.URL)
	fmt.Fprintf(stdout, "path: %s\n", out.Result.Path)
	fmt.Fprintf(stdout, "bytes: %d\n", out.Result.Bytes)
	fmt.Fprintf(stdout, "sha256: %s\n", out.Result.SHA256)
	fmt.Fprintf(stdout, "llama-server: %t\n", out.Result.LlamaServerFound)
	return nil
}

func normalizeCLIGrepOptions(opts memory.GrepOptions) memory.GrepOptions {
	normalized := opts
	normalized.Mode = strings.ToLower(strings.TrimSpace(normalized.Mode))
	if normalized.Mode == "" {
		normalized.Mode = "literal"
	}
	if normalized.Mode == "regex" {
		normalized.Word = false
	}
	if normalized.MaxMatches <= 0 {
		normalized.MaxMatches = 50
	}
	if normalized.MaxMatches > 500 {
		normalized.MaxMatches = 500
	}
	if normalized.ContextLines < 0 {
		normalized.ContextLines = 0
	}
	if normalized.ContextLines > 5 {
		normalized.ContextLines = 5
	}
	normalized.Source = strings.ToLower(strings.TrimSpace(normalized.Source))
	if normalized.Source == "" {
		normalized.Source = "all"
	}
	return normalized
}

func vectorSnapshotFromStatus(status memory.VectorStatus) vectorSnapshot {
	return vectorSnapshot{
		Ready:            status.Ready,
		ModelID:          status.ModelID,
		ModelPath:        status.ModelPath,
		ModelPresent:     status.ModelPresent,
		ModelSHA256:      status.ModelSHA256,
		LlamaServerFound: status.LlamaServerFound,
		VectorCount:      status.VectorCount,
		Dimension:        status.Dimension,
		SearchMode:       status.SearchMode,
		LastError:        status.LastError,
	}
}

func normalizeSearchModeForOutput(mode, fallback string) string {
	normalized := strings.ToLower(strings.TrimSpace(mode))
	if normalized == "" {
		normalized = strings.ToLower(strings.TrimSpace(fallback))
	}
	if normalized == "" {
		return memory.SearchModeHybrid
	}
	switch normalized {
	case memory.SearchModeHybrid, memory.SearchModeKeyword, memory.SearchModeVector:
		return normalized
	default:
		return memory.SearchModeHybrid
	}
}

func toMemoryVectorConfig(cfg config.VectorConfig) memory.VectorConfig {
	return memory.VectorConfig{
		Enabled:    cfg.Enabled,
		Provider:   cfg.Provider,
		SearchMode: cfg.SearchMode,
		Model: memory.VectorModelConfig{
			ID:         cfg.Model.ID,
			Path:       cfg.Model.Path,
			PrimaryURL: cfg.Model.PrimaryURL,
			MirrorURL:  cfg.Model.MirrorURL,
			SHA256:     cfg.Model.SHA256,
		},
		Server: memory.VectorServerConfig{
			Managed:               cfg.Server.Managed,
			Binary:                cfg.Server.Binary,
			Host:                  cfg.Server.Host,
			Port:                  cfg.Server.Port,
			StartupTimeoutSeconds: cfg.Server.StartupTimeoutSeconds,
		},
	}
}

func splitSearchArgs(args []string) ([]string, string, error) {
	flagArgs := make([]string, 0, len(args))
	queryParts := make([]string, 0, len(args))

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--agent", "--max-results", "--min-score":
			if i+1 >= len(args) {
				return nil, "", fmt.Errorf("flag %s requires a value", arg)
			}
			flagArgs = append(flagArgs, arg, args[i+1])
			i++
		case "--mode":
			if i+1 >= len(args) {
				return nil, "", fmt.Errorf("flag %s requires a value", arg)
			}
			flagArgs = append(flagArgs, arg, args[i+1])
			i++
		case "--json":
			flagArgs = append(flagArgs, arg)
		default:
			if strings.HasPrefix(arg, "-") {
				return nil, "", fmt.Errorf("unknown flag %q", arg)
			}
			queryParts = append(queryParts, arg)
		}
	}

	query := strings.TrimSpace(strings.Join(queryParts, " "))
	return flagArgs, query, nil
}

func splitGrepArgs(args []string) ([]string, string, error) {
	flagArgs := make([]string, 0, len(args))
	queryParts := make([]string, 0, len(args))

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--agent", "--mode", "--max-matches", "--context-lines", "--path-glob", "--source":
			if i+1 >= len(args) {
				return nil, "", fmt.Errorf("flag %s requires a value", arg)
			}
			flagArgs = append(flagArgs, arg, args[i+1])
			i++
		case "--json", "--case-sensitive", "--word":
			flagArgs = append(flagArgs, arg)
		default:
			if strings.HasPrefix(arg, "-") {
				return nil, "", fmt.Errorf("unknown flag %q", arg)
			}
			queryParts = append(queryParts, arg)
		}
	}

	query := strings.TrimSpace(strings.Join(queryParts, " "))
	return flagArgs, query, nil
}

type stringSliceFlag []string

func (s *stringSliceFlag) String() string {
	return strings.Join(*s, ",")
}

func (s *stringSliceFlag) Set(value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return errors.New("path glob cannot be empty")
	}
	*s = append(*s, trimmed)
	return nil
}

type memoryCommandResolution struct {
	agentID          string
	sources          []string
	workspacePath    string
	storePath        string
	queryMaxResults  int
	vectorSearchMode string
}

type sourceScanDetails struct {
	ScannedFiles int
	MemoryFiles  int
	ExtraFiles   int
	Issues       []string
}

func newMemoryCommandContext(ctx context.Context, cfg config.Config, app *runtime.App, agentID string, deep bool) (memoryCommandResolution, *memory.SQLiteIndexManager, sourceScanDetails, error) {
	resolvedAgent := runtime.ResolveAgentID(agentID)
	workspacePath, err := app.ResolveWorkspacePath(resolvedAgent)
	if err != nil {
		return memoryCommandResolution{}, nil, sourceScanDetails{}, fmt.Errorf("resolve workspace: %w", err)
	}
	memoryCfg := runtime.ResolveMemoryConfig(cfg, resolvedAgent)
	storePath, err := resolveStorePath(cfg.App.Root, memoryCfg.Store.Path, resolvedAgent)
	if err != nil {
		return memoryCommandResolution{}, nil, sourceScanDetails{}, fmt.Errorf("resolve memory store path: %w", err)
	}

	sourceSet := normalizeSources(memoryCfg.Sources)
	allowMemorySource := sourceSet["memory"]

	extraPaths := append([]string{}, memoryCfg.ExtraPaths...)
	if !allowMemorySource {
		extraPaths = nil
	}

	manager := memory.NewSQLiteIndexManager(memory.IndexManagerConfig{
		DBPath:        storePath,
		WorkspaceRoot: workspacePath,
		Sources:       memoryCfg.Sources,
		ExtraPaths:    extraPaths,
		ChunkTokens:   memoryCfg.Chunking.Tokens,
		ChunkOverlap:  memoryCfg.Chunking.Overlap,
		EnableFTS:     true,
		Vector:        toMemoryVectorConfig(memoryCfg.Vector),
	})
	if err := manager.Open(ctx); err != nil {
		return memoryCommandResolution{}, nil, sourceScanDetails{}, fmt.Errorf("open memory index: %w", err)
	}

	scan := sourceScanDetails{}
	if deep {
		scan = scanSources(workspacePath, memoryCfg.Sources, memoryCfg.ExtraPaths)
	} else {
		scan.Issues = []string{}
	}

	configuredSources := make([]string, 0, len(sourceSet))
	for source := range sourceSet {
		configuredSources = append(configuredSources, source)
	}
	sort.Strings(configuredSources)
	if len(configuredSources) == 0 {
		configuredSources = []string{"memory"}
	}

	resolution := memoryCommandResolution{
		agentID:          resolvedAgent,
		sources:          configuredSources,
		workspacePath:    workspacePath,
		storePath:        storePath,
		queryMaxResults:  memoryCfg.Query.MaxResults,
		vectorSearchMode: memoryCfg.Vector.SearchMode,
	}
	if resolution.queryMaxResults <= 0 {
		resolution.queryMaxResults = 8
	}
	return resolution, manager, scan, nil
}

func scanSources(workspacePath string, sources []string, extraPaths []string) sourceScanDetails {
	result := sourceScanDetails{Issues: []string{}}
	sourceSet := normalizeSources(sources)

	if len(sourceSet) == 0 {
		sourceSet["memory"] = true
	}

	for source := range sourceSet {
		switch source {
		case "memory":
		default:
			result.Issues = append(result.Issues, fmt.Sprintf("unsupported source %q", source))
		}
	}

	memoryFiles := []memory.MemoryFile{}
	if sourceSet["memory"] {
		files, err := memory.DiscoverMemoryFiles(workspacePath, extraPaths)
		if err != nil {
			result.Issues = append(result.Issues, fmt.Sprintf("memory source scan failed: %v", err))
		} else {
			memoryFiles = files
			result.ScannedFiles = len(files)
		}
	}

	extraSet := map[string]struct{}{}
	for _, raw := range extraPaths {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			result.Issues = append(result.Issues, "extra path is empty")
			continue
		}
		resolved := trimmed
		if !filepath.IsAbs(trimmed) {
			resolved = filepath.Join(workspacePath, trimmed)
		}
		info, err := os.Lstat(filepath.Clean(resolved))
		if err != nil {
			if os.IsNotExist(err) {
				result.Issues = append(result.Issues, fmt.Sprintf("extra path %q does not exist", trimmed))
				continue
			}
			result.Issues = append(result.Issues, fmt.Sprintf("extra path %q stat failed: %v", trimmed, err))
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			result.Issues = append(result.Issues, fmt.Sprintf("extra path %q is a symlink and will be ignored", trimmed))
		}
		extraSet[filepath.Clean(resolved)] = struct{}{}
	}

	for _, file := range memoryFiles {
		if strings.HasPrefix(strings.ToLower(file.RelativePath), "memory/") || strings.EqualFold(file.RelativePath, "memory.md") || strings.EqualFold(file.RelativePath, "MEMORY.md") {
			result.MemoryFiles++
			continue
		}
		fileAbs := filepath.Clean(file.AbsolutePath)
		for extraRoot := range extraSet {
			if fileAbs == extraRoot || strings.HasPrefix(fileAbs, extraRoot+string(filepath.Separator)) {
				result.ExtraFiles++
				goto counted
			}
		}
		result.MemoryFiles++
	counted:
	}

	sort.Strings(result.Issues)
	return result
}

func normalizeSources(values []string) map[string]bool {
	out := map[string]bool{}
	for _, raw := range values {
		source := strings.ToLower(strings.TrimSpace(raw))
		if source == "" {
			continue
		}
		out[source] = true
	}
	return out
}

func resolveStorePath(stateRoot string, storePattern string, agentID string) (string, error) {
	pattern := strings.TrimSpace(storePattern)
	if pattern == "" {
		return "", errors.New("memory.store.path is required")
	}

	pattern = strings.ReplaceAll(pattern, "{agentId}", agentID)
	resolved, err := expandPath(pattern)
	if err != nil {
		return "", err
	}
	if filepath.IsAbs(resolved) {
		return filepath.Clean(resolved), nil
	}

	root, err := expandPath(stateRoot)
	if err != nil {
		return "", err
	}
	return filepath.Clean(filepath.Join(root, resolved)), nil
}

func expandPath(path string) (string, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return "", errors.New("path is empty")
	}
	if trimmed == "~" || strings.HasPrefix(trimmed, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if trimmed == "~" {
			return filepath.Clean(home), nil
		}
		return filepath.Clean(filepath.Join(home, strings.TrimPrefix(trimmed, "~/"))), nil
	}
	return filepath.Clean(trimmed), nil
}

func writeJSON(w io.Writer, payload interface{}) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

func scanDirty(status statusOutput, didSync bool) bool {
	if didSync {
		if status.Sync == nil {
			return false
		}
		return status.Sync.IndexedFiles > 0 || status.Sync.RemovedFiles > 0
	}
	if !status.Scan.Deep {
		return false
	}
	if len(status.Scan.Issues) > 0 {
		return true
	}
	if status.Scan.ScannedFiles != status.Index.FileCount {
		return true
	}
	return false
}
