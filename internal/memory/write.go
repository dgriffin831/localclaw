package memory

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var ErrEmptyWriteContent = errors.New("write content is empty")

// Write appends or overwrites markdown memory files and refreshes the index.
// Path scope is restricted to MEMORY.md / memory.md and memory/**/*.md.
func (m *SQLiteIndexManager) Write(ctx context.Context, content string, opts WriteOptions) (WriteResult, error) {
	if strings.TrimSpace(content) == "" {
		return WriteResult{}, ErrEmptyWriteContent
	}

	relativePath, absolutePath, err := m.resolveWritablePath(opts.Path)
	if err != nil {
		return WriteResult{}, err
	}

	bytesWritten, appended, err := writeContent(absolutePath, content, opts.Overwrite)
	if err != nil {
		return WriteResult{}, err
	}

	result := WriteResult{
		Path:         relativePath,
		BytesWritten: bytesWritten,
		Appended:     appended,
	}

	if _, err := m.Sync(ctx, false); err != nil {
		result.Indexed = false
		result.SyncError = err.Error()
		return result, nil
	}
	result.Indexed = true
	return result, nil
}

func (m *SQLiteIndexManager) resolveWritablePath(rawPath string) (string, string, error) {
	root := strings.TrimSpace(m.cfg.WorkspaceRoot)
	if root == "" {
		return "", "", errors.New("workspace root is empty")
	}
	rootAbs, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return "", "", err
	}

	relPath := strings.TrimSpace(rawPath)
	if relPath == "" {
		relPath = filepath.ToSlash(filepath.Join("memory", time.Now().UTC().Format("2006-01-02")+".md"))
	}
	normalizedRel, err := NormalizeRelativePath(relPath)
	if err != nil {
		if errors.Is(err, errPathOutsideRoot) {
			return "", "", ErrMemoryPathOutOfScope
		}
		return "", "", ErrMemoryPathOutOfScope
	}
	if !isMarkdownFile(normalizedRel) {
		return "", "", ErrMemoryPathNotMarkdown
	}
	if !memoryWritePathAllowed(normalizedRel) {
		return "", "", ErrMemoryPathOutOfScope
	}

	absolutePath := filepath.Join(rootAbs, filepath.FromSlash(normalizedRel))
	safeRel, err := SafeRelativePath(rootAbs, absolutePath)
	if err != nil {
		return "", "", ErrMemoryPathOutOfScope
	}
	if !memoryWritePathAllowed(safeRel) {
		return "", "", ErrMemoryPathOutOfScope
	}

	hasSymlink, err := pathHasSymlinkComponentWithin(rootAbs, absolutePath)
	if err != nil {
		return "", "", err
	}
	if hasSymlink {
		return "", "", ErrMemoryPathOutOfScope
	}

	return safeRel, absolutePath, nil
}

func memoryWritePathAllowed(rel string) bool {
	normalized := strings.ToLower(strings.TrimSpace(filepath.ToSlash(rel)))
	if normalized == "memory.md" {
		return true
	}
	if strings.HasPrefix(normalized, "memory/") {
		return true
	}
	return false
}

func writeContent(path string, content string, overwrite bool) (int, bool, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return 0, false, fmt.Errorf("mkdir memory path: %w", err)
	}

	body := content
	if !strings.HasSuffix(body, "\n") {
		body += "\n"
	}

	if overwrite {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			return 0, false, fmt.Errorf("write memory file: %w", err)
		}
		return len(body), false, nil
	}

	separator := ""
	info, err := os.Stat(path)
	switch {
	case err == nil && info.Size() > 0:
		endsWithNewline, err := fileEndsWithNewline(path)
		if err != nil {
			return 0, false, err
		}
		if !endsWithNewline {
			separator = "\n"
		}
	case err != nil && !errors.Is(err, os.ErrNotExist):
		return 0, false, fmt.Errorf("stat memory file: %w", err)
	}

	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return 0, false, fmt.Errorf("open memory file: %w", err)
	}
	defer file.Close()

	written, err := file.WriteString(separator + body)
	if err != nil {
		return 0, false, fmt.Errorf("append memory file: %w", err)
	}
	return written, true, nil
}

func fileEndsWithNewline(path string) (bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return false, fmt.Errorf("open memory file for newline check: %w", err)
	}
	defer file.Close()

	if _, err := file.Seek(-1, io.SeekEnd); err != nil {
		return false, fmt.Errorf("seek memory file for newline check: %w", err)
	}
	buf := []byte{0}
	if _, err := file.Read(buf); err != nil {
		return false, fmt.Errorf("read memory file for newline check: %w", err)
	}
	return buf[0] == '\n', nil
}
