package runtime

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type stateFileLogger struct {
	path string
	mu   sync.Mutex
}

func newStateFileLogger(stateRoot, fileName string) func(format string, args ...interface{}) {
	root := strings.TrimSpace(stateRoot)
	name := strings.TrimSpace(fileName)
	if root == "" || name == "" {
		return func(format string, args ...interface{}) {}
	}
	logger := &stateFileLogger{
		path: filepath.Join(root, "logs", name),
	}
	return logger.Printf
}

func (l *stateFileLogger) Printf(format string, args ...interface{}) {
	if l == nil || strings.TrimSpace(l.path) == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(l.path), 0o755); err != nil {
		return
	}
	file, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer file.Close()

	log.New(file, "", log.LstdFlags).Printf(format, args...)
}
