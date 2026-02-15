package memory

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const approxCharsPerToken = 4

var errPathOutsideRoot = errors.New("path is outside root")

// MemoryFile identifies an indexable markdown memory file.
type MemoryFile struct {
	AbsolutePath string
	RelativePath string
}

// Chunk is a deterministic text chunk and hash tuple.
type Chunk struct {
	Text string
	Hash string
}

// DiscoverMemoryFiles scans workspace memory markdown files and optional extra paths.
// It returns a deterministic, de-duplicated list sorted by absolute path.
func DiscoverMemoryFiles(workspaceRoot string, extraPaths []string) ([]MemoryFile, error) {
	rootAbs, err := filepath.Abs(filepath.Clean(workspaceRoot))
	if err != nil {
		return nil, err
	}

	seen := make(map[string]MemoryFile)

	rootCandidates := []string{
		filepath.Join(rootAbs, "MEMORY.md"),
		filepath.Join(rootAbs, "memory.md"),
	}
	for _, candidate := range rootCandidates {
		if err := addMarkdownFile(rootAbs, candidate, seen); err != nil {
			return nil, err
		}
	}

	if err := walkMarkdownTree(rootAbs, filepath.Join(rootAbs, "memory"), seen); err != nil {
		return nil, err
	}

	for _, rawPath := range extraPaths {
		resolved, err := resolveFromRoot(rootAbs, rawPath)
		if err != nil {
			return nil, err
		}
		if err := walkOrAddPath(rootAbs, resolved, seen); err != nil {
			return nil, err
		}
	}

	files := make([]MemoryFile, 0, len(seen))
	for _, file := range seen {
		files = append(files, file)
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].AbsolutePath < files[j].AbsolutePath
	})
	return files, nil
}

func walkOrAddPath(rootAbs string, path string, seen map[string]MemoryFile) error {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil
	}
	if info.IsDir() {
		return walkMarkdownTree(rootAbs, path, seen)
	}
	hasSymlink, err := pathHasSymlinkComponent(path)
	if err != nil {
		return err
	}
	if hasSymlink {
		return nil
	}
	return addMarkdownFile(rootAbs, path, seen)
}

func walkMarkdownTree(rootAbs string, dir string, seen map[string]MemoryFile) error {
	info, err := os.Lstat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil
	}
	if !info.IsDir() {
		return nil
	}

	return filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) {
				return nil
			}
			return walkErr
		}

		isSymlink, err := entryIsSymlink(d)
		if err != nil {
			return err
		}
		if isSymlink {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if d.IsDir() {
			return nil
		}
		return addMarkdownFile(rootAbs, path, seen)
	})
}

func addMarkdownFile(rootAbs string, path string, seen map[string]MemoryFile) error {
	if !isMarkdownFile(path) {
		return nil
	}

	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || info.IsDir() {
		return nil
	}

	absPath, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return err
	}
	if _, ok := seen[absPath]; ok {
		return nil
	}

	rel, err := SafeRelativePath(rootAbs, absPath)
	if err != nil {
		rel = ""
	}

	seen[absPath] = MemoryFile{AbsolutePath: absPath, RelativePath: rel}
	return nil
}

func entryIsSymlink(d fs.DirEntry) (bool, error) {
	if d.Type()&fs.ModeSymlink != 0 {
		return true, nil
	}
	if d.Type() != 0 {
		return false, nil
	}
	info, err := d.Info()
	if err != nil {
		return false, err
	}
	return info.Mode()&os.ModeSymlink != 0, nil
}

func pathHasSymlinkComponent(path string) (bool, error) {
	clean := filepath.Clean(path)
	volume := filepath.VolumeName(clean)
	remainder := strings.TrimPrefix(clean, volume)

	current := volume
	if filepath.IsAbs(clean) {
		current += string(filepath.Separator)
		remainder = strings.TrimPrefix(remainder, string(filepath.Separator))
	}

	parts := strings.Split(remainder, string(filepath.Separator))
	for _, part := range parts {
		if part == "" || part == "." {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			return false, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return true, nil
		}
	}

	return false, nil
}

func isMarkdownFile(path string) bool {
	return strings.EqualFold(filepath.Ext(path), ".md")
}

// NormalizeRelativePath cleans and validates a user-provided relative path.
func NormalizeRelativePath(path string) (string, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return "", errors.New("path is empty")
	}
	if filepath.IsAbs(trimmed) {
		return "", errors.New("path must be relative")
	}

	clean := filepath.Clean(trimmed)
	if clean == "." {
		return "", errors.New("path points to root")
	}
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", errPathOutsideRoot
	}

	return filepath.ToSlash(clean), nil
}

// SafeRelativePath returns a normalized relative path from root to target,
// rejecting targets outside root.
func SafeRelativePath(root string, target string) (string, error) {
	rootAbs, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return "", err
	}
	targetAbs, err := filepath.Abs(filepath.Clean(target))
	if err != nil {
		return "", err
	}

	rel, err := filepath.Rel(rootAbs, targetAbs)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errPathOutsideRoot
	}
	return NormalizeRelativePath(rel)
}

func resolveFromRoot(rootAbs string, rawPath string) (string, error) {
	trimmed := strings.TrimSpace(rawPath)
	if trimmed == "" {
		return "", errors.New("path is empty")
	}
	if filepath.IsAbs(trimmed) {
		return filepath.Abs(filepath.Clean(trimmed))
	}
	return filepath.Abs(filepath.Clean(filepath.Join(rootAbs, trimmed)))
}

// ChunkText deterministically chunks text using an approximate token-to-char
// conversion with overlap support.
func ChunkText(text string, tokens int, overlap int) []Chunk {
	if tokens <= 0 {
		return nil
	}

	runes := []rune(text)
	if len(runes) == 0 {
		return nil
	}

	chunkSize := tokens * approxCharsPerToken
	if chunkSize <= 0 {
		return nil
	}

	overlapSize := overlap * approxCharsPerToken
	if overlapSize < 0 {
		overlapSize = 0
	}
	if overlapSize >= chunkSize {
		overlapSize = chunkSize - 1
	}

	chunks := make([]Chunk, 0)
	for start := 0; start < len(runes); {
		end := start + chunkSize
		if end > len(runes) {
			end = len(runes)
		}

		chunkText := string(runes[start:end])
		if strings.TrimSpace(chunkText) != "" {
			chunks = append(chunks, Chunk{Text: chunkText, Hash: HashText(chunkText)})
		}

		if end == len(runes) {
			break
		}

		next := end - overlapSize
		if next <= start {
			next = start + chunkSize
		}
		start = next
	}
	return chunks
}

// HashText returns the deterministic SHA-256 hash hex digest for text.
func HashText(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}
