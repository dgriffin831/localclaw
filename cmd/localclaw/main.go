package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/dgriffin831/localclaw/internal/cli"
	"github.com/dgriffin831/localclaw/internal/config"
	"github.com/dgriffin831/localclaw/internal/runtime"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

type doctorRuntime interface {
	Run(ctx context.Context) error
	ResolveWorkspacePath(agentID string) (string, error)
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("localclaw", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	configPath := fs.String("config", "", "path to config JSON")
	showHelp := fs.Bool("help", false, "display help for command")
	fs.BoolVar(showHelp, "h", false, "display help for command")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(stderr, "flag error: %v\n\n%s", err, rootHelpText())
		return 2
	}

	mode, modeArgs := resolveCommand(fs.Args())
	if *showHelp || mode == "help" {
		fmt.Fprint(stdout, rootHelpText())
		return 0
	}
	if !isKnownCommand(mode) {
		fmt.Fprintf(stderr, "unknown command %q\n\n%s", mode, rootHelpText())
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if mode == "setup" {
		if err := cli.RunSetupCommand(ctx, modeArgs, stdout, stderr); err != nil {
			fmt.Fprintf(stderr, "setup error: %v\n", err)
			return 1
		}
		return 0
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(stderr, "config error: %v\n", err)
		return 1
	}

	app, err := runtime.New(cfg)
	if err != nil {
		fmt.Fprintf(stderr, "startup error: %v\n", err)
		return 1
	}

	switch mode {
	case "doctor":
		if err := runDoctor(ctx, app, stdout, modeArgs); err != nil {
			fmt.Fprintf(stderr, "doctor error: %v\n", err)
			return 1
		}
	case "memory":
		if err := app.Run(ctx); err != nil {
			fmt.Fprintf(stderr, "runtime error: %v\n", err)
			return 1
		}
		if err := cli.RunMemoryCommand(ctx, cfg, app, modeArgs, stdout, stderr); err != nil {
			fmt.Fprintf(stderr, "memory error: %v\n", err)
			return 1
		}
	case "mcp":
		if err := cli.RunMCPCommand(ctx, cfg, app, modeArgs, os.Stdin, stdout, stderr); err != nil {
			fmt.Fprintf(stderr, "mcp error: %v\n", err)
			return 1
		}
	}
	return 0
}

func resolveCommand(args []string) (string, []string) {
	if len(args) == 0 {
		return "help", nil
	}
	mode := strings.TrimSpace(args[0])
	if mode == "" || mode == "help" {
		return "help", args[1:]
	}
	return mode, args[1:]
}

func isKnownCommand(mode string) bool {
	switch mode {
	case "doctor", "memory", "mcp", "setup":
		return true
	default:
		return false
	}
}

func runDoctor(ctx context.Context, app doctorRuntime, stdout io.Writer, args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	showHelp := fs.Bool("help", false, "display help for command")
	fs.BoolVar(showHelp, "h", false, "display help for command")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *showHelp {
		fmt.Fprint(stdout, doctorHelpText())
		return nil
	}
	if len(fs.Args()) != 0 {
		return fmt.Errorf("unexpected arguments %q", strings.Join(fs.Args(), " "))
	}

	start := time.Now()
	fmt.Fprintln(stdout, "localclaw doctor")
	fmt.Fprintln(stdout, "")
	fmt.Fprintln(stdout, "Checks:")
	if err := app.Run(ctx); err != nil {
		return fmt.Errorf("runtime startup: %w", err)
	}
	fmt.Fprintln(stdout, "  [ok] runtime startup")

	workspacePath, err := app.ResolveWorkspacePath(runtime.DefaultAgentID)
	if err != nil {
		return fmt.Errorf("resolve workspace path: %w", err)
	}
	if err := validateDoctorWorkspace(workspacePath); err != nil {
		return fmt.Errorf("workspace path check: %w", err)
	}
	fmt.Fprintf(stdout, "  [ok] workspace path check (%s)\n", workspacePath)

	fmt.Fprintln(stdout, "")
	fmt.Fprintln(stdout, "Details:")
	fmt.Fprintf(stdout, "  Agent: %s\n", runtime.DefaultAgentID)
	fmt.Fprintf(stdout, "  Workspace path: %s\n", workspacePath)
	fmt.Fprintf(stdout, "  Runtime: %s\n", time.Since(start).Round(time.Millisecond))
	fmt.Fprintln(stdout, "")
	fmt.Fprintln(stdout, "Doctor complete.")
	return nil
}

func validateDoctorWorkspace(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("expected directory, got file %q", path)
	}
	return nil
}

func rootHelpText() string {
	return `localclaw - local MCP memory/workspace server

Usage: localclaw [options] [command]

Options:
  -config string   path to config JSON
  -h, --help       display help for command

Commands:
  doctor           Health checks + workspace diagnostics
  memory           Memory tools (status/index/search/grep/model)
  mcp              MCP stdio server (serve subcommand)
  setup            Ask claude, codex, or opencode to configure LocalClaw MCP
  help             Display help for command

Examples:
  localclaw doctor
  localclaw memory status
  localclaw memory model status
  localclaw memory search --mode vector "incident summary"
  localclaw mcp serve
  localclaw setup claude
  localclaw setup codex --dry-run
  localclaw setup opencode

Docs: README.md, docs/RUNTIME.md
`
}

func doctorHelpText() string {
	return `Usage: localclaw doctor

Run startup and workspace path checks for the local MCP server.
`
}
