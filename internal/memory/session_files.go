package memory

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dgriffin831/localclaw/internal/session"
)

// DiscoverSessionFiles scans transcript JSONL files from sessionsRoot.
func DiscoverSessionFiles(sessionsRoot string) ([]MemoryFile, error) {
	root := strings.TrimSpace(sessionsRoot)
	if root == "" {
		return []MemoryFile{}, nil
	}
	rootAbs, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return nil, err
	}

	info, err := os.Lstat(rootAbs)
	if err != nil {
		if os.IsNotExist(err) {
			return []MemoryFile{}, nil
		}
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return []MemoryFile{}, nil
	}

	seen := map[string]MemoryFile{}
	err = filepath.WalkDir(rootAbs, func(path string, d fs.DirEntry, walkErr error) error {
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
		if !strings.EqualFold(filepath.Ext(path), ".jsonl") {
			return nil
		}
		if hasLink, err := pathHasSymlinkComponentWithin(rootAbs, path); err != nil {
			return err
		} else if hasLink {
			return nil
		}

		absPath, err := filepath.Abs(filepath.Clean(path))
		if err != nil {
			return err
		}
		rel, err := SafeRelativePath(rootAbs, absPath)
		if err != nil {
			rel = ""
		}
		seen[absPath] = MemoryFile{AbsolutePath: absPath, RelativePath: rel}
		return nil
	})
	if err != nil {
		return nil, err
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

func ReadSessionTranscriptNormalized(path string) (string, error) {
	return session.ReadNormalizedTranscript(path)
}
