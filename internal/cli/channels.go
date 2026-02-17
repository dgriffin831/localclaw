package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/dgriffin831/localclaw/internal/config"
	"github.com/dgriffin831/localclaw/internal/runtime"
)

var errMissingChannelsSubcommand = errors.New("channels subcommand is required")

// RunChannelsCommand executes localclaw channels command modes.
func RunChannelsCommand(ctx context.Context, cfg config.Config, app *runtime.App, args []string, stdout, stderr io.Writer) error {
	if stdout == nil {
		stdout = os.Stdout
	}
	if stderr == nil {
		stderr = os.Stderr
	}
	if len(args) == 0 {
		return errMissingChannelsSubcommand
	}

	switch args[0] {
	case "serve":
		return runChannelsServe(ctx, cfg, app, args[1:], stdout, stderr)
	default:
		return fmt.Errorf("unknown channels subcommand %q (supported: serve)", args[0])
	}
}

func runChannelsServe(ctx context.Context, cfg config.Config, app *runtime.App, args []string, stdout, stderr io.Writer) error {
	if app == nil {
		return errors.New("runtime app is required")
	}
	fs := flag.NewFlagSet("channels serve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	once := fs.Bool("once", false, "process a single signal receive poll and exit")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(fs.Args()) > 0 {
		return fmt.Errorf("channels serve does not accept positional arguments")
	}

	if err := app.Run(ctx); err != nil {
		return fmt.Errorf("runtime init: %w", err)
	}
	if !*once {
		startBackgroundBackupLoops(ctx, cfg, app)
	}
	fmt.Fprintln(stdout, "channels serve: signal inbound loop started")
	if *once {
		fmt.Fprintln(stdout, "channels serve: once mode enabled")
	}
	return app.RunSignalInbound(ctx, runtime.SignalInboundRunOptions{
		Once: *once,
		Logf: func(format string, args ...interface{}) {
			fmt.Fprintf(stderr, format+"\n", args...)
		},
	})
}
