package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

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
	memoryBackend := mcpTools.RuntimeMemoryBackend{App: app}
	searchTool := mcpTools.NewMemorySearchTool(memoryBackend)
	getTool := mcpTools.NewMemoryGetTool(memoryBackend)

	workspaceBackend := mcpTools.RuntimeWorkspaceBackend{App: app}
	workspaceStatusTool := mcpTools.NewWorkspaceStatusTool(workspaceBackend)
	workspaceBootstrapContextTool := mcpTools.NewWorkspaceBootstrapContextTool(workspaceBackend)

	cronBackend := mcpTools.RuntimeCronBackend{App: app}
	cronListTool := mcpTools.NewCronListTool(cronBackend)
	cronAddTool := mcpTools.NewCronAddTool(cronBackend)
	cronRemoveTool := mcpTools.NewCronRemoveTool(cronBackend)
	cronRunTool := mcpTools.NewCronRunTool(cronBackend)

	orchestrationBackend := mcpTools.RuntimeOrchestrationBackend{App: app}
	sessionsListTool := mcpTools.NewSessionsListTool(orchestrationBackend)
	sessionsHistoryTool := mcpTools.NewSessionsHistoryTool(orchestrationBackend)
	sessionsSendTool := mcpTools.NewSessionsSendTool(orchestrationBackend)
	sessionStatusTool := mcpTools.NewSessionStatusTool(orchestrationBackend)

	tools := []mcp.ToolRegistration{
		{
			Definition: mcpTools.MemorySearchDefinition(),
			Handler:    withRuntimePolicy(app, mcpTools.RuntimeToolMemorySearch, searchTool.Call),
		},
		{
			Definition: mcpTools.MemorySearchAliasDefinition(),
			Handler:    withRuntimePolicy(app, mcpTools.RuntimeToolMemorySearch, searchTool.Call),
		},
		{
			Definition: mcpTools.MemoryGetDefinition(),
			Handler:    withRuntimePolicy(app, mcpTools.RuntimeToolMemoryGet, getTool.Call),
		},
		{
			Definition: mcpTools.MemoryGetAliasDefinition(),
			Handler:    withRuntimePolicy(app, mcpTools.RuntimeToolMemoryGet, getTool.Call),
		},
		{
			Definition: mcpTools.WorkspaceStatusDefinition(),
			Handler:    withRuntimePolicy(app, mcpTools.RuntimeToolWorkspaceStatus, workspaceStatusTool.Call),
		},
		{
			Definition: mcpTools.WorkspaceBootstrapContextDefinition(),
			Handler:    withRuntimePolicy(app, mcpTools.RuntimeToolWorkspaceBootstrapContext, workspaceBootstrapContextTool.Call),
		},
		{
			Definition: mcpTools.CronListDefinition(),
			Handler:    withRuntimePolicy(app, mcpTools.RuntimeToolCronList, cronListTool.Call),
		},
		{
			Definition: mcpTools.CronAddDefinition(),
			Handler:    withRuntimePolicy(app, mcpTools.RuntimeToolCronAdd, cronAddTool.Call),
		},
		{
			Definition: mcpTools.CronRemoveDefinition(),
			Handler:    withRuntimePolicy(app, mcpTools.RuntimeToolCronRemove, cronRemoveTool.Call),
		},
		{
			Definition: mcpTools.CronRunDefinition(),
			Handler:    withRuntimePolicy(app, mcpTools.RuntimeToolCronRun, cronRunTool.Call),
		},
		{
			Definition: mcpTools.SessionsListDefinition(),
			Handler:    withRuntimePolicy(app, mcpTools.RuntimeToolSessionsList, sessionsListTool.Call),
		},
		{
			Definition: mcpTools.SessionsHistoryDefinition(),
			Handler:    withRuntimePolicy(app, mcpTools.RuntimeToolSessionsHistory, sessionsHistoryTool.Call),
		},
		{
			Definition: mcpTools.SessionsSendDefinition(),
			Handler:    withRuntimePolicy(app, mcpTools.RuntimeToolSessionsSend, sessionsSendTool.Call),
		},
		{
			Definition: mcpTools.SessionStatusDefinition(),
			Handler:    withRuntimePolicy(app, mcpTools.RuntimeToolSessionStatus, sessionStatusTool.Call),
		},
	}
	return mcp.NewServer(mcp.Settings{ServerName: "localclaw", ServerVersion: "phase4", Tools: tools}), nil
}

func withRuntimePolicy(app *runtime.App, runtimeToolName string, next func(context.Context, map[string]interface{}) protocol.CallToolResult) mcp.ToolHandler {
	return func(ctx context.Context, args map[string]interface{}) protocol.CallToolResult {
		agentID := runtime.ResolveAgentID(stringArg(args, "agent_id"))
		policy := app.MCPToolsConfig(agentID)
		gate, err := mcpTools.NewPolicy(policy.Allow, policy.Deny)
		if err != nil {
			return protocol.CallToolResult{IsError: true, StructuredContent: map[string]interface{}{"ok": false, "error": err.Error()}}
		}
		allowed, reason := gate.Allowed(runtimeToolName)
		if !allowed {
			return protocol.CallToolResult{IsError: true, StructuredContent: map[string]interface{}{"ok": false, "error": reason}}
		}
		return next(ctx, args)
	}
}

func stringArg(args map[string]interface{}, key string) string {
	raw, ok := args[key]
	if !ok {
		return ""
	}
	text, ok := raw.(string)
	if !ok {
		return ""
	}
	return text
}
