package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/dgriffin831/localclaw/internal/config"
	"github.com/dgriffin831/localclaw/internal/runtime"
	"github.com/dgriffin831/localclaw/internal/tui"
)

func main() {
	fs := flag.NewFlagSet("localclaw", flag.ExitOnError)
	configPath := fs.String("config", "", "path to config JSON")
	fs.Parse(os.Args[1:])

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}

	app, err := runtime.New(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "startup error: %v\n", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	mode := "check"
	if args := fs.Args(); len(args) > 0 {
		mode = args[0]
	}

	switch mode {
	case "check":
		if err := app.Run(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "runtime error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("localclaw startup checks passed")
	case "tui":
		if err := runTUI(ctx, app, cfg); err != nil {
			fmt.Fprintf(os.Stderr, "tui error: %v\n", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q (supported: check, tui)\n", mode)
		os.Exit(1)
	}
}

func runTUI(ctx context.Context, app *runtime.App, cfg config.Config) error {
	if err := app.Run(ctx); err != nil {
		return err
	}
	return tui.Run(ctx, app, cfg)
}
