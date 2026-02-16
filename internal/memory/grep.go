package memory

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

const (
	defaultGrepMode        = "literal"
	defaultGrepMaxMatches  = 50
	maxGrepMaxMatches      = 500
	maxGrepContextLines    = 5
	grepScannerBufferBytes = 64 * 1024
	grepScannerMaxBytes    = 1024 * 1024
)

var ErrMemoryPathGlobOutOfScope = errors.New("memory path_glob is out of scope")

func (m *SQLiteIndexManager) Grep(ctx context.Context, query string, opts GrepOptions) (GrepResult, error) {
	_ = ctx
	trimmedQuery := strings.TrimSpace(query)
	if trimmedQuery == "" {
		return GrepResult{}, ErrEmptySearchQuery
	}

	normalizedOpts, err := normalizeGrepOptions(opts)
	if err != nil {
		return GrepResult{}, err
	}
	var re *regexp.Regexp
	if normalizedOpts.Mode == "regex" {
		pattern := trimmedQuery
		if !normalizedOpts.CaseSensitive {
			pattern = "(?i)" + pattern
		}
		re, err = regexp.Compile(pattern)
		if err != nil {
			return GrepResult{}, err
		}
	}

	files, err := m.discoverGrepFiles(normalizedOpts.Source)
	if err != nil {
		return GrepResult{}, err
	}
	if len(normalizedOpts.PathGlob) > 0 {
		files, err = m.filterGrepFilesByGlob(files, normalizedOpts.PathGlob)
		if err != nil {
			return GrepResult{}, err
		}
	}

	matches := make([]GrepMatch, 0, minInt(normalizedOpts.MaxMatches, 64))
	for _, file := range files {
		if len(matches) >= normalizedOpts.MaxMatches {
			break
		}

		lines, err := readFileLines(file.File.AbsolutePath)
		if err != nil {
			return GrepResult{}, fmt.Errorf("read %s: %w", file.File.AbsolutePath, err)
		}
		for i, line := range lines {
			if len(matches) >= normalizedOpts.MaxMatches {
				break
			}
			start, end, ok := grepLineMatch(line, trimmedQuery, normalizedOpts, re)
			if !ok {
				continue
			}
			before, after := lineContext(lines, i, normalizedOpts.ContextLines)
			matches = append(matches, GrepMatch{
				Path:   file.DisplayPath,
				Line:   i + 1,
				Start:  start,
				End:    end,
				Text:   line,
				Before: before,
				After:  after,
				Source: file.Source,
			})
		}
	}

	return GrepResult{
		Count:   len(matches),
		Matches: matches,
	}, nil
}

type grepCandidateFile struct {
	File        MemoryFile
	Source      string
	DisplayPath string
}

func (m *SQLiteIndexManager) discoverGrepFiles(source string) ([]grepCandidateFile, error) {
	candidates := make([]grepCandidateFile, 0)
	added := map[string]struct{}{}

	if source == "all" || source == "memory" {
		files, err := DiscoverMemoryFiles(m.cfg.WorkspaceRoot, m.cfg.ExtraPaths)
		if err != nil {
			return nil, err
		}
		for _, file := range files {
			key := filepath.Clean(file.AbsolutePath)
			if _, ok := added[key]; ok {
				continue
			}
			added[key] = struct{}{}
			candidates = append(candidates, grepCandidateFile{
				File:        file,
				Source:      classifyMemorySource(file),
				DisplayPath: m.grepDisplayPath(file, "memory"),
			})
		}
	}

	if source == "all" || source == "sessions" {
		files, err := DiscoverSessionFiles(m.cfg.SessionsRoot)
		if err != nil {
			return nil, err
		}
		for _, file := range files {
			key := filepath.Clean(file.AbsolutePath)
			if _, ok := added[key]; ok {
				continue
			}
			added[key] = struct{}{}
			candidates = append(candidates, grepCandidateFile{
				File:        file,
				Source:      "sessions",
				DisplayPath: m.grepDisplayPath(file, "sessions"),
			})
		}
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].DisplayPath != candidates[j].DisplayPath {
			return candidates[i].DisplayPath < candidates[j].DisplayPath
		}
		return candidates[i].File.AbsolutePath < candidates[j].File.AbsolutePath
	})
	return candidates, nil
}

