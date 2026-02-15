package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dgriffin831/localclaw/internal/config"
)

func TestParseSlash(t *testing.T) {
	name, arg := parseSlash("/thinking on")
	if name != "thinking" || arg != "on" {
		t.Fatalf("unexpected parse result: name=%q arg=%q", name, arg)
	}

	name, arg = parseSlash("/status")
	if name != "status" || arg != "" {
		t.Fatalf("unexpected parse result for no-arg command: name=%q arg=%q", name, arg)
	}
}

func TestFormatElapsed(t *testing.T) {
	if got := formatElapsed(500 * time.Millisecond); got != "0s" {
		t.Fatalf("expected 0s, got %q", got)
	}
	if got := formatElapsed(15 * time.Second); got != "15s" {
		t.Fatalf("expected 15s, got %q", got)
	}
	if got := formatElapsed(125 * time.Second); got != "2m5s" {
		t.Fatalf("expected 2m5s, got %q", got)
	}
}

func TestInputAcceptsTyping(t *testing.T) {
	m := newModel(context.Background(), nil, config.Default())
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")})
	next := updated.(model)

	if got := next.input.Value(); got != "h" {
		t.Fatalf("expected typed value to be captured, got %q", got)
	}
}

func TestViewDoesNotOverflowHeight(t *testing.T) {
	cfg := config.Default()
	cfg.Workspace.Root = "/Users/dennis/Documents/GitHub/localclaw/very/long/path/that/used/to/wrap"

	m := newModel(context.Background(), nil, cfg)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	next := updated.(model)

	view := next.View()
	lines := strings.Count(view, "\n") + 1
	if lines > 24 {
		t.Fatalf("expected view lines to fit terminal height, got %d lines for height 24", lines)
	}
}
