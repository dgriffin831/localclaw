package runtime

import (
	"testing"

	"github.com/dgriffin831/localclaw/internal/config"
)

func TestNewFailsWhenNetworkServerEnabled(t *testing.T) {
	cfg := config.Default()
	cfg.Security.EnableHTTPServer = true

	if _, err := New(cfg); err == nil {
		t.Fatalf("expected startup failure when HTTP server is enabled")
	}
}