func normalizeGrepOptions(opts GrepOptions) (GrepOptions, error) {
	normalized := opts
	normalized.Mode = strings.ToLower(strings.TrimSpace(normalized.Mode))
	if normalized.Mode == "" {
		normalized.Mode = defaultGrepMode
	}
	switch normalized.Mode {
	case "literal", "regex":
	default:
		return GrepOptions{}, fmt.Errorf("mode must be literal or regex")
	}
	if normalized.Mode == "regex" {
		normalized.Word = false
	}

	if normalized.MaxMatches <= 0 {
		normalized.MaxMatches = defaultGrepMaxMatches
	}
	if normalized.MaxMatches > maxGrepMaxMatches {
		normalized.MaxMatches = maxGrepMaxMatches
	}
	if normalized.ContextLines < 0 {
		normalized.ContextLines = 0
	}
	if normalized.ContextLines > maxGrepContextLines {
		normalized.ContextLines = maxGrepContextLines
	}

	normalized.Source = strings.ToLower(strings.TrimSpace(normalized.Source))
	if normalized.Source == "" {
		normalized.Source = "all"
	}
	switch normalized.Source {
	case "memory", "sessions", "all":
	default:
		return GrepOptions{}, fmt.Errorf("source must be memory, sessions, or all")
	}

	cleanedGlobs := make([]string, 0, len(normalized.PathGlob))
	for _, raw := range normalized.PathGlob {
		glob := strings.TrimSpace(raw)
		if glob == "" {
			continue
		}
		if filepath.IsAbs(glob) {
			return GrepOptions{}, ErrMemoryPathGlobOutOfScope
		}
		cleaned := filepath.Clean(glob)
		if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
			return GrepOptions{}, ErrMemoryPathGlobOutOfScope
		}
		cleanedGlobs = append(cleanedGlobs, filepath.ToSlash(cleaned))
	}
	normalized.PathGlob = cleanedGlobs

	return normalized, nil
}

func (m *SQLiteIndexManager) filterGrepFilesByGlob(files []grepCandidateFile, patterns []string) ([]grepCandidateFile, error) {
	filtered := make([]grepCandidateFile, 0, len(files))
	for _, file := range files {
		matched := false
		for _, pattern := range patterns {
			ok, err := filepath.Match(pattern, file.DisplayPath)
			if err != nil {
				return nil, fmt.Errorf("invalid path_glob %q: %w", pattern, err)
			}
			if ok {
				matched = true
				break
			}
		}
		if matched {
			filtered = append(filtered, file)
		}
	}
	return filtered, nil
}

func (m *SQLiteIndexManager) grepDisplayPath(file MemoryFile, source string) string {
	absPath := filepath.Clean(file.AbsolutePath)
	if source == "memory" {
		if rel, err := SafeRelativePath(m.cfg.WorkspaceRoot, absPath); err == nil {
			return rel
		}
	}
	if source == "sessions" {
		if rel, err := SafeRelativePath(m.cfg.SessionsRoot, absPath); err == nil {
			return filepath.ToSlash(filepath.Join("sessions", rel))
		}
	}
	if file.RelativePath != "" {
		return file.RelativePath
	}
	return filepath.ToSlash(absPath)
}

func readFileLines(path string) ([]string, error) {
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil, err
	}
	defer f.Close()

	lines := make([]string, 0, 64)
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, grepScannerBufferBytes), grepScannerMaxBytes)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return lines, nil
}

func grepLineMatch(line string, query string, opts GrepOptions, re *regexp.Regexp) (int, int, bool) {
	if opts.Mode == "regex" {
		if re == nil {
			return 0, 0, false
		}
		loc := re.FindStringIndex(line)
		if len(loc) != 2 {
			return 0, 0, false
		}
		return loc[0], loc[1], true
	}

	haystack := line
	needle := query
	if !opts.CaseSensitive {
		haystack = strings.ToLower(haystack)
		needle = strings.ToLower(needle)
	}
	start := strings.Index(haystack, needle)
	if start < 0 {
		return 0, 0, false
	}
	end := start + len(needle)
	if opts.Word && !isWordMatchBoundary(line, start, end) {
		return 0, 0, false
	}
	return start, end, true
}

func isWordMatchBoundary(line string, start int, end int) bool {
	runes := []rune(line)
	startRune := len([]rune(line[:start]))
	endRune := len([]rune(line[:end]))

	prevBoundary := startRune == 0 || !isWordRune(runes[startRune-1])
	nextBoundary := endRune >= len(runes) || !isWordRune(runes[endRune])
	return prevBoundary && nextBoundary
}

func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
}

func lineContext(lines []string, lineIndex int, context int) ([]string, []string) {
	if context <= 0 {
		return nil, nil
	}
	start := lineIndex - context
	if start < 0 {
		start = 0
	}
	end := lineIndex + context + 1
	if end > len(lines) {
		end = len(lines)
	}

	before := append([]string{}, lines[start:lineIndex]...)
	after := append([]string{}, lines[lineIndex+1:end]...)
	return before, after
}

func minInt(a int, b int) int {
	if a < b {
		return a
	}
	return b
}
