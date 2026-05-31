package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/dgriffin831/localclaw/internal/config"
	"github.com/dgriffin831/localclaw/internal/mcp"
	"github.com/dgriffin831/localclaw/internal/mcp/protocol"
	mcpTools "github.com/dgriffin831/localclaw/internal/mcp/tools"
	"github.com/dgriffin831/localclaw/internal/runtime"
)

var errMissingMCPSubcommand = errors.New("mcp subcommand is required")

// RunMCPCommand executes localclaw mcp command modes.
func RunMCPCommand(ctx context.Context, cfg config.Config, app *runtime.App, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	_ = cfg
	if stdin == nil {
		stdin = os.Stdin
	}
	if stdout == nil {
		stdout = os.Stdout
	}
	if stderr == nil {
		stderr = os.Stderr
	}
	if len(args) == 0 {
		return errMissingMCPSubcommand
	}

	switch args[0] {
	case "serve":
		if len(args) != 1 {
			return fmt.Errorf("mcp serve does not accept positional arguments")
		}
		if app == nil {
			return fmt.Errorf("runtime app is required")
		}
		if err := app.Run(ctx); err != nil {
			return fmt.Errorf("runtime init: %w", err)
		}
		server, err := newMCPServer(app)
		if err != nil {
			return err
		}
		return server.Serve(ctx, stdin, stdout)
	default:
		return fmt.Errorf("unknown mcp subcommand %q (supported: serve)", args[0])
	}
}

func newMCPServer(app *runtime.App) (*mcp.Server, error) {
	policy, err := mcpTools.NewPolicy(nil, nil)
	if err != nil {
		return nil, fmt.Errorf("build mcp tool policy: %w", err)
	}
	return newMCPServerWithPolicy(app, policy)
}

func newMCPServerWithPolicy(app *runtime.App, policy mcpTools.Policy) (*mcp.Server, error) {
	memoryBackend := mcpTools.RuntimeMemoryBackend{App: app}
	createTool := mcpTools.NewMemoryCreateTool(memoryBackend)
	searchTool := mcpTools.NewMemorySearchTool(memoryBackend)
	getTool := mcpTools.NewMemoryGetTool(memoryBackend)
	grepTool := mcpTools.NewMemoryGrepTool(memoryBackend)

	workspaceBackend := mcpTools.RuntimeWorkspaceBackend{App: app}
	workspaceStatusTool := mcpTools.NewWorkspaceStatusTool(workspaceBackend)
	workspaceListTool := mcpTools.NewWorkspaceListTool(workspaceBackend)
	workspaceReadTool := mcpTools.NewWorkspaceReadTool(workspaceBackend)

	registrations := []mcp.ToolRegistration{
		{
			Definition: mcpTools.MemoryCreateDefinition(),
			Handler:    createTool.Call,
		},
		{
			Definition: mcpTools.MemorySearchDefinition(),
			Handler:    searchTool.Call,
		},
		{
			Definition: mcpTools.MemoryGetDefinition(),
			Handler:    getTool.Call,
		},
		{
			Definition: mcpTools.MemoryGrepDefinition(),
			Handler:    grepTool.Call,
		},
		{
			Definition: mcpTools.WorkspaceStatusDefinition(),
			Handler:    workspaceStatusTool.Call,
		},
		{
			Definition: mcpTools.WorkspaceListDefinition(),
			Handler:    workspaceListTool.Call,
		},
		{
			Definition: mcpTools.WorkspaceReadDefinition(),
			Handler:    workspaceReadTool.Call,
		},
	}
	tools := applyMCPToolPolicy(registrations, policy)
	return mcp.NewServer(mcp.Settings{ServerName: "localclaw", ServerVersion: "memory-workspace", Tools: tools}), nil
}

func applyMCPToolPolicy(registrations []mcp.ToolRegistration, policy mcpTools.Policy) []mcp.ToolRegistration {
	filtered := make([]mcp.ToolRegistration, 0, len(registrations))
	for _, registration := range registrations {
		name := strings.TrimSpace(registration.Definition.Name)
		if name == "" || registration.Handler == nil {
			continue
		}
		registration.Definition.Name = name
		if allowed, reason := policy.Allowed(name); !allowed {
			registration.Handler = deniedToolHandler(reason)
		}
		filtered = append(filtered, registration)
	}
	return filtered
}

func deniedToolHandler(reason string) mcp.ToolHandler {
	trimmedReason := strings.TrimSpace(reason)
	if trimmedReason == "" {
		trimmedReason = "tool denied by policy"
	}
	return func(ctx context.Context, args map[string]interface{}) protocol.CallToolResult {
		_ = ctx
		_ = args
		return protocol.CallToolResult{
			IsError: true,
			StructuredContent: map[string]interface{}{
				"ok":    false,
				"error": trimmedReason,
			},
		}
	}
}
