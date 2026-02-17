package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/dgriffin831/localclaw/internal/cli"
	"github.com/dgriffin831/localclaw/internal/config"
	"github.com/dgriffin831/localclaw/internal/runtime"
	"github.com/dgriffin831/localclaw/internal/tui"
)

var runTUIProgram = tui.Run
var startBackupLoops = cli.StartBackupLoops

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

type doctorRuntime interface {
	Run(ctx context.Context) error
	ResolveWorkspacePath(agentID string) (string, error)
	ResolveSessionsPath(agentID string) (string, error)
	Prompt(ctx context.Context, input string) (string, error)
}

type doctorOptions struct {
	Deep bool
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
	doctorOpts := doctorOptions{}
	if mode == "doctor" {
		opts, showDoctorHelp, err := parseDoctorArgs(modeArgs)
		if err != nil {
			fmt.Fprintf(stderr, "doctor error: %v\n\n%s", err, doctorHelpText())
			return 1
		}
		if showDoctorHelp {
			fmt.Fprint(stdout, doctorHelpText())
			return 0
		}
		doctorOpts = opts
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

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	switch mode {
	case "doctor":
		if err := runDoctor(ctx, app, stdout, doctorOpts); err != nil {
			fmt.Fprintf(stderr, "doctor error: %v\n", err)
			return 1
		}
	case "tui":
		if err := runTUI(ctx, app, cfg); err != nil {
			fmt.Fprintf(stderr, "tui error: %v\n", err)
			return 1
		}
	case "backup":
		if err := cli.RunBackupCommand(ctx, cfg, app, modeArgs, stdout, stderr); err != nil {
			fmt.Fprintf(stderr, "backup error: %v\n", err)
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
	case "channels":
		if err := cli.RunChannelsCommand(ctx, cfg, app, modeArgs, stdout, stderr); err != nil {
			fmt.Fprintf(stderr, "channels error: %v\n", err)
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

func runTUI(ctx context.Context, app *runtime.App, cfg config.Config) error {
	if err := app.Run(ctx); err != nil {
		return err
	}
	startBackupLoops(ctx, cfg, app)
	return runTUIProgram(ctx, app, cfg)
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
	case "doctor", "tui", "backup", "memory", "channels", "mcp":
		return true
	default:
		return false
	}
}

func parseDoctorArgs(args []string) (doctorOptions, bool, error) {
	doctorFS := flag.NewFlagSet("doctor", flag.ContinueOnError)
	doctorFS.SetOutput(io.Discard)
	deep := doctorFS.Bool("deep", false, "run deep checks")
	help := doctorFS.Bool("help", false, "display help for command")
	doctorFS.BoolVar(help, "h", false, "display help for command")

	if err := doctorFS.Parse(args); err != nil {
		return doctorOptions{}, false, err
	}
	if len(doctorFS.Args()) > 0 {
		return doctorOptions{}, false, fmt.Errorf("unexpected arguments %q", strings.Join(doctorFS.Args(), " "))
	}
	return doctorOptions{Deep: *deep}, *help, nil
}

func runDoctor(ctx context.Context, app doctorRuntime, stdout io.Writer, opts doctorOptions) error {
	start := time.Now()
	resolution := runtime.ResolveSession("", "")

	fmt.Fprintln(stdout, "localclaw doctor")
	fmt.Fprintln(stdout, "")
	fmt.Fprintln(stdout, "Checks:")
	if err := app.Run(ctx); err != nil {
		return fmt.Errorf("runtime startup: %w", err)
	}
	fmt.Fprintln(stdout, "  [ok] runtime startup")

	workspacePath, err := app.ResolveWorkspacePath(resolution.AgentID)
	if err != nil {
		return fmt.Errorf("resolve workspace path: %w", err)
	}
	if err := validateDoctorPath(workspacePath, true); err != nil {
		return fmt.Errorf("workspace path check: %w", err)
	}
	fmt.Fprintf(stdout, "  [ok] workspace path check (%s)\n", workspacePath)

	sessionsPath, err := app.ResolveSessionsPath(resolution.AgentID)
	if err != nil {
		return fmt.Errorf("resolve sessions path: %w", err)
	}
	if err := validateDoctorPath(sessionsPath, false); err != nil {
		return fmt.Errorf("sessions path check: %w", err)
	}
	fmt.Fprintf(stdout, "  [ok] sessions path check (%s)\n", sessionsPath)
	if opts.Deep {
		if err := runDoctorDeepLLMProbe(ctx, app); err != nil {
			return fmt.Errorf("deep llm probe: %w", err)
		}
		fmt.Fprintln(stdout, "  [ok] deep llm prompt check")
	}

	fmt.Fprintln(stdout, "")
	fmt.Fprintln(stdout, "Details:")
	fmt.Fprintf(stdout, "  Agent: %s\n", resolution.AgentID)
	fmt.Fprintf(stdout, "  Workspace path: %s\n", workspacePath)
	fmt.Fprintf(stdout, "  Sessions path: %s\n", sessionsPath)
	fmt.Fprintf(stdout, "  Runtime: %s\n", time.Since(start).Round(time.Millisecond))
	fmt.Fprintln(stdout, "")
	fmt.Fprintln(stdout, "Doctor complete.")
	return nil
}

func runDoctorDeepLLMProbe(ctx context.Context, app doctorRuntime) error {
	probeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	const prompt = "Reply with one short line that says hello from localclaw doctor."
	response, err := app.Prompt(probeCtx, prompt)
	if err != nil {
		return err
	}
	if strings.TrimSpace(response) == "" {
		return errors.New("llm returned empty response")
	}
	return nil
}

func validateDoctorPath(path string, mustBeDir bool) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if mustBeDir && !info.IsDir() {
		return fmt.Errorf("expected directory, got file %q", path)
	}
	if !mustBeDir && info.IsDir() {
		return fmt.Errorf("expected file, got directory %q", path)
	}
	if !mustBeDir {
		parent := filepath.Dir(path)
		parentInfo, err := os.Stat(parent)
		if err != nil {
			return fmt.Errorf("stat parent directory %q: %w", parent, err)
		}
		if !parentInfo.IsDir() {
			return fmt.Errorf("sessions parent is not a directory %q", parent)
		}
	}
	return nil
}

func rootHelpText() string {
	return `localclaw - local-only single-process CLI runtime

Usage: localclaw [options] [command]

Options:
  -config string   path to config JSON
  -h, --help       display help for command

Commands:
  doctor           Health checks + startup diagnostics
  tui              Run full-screen terminal UI
  backup           Create one compressed local backup archive
  memory           Memory search tools (status/index/search/grep)
  channels         Channel worker modes (currently signal inbound serve)
  mcp              MCP stdio server (serve subcommand)
  help             Display help for command

Examples:
  localclaw
  localclaw doctor
  localclaw tui
  localclaw backup
  localclaw memory status
  localclaw channels serve
  localclaw mcp serve

Docs: README.md, docs/RUNTIME.md
`
}

func doctorHelpText() string {
	return `Usage: localclaw doctor [options]

Run startup and path checks for the local runtime.
Use --deep to run provider-level LLM probe checks.

Options:
  --deep           run deep checks (includes LLM prompt probe)
  -h, --help       display help for command
`
}
