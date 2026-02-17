package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml"

	"github.com/dgriffin831/localclaw/internal/llm"
)

type Settings struct {
	BinaryPath       string
	Profile          string
	Model            string
	ExtraArgs        []string
	SessionMode      string
	SessionArg       string
	ResumeArgs       []string
	SessionIDFields  []string
	ResumeOutput     string
	WorkingDirectory string
	MCP              MCPSettings
}

type MCPSettings struct {
	ConfigPath       string
	ServerName       string
	ServerBinaryPath string
	ServerArgs       []string
}

type LocalClient struct {
	settings Settings
}

func NewClient(settings Settings) *LocalClient {
	if strings.TrimSpace(settings.BinaryPath) == "" {
		settings.BinaryPath = "codex"
	}
	if strings.TrimSpace(settings.WorkingDirectory) == "" {
		settings.WorkingDirectory = "."
	}
	if strings.TrimSpace(settings.MCP.ServerName) == "" {
		settings.MCP.ServerName = "localclaw"
	}
	if strings.TrimSpace(settings.MCP.ServerBinaryPath) == "" {
		settings.MCP.ServerBinaryPath = "localclaw"
	}
	if len(settings.MCP.ServerArgs) == 0 {
		settings.MCP.ServerArgs = []string{"mcp", "serve"}
	}
	if strings.TrimSpace(settings.SessionMode) == "" {
		settings.SessionMode = "existing"
	}
	if len(settings.ResumeArgs) == 0 {
		settings.ResumeArgs = []string{"resume", "{sessionId}"}
	}
	if len(settings.SessionIDFields) == 0 {
		settings.SessionIDFields = []string{"thread_id", "threadId", "session_id", "sessionId"}
	}
	if strings.TrimSpace(settings.ResumeOutput) == "" {
		settings.ResumeOutput = "text"
	}
	return &LocalClient{settings: settings}
}

func (c *LocalClient) Capabilities() llm.Capabilities {
	return llm.Capabilities{
		SupportsRequestOptions: true,
		StructuredToolCalls:    false,
	}
}

func (c *LocalClient) ValidateMCPWiring() error {
	configPath, _, err := c.resolveEffectiveMCPConfigPath()
	if err != nil {
		return err
	}
	if err := c.ensureMCPServerConfig(configPath); err != nil {
		return err
	}
	return nil
}

func (c *LocalClient) Prompt(ctx context.Context, input string) (string, error) {
	return c.PromptRequest(ctx, llm.Request{Input: input})
}

func (c *LocalClient) PromptRequest(ctx context.Context, req llm.Request) (string, error) {
	events, errs := c.PromptStreamRequest(ctx, req)
	var streamed strings.Builder
	final := ""

	for events != nil || errs != nil {
		select {
		case evt, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			if evt.Type == llm.StreamEventDelta {
				streamed.WriteString(evt.Text)
				continue
			}
			if evt.Type == llm.StreamEventFinal {
				final = strings.TrimSpace(evt.Text)
			}
		case err, ok := <-errs:
			if !ok {
				errs = nil
				continue
			}
			if err != nil {
				return "", err
			}
		}
	}

	if final != "" {
		return final, nil
	}
	return strings.TrimSpace(streamed.String()), nil
}

func (c *LocalClient) PromptStream(ctx context.Context, input string) (<-chan llm.StreamEvent, <-chan error) {
	return c.PromptStreamRequest(ctx, llm.Request{Input: input})
}

func (c *LocalClient) PromptStreamRequest(ctx context.Context, req llm.Request) (<-chan llm.StreamEvent, <-chan error) {
	// NOTE: ComposePromptFallback remains the active request composition path for Codex for now.
	prompt := strings.TrimSpace(llm.ComposePromptFallback(req))
	if prompt == "" {
		events := make(chan llm.StreamEvent)
		errs := make(chan error, 1)
		defer close(events)
		defer close(errs)
		errs <- fmt.Errorf("input is required")
		return events, errs
	}

	configPath, extraEnv, err := c.resolveEffectiveMCPConfigPath()
	if err != nil {
		events := make(chan llm.StreamEvent)
		errs := make(chan error, 1)
		defer close(events)
		defer close(errs)
		errs <- fmt.Errorf("resolve codex mcp config path: %w", err)
		return events, errs
	}
	if err := c.ensureMCPServerConfig(configPath); err != nil {
		events := make(chan llm.StreamEvent)
		errs := make(chan error, 1)
		defer close(events)
		defer close(errs)
		errs <- fmt.Errorf("prepare codex mcp config: %w", err)
		return events, errs
	}

	return c.promptStreamWithArgs(ctx, c.buildCommandArgsForRequest(req), extraEnv, prompt)
}

func (c *LocalClient) buildCommandArgsForRequest(req llm.Request) []string {
	mode := strings.ToLower(strings.TrimSpace(c.settings.SessionMode))
	if mode == "" {
		mode = "existing"
	}
	providerSessionID := strings.TrimSpace(req.Session.ProviderSessionID)
	resume := mode != "none" && providerSessionID != ""

	args := []string{"exec"}
	if resume {
		args = append(args, expandSessionArgs(c.settings.ResumeArgs, providerSessionID, []string{"resume", "{sessionId}"})...)
	}
	if !resume || c.resumeOutputMode() == "json" || c.resumeOutputMode() == "jsonl" {
		args = append(args, "--json")
	}
	if !resume {
		args = append(args, "-C", c.settings.WorkingDirectory)
		if profile := strings.TrimSpace(c.settings.Profile); profile != "" {
			args = append(args, "-p", profile)
		}
	}
	if model := c.resolveModelForRequest(req); model != "" {
		args = append(args, "-m", model)
	}
	args = append(args, normalizeNonBlankArgs(c.settings.ExtraArgs)...)
	args = append(args, "-")
	return args
}

func (c *LocalClient) resolveModelForRequest(req llm.Request) string {
	if override := strings.TrimSpace(req.Options.ModelOverride); override != "" {
		return override
	}
	return strings.TrimSpace(c.settings.Model)
}

func (c *LocalClient) resolveEffectiveMCPConfigPath() (string, map[string]string, error) {
	extraEnv := map[string]string{}
	if configured := strings.TrimSpace(c.settings.MCP.ConfigPath); configured != "" {
		resolved, err := normalizePath(configured)
		if err != nil {
			return "", nil, fmt.Errorf("resolve mcp config path: %w", err)
		}
		return resolved, extraEnv, nil
	}
	if codexHome := strings.TrimSpace(os.Getenv("CODEX_HOME")); codexHome != "" {
		resolvedHome, err := normalizePath(codexHome)
		if err != nil {
			return "", nil, fmt.Errorf("resolve CODEX_HOME: %w", err)
		}
		return filepath.Join(resolvedHome, "config.toml"), extraEnv, nil
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		return "", nil, fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(userHome, ".codex", "config.toml"), extraEnv, nil
}

func normalizePath(path string) (string, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return "", fmt.Errorf("path is required")
	}
	if strings.HasPrefix(trimmed, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home dir: %w", err)
		}
		trimmed = filepath.Join(home, strings.TrimPrefix(trimmed, "~"))
	}
	if filepath.IsAbs(trimmed) {
		return filepath.Clean(trimmed), nil
	}
	resolved, err := filepath.Abs(trimmed)
	if err != nil {
		return "", fmt.Errorf("resolve absolute path: %w", err)
	}
	return filepath.Clean(resolved), nil
}

func (c *LocalClient) ensureMCPServerConfig(configPath string) error {
	if strings.TrimSpace(configPath) == "" {
		return fmt.Errorf("mcp config path is required")
	}
	if strings.TrimSpace(c.settings.MCP.ServerName) == "" {
		return fmt.Errorf("mcp server name is required")
	}
	if strings.TrimSpace(c.settings.MCP.ServerBinaryPath) == "" {
		return fmt.Errorf("mcp server binary path is required")
	}
	if len(c.settings.MCP.ServerArgs) == 0 {
		return fmt.Errorf("mcp server args are required")
	}

	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		return fmt.Errorf("create codex config directory: %w", err)
	}

	var tree *toml.Tree
	buf, err := os.ReadFile(configPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("read codex config: %w", err)
		}
		tree, err = toml.TreeFromMap(map[string]interface{}{})
		if err != nil {
			return fmt.Errorf("initialize codex config: %w", err)
		}
	} else {
		tree, err = toml.LoadBytes(buf)
		if err != nil {
			return fmt.Errorf("parse codex config toml: %w", err)
		}
	}

	keyBase := fmt.Sprintf("mcp_servers.%s", c.settings.MCP.ServerName)
	tree.Set(keyBase+".command", c.settings.MCP.ServerBinaryPath)
	args := make([]interface{}, 0, len(c.settings.MCP.ServerArgs))
	for _, arg := range c.settings.MCP.ServerArgs {
		args = append(args, arg)
	}
	tree.Set(keyBase+".args", args)

	payload := []byte(tree.String())
	if len(payload) == 0 || payload[len(payload)-1] != '\n' {
		payload = append(payload, '\n')
	}
	if err := os.WriteFile(configPath, payload, 0o600); err != nil {
		return fmt.Errorf("write codex config: %w", err)
	}
	return nil
}

func (c *LocalClient) promptStreamWithArgs(ctx context.Context, args []string, extraEnv map[string]string, prompt string) (<-chan llm.StreamEvent, <-chan error) {
	cmd := exec.CommandContext(ctx, c.settings.BinaryPath, args...)
	cmd.Env = append(os.Environ(), buildEnv(extraEnv)...)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		events := make(chan llm.StreamEvent)
		errs := make(chan error, 1)
		defer close(events)
		defer close(errs)
		errs <- fmt.Errorf("create stdin pipe: %w", err)
		return events, errs
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		events := make(chan llm.StreamEvent)
		errs := make(chan error, 1)
		defer close(events)
		defer close(errs)
		errs <- fmt.Errorf("create stdout pipe: %w", err)
		return events, errs
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		events := make(chan llm.StreamEvent)
		errs := make(chan error, 1)
		defer close(events)
		defer close(errs)
		errs <- fmt.Errorf("create stderr pipe: %w", err)
		return events, errs
	}
	if err := cmd.Start(); err != nil {
		events := make(chan llm.StreamEvent)
		errs := make(chan error, 1)
		defer close(events)
		defer close(errs)
		errs <- fmt.Errorf("start codex cli: %w", err)
		return events, errs
	}
	if _, err := io.WriteString(stdin, prompt); err != nil {
		_ = stdin.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		events := make(chan llm.StreamEvent)
		errs := make(chan error, 1)
		defer close(events)
		defer close(errs)
		errs <- fmt.Errorf("write codex stdin prompt: %w", err)
		return events, errs
	}
	if err := stdin.Close(); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		events := make(chan llm.StreamEvent)
		errs := make(chan error, 1)
		defer close(events)
		defer close(errs)
		errs <- fmt.Errorf("close codex stdin prompt: %w", err)
		return events, errs
	}

	events := make(chan llm.StreamEvent, 32)
	errs := make(chan error, 1)

	go func() {
		defer close(events)
		defer close(errs)

		stderrTextCh := make(chan string, 1)
		go func() {
			data, _ := io.ReadAll(stderr)
			stderrTextCh <- strings.TrimSpace(string(data))
		}()

		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
		var deltaText strings.Builder
		finalSeen := false

		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			parsedEvents, parseErr := parseStreamJSONLine(line, c.settings.SessionIDFields)
			if parseErr != nil {
				// NOTE: Compatibility fallback is intentionally retained for now; unparseable provider lines are streamed as raw text deltas.
				delta := line + "\n"
				deltaText.WriteString(delta)
				if !emitEvent(ctx, events, llm.StreamEvent{Type: llm.StreamEventDelta, Text: delta}) {
					_ = cmd.Process.Kill()
					_ = cmd.Wait()
					return
				}
				continue
			}

			for _, evt := range parsedEvents {
				if evt.Type == llm.StreamEventDelta {
					deltaText.WriteString(evt.Text)
				}
				if evt.Type == llm.StreamEventFinal {
					finalSeen = true
				}
				if !emitEvent(ctx, events, evt) {
					_ = cmd.Process.Kill()
					_ = cmd.Wait()
					return
				}
			}
		}
		if scanErr := scanner.Err(); scanErr != nil && scanErr != io.EOF {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			emitError(ctx, errs, fmt.Errorf("read codex output: %w", scanErr))
			return
		}

		waitErr := cmd.Wait()
		stderrText := <-stderrTextCh
		if waitErr != nil {
			if stderrText != "" {
				emitError(ctx, errs, fmt.Errorf("codex cli execution failed: %w: %s", waitErr, stderrText))
				return
			}
			emitError(ctx, errs, fmt.Errorf("codex cli execution failed: %w", waitErr))
			return
		}

		if !finalSeen {
			finalText := strings.TrimSpace(deltaText.String())
			if finalText != "" {
				emitEvent(ctx, events, llm.StreamEvent{Type: llm.StreamEventFinal, Text: finalText})
			}
		}
	}()

	return events, errs
}

func parseStreamJSONLine(line string, sessionIDFields []string) ([]llm.StreamEvent, error) {
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(line), &payload); err != nil {
		return nil, err
	}

	events := make([]llm.StreamEvent, 0, 1)
	lineType, _ := payload["type"].(string)
	sessionID := extractProviderSessionID(payload, sessionIDFields)
	if sessionID != "" && lineType != "session.configured" {
		events = append(events, llm.StreamEvent{
			Type: llm.StreamEventProviderMetadata,
			ProviderMetadata: &llm.ProviderMetadata{
				Provider:  "codex",
				SessionID: sessionID,
			},
		})
	}

	if lineType == "item.completed" {
		item, _ := payload["item"].(map[string]interface{})
		if itemType, _ := item["type"].(string); itemType == "agent_message" {
			if text := extractAgentMessageText(item); text != "" {
				events = append(events, llm.StreamEvent{Type: llm.StreamEventDelta, Text: text})
			}
		}
	}

	if lineType == "item.started" {
		if evt := parseCommandExecutionStarted(payload); evt != nil {
			events = append(events, *evt)
		} else if evt := parseDelegatedToolStarted(payload); evt != nil {
			events = append(events, *evt)
		}
	}

	if lineType == "item.completed" {
		if evt := parseCommandExecutionCompleted(payload); evt != nil {
			events = append(events, *evt)
		} else if evt := parseDelegatedToolCompleted(payload); evt != nil {
			events = append(events, *evt)
		}
	}

	if lineType == "session.configured" {
		meta := llm.ProviderMetadata{Provider: "codex"}
		if model, _ := payload["model"].(string); model != "" {
			meta.Model = model
		}
		if toolsRaw, ok := payload["tools"].([]interface{}); ok {
			meta.Tools = make([]string, 0, len(toolsRaw))
			for _, value := range toolsRaw {
				if tool, ok := value.(string); ok && strings.TrimSpace(tool) != "" {
					meta.Tools = append(meta.Tools, tool)
				}
			}
		}
		meta.SessionID = sessionID
		events = append(events, llm.StreamEvent{Type: llm.StreamEventProviderMetadata, ProviderMetadata: &meta})
	}

	return events, nil
}

func extractProviderSessionID(payload map[string]interface{}, fields []string) string {
	if len(fields) == 0 {
		fields = []string{"thread_id", "threadId", "session_id", "sessionId"}
	}
	for _, field := range fields {
		key := strings.TrimSpace(field)
		if key == "" {
			continue
		}
		if value, ok := payload[key]; ok {
			text := strings.TrimSpace(fmt.Sprint(value))
			if text != "" && text != "<nil>" {
				return text
			}
		}
	}
	return ""
}

func extractAgentMessageText(item map[string]interface{}) string {
	if text, _ := item["text"].(string); strings.TrimSpace(text) != "" {
		return text
	}
	content, _ := item["content"].([]interface{})
	if len(content) == 0 {
		return ""
	}
	var out strings.Builder
	for _, raw := range content {
		entry, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		if text, _ := entry["text"].(string); text != "" {
			out.WriteString(text)
		}
	}
	return out.String()
}

func parseCommandExecutionStarted(payload map[string]interface{}) *llm.StreamEvent {
	item, _ := payload["item"].(map[string]interface{})
	if strings.ToLower(strings.TrimSpace(asString(item["type"]))) != "command_execution" {
		return nil
	}

	callID := firstNonBlankString(asString(item["id"]), asString(payload["id"]))
	command := extractCommandText(item)
	args := map[string]interface{}{}
	if command != "" {
		args["command"] = command
	}

	return &llm.StreamEvent{
		Type: llm.StreamEventToolCall,
		ToolCall: &llm.ToolCall{
			ID:    callID,
			Name:  "command_execution",
			Args:  args,
			Class: llm.ToolClassDelegated,
		},
	}
}

func parseDelegatedToolStarted(payload map[string]interface{}) *llm.StreamEvent {
	item, _ := payload["item"].(map[string]interface{})
	itemType := strings.ToLower(strings.TrimSpace(asString(item["type"])))
	if itemType == "" || itemType == "command_execution" || itemType == "agent_message" || itemType == "reasoning" {
		return nil
	}

	callID := firstNonBlankString(asString(item["id"]), asString(payload["id"]))
	toolName := deriveDelegatedToolName(item)
	if toolName == "" {
		toolName = itemType
	}

	return &llm.StreamEvent{
		Type: llm.StreamEventToolCall,
		ToolCall: &llm.ToolCall{
			ID:    callID,
			Name:  toolName,
			Args:  extractDelegatedToolArgs(item),
			Class: llm.ToolClassDelegated,
		},
	}
}

func parseCommandExecutionCompleted(payload map[string]interface{}) *llm.StreamEvent {
	item, _ := payload["item"].(map[string]interface{})
	if strings.ToLower(strings.TrimSpace(asString(item["type"]))) != "command_execution" {
		return nil
	}

	callID := firstNonBlankString(asString(item["id"]), asString(payload["id"]))
	command := extractCommandText(item)
	status := firstNonBlankString(asString(item["status"]), asString(payload["status"]), "completed")
	exitCode, hasExitCode := firstInt(
		item["exit_code"],
		item["exitCode"],
		lookupMap(item, "result", "exit_code"),
		lookupMap(item, "result", "exitCode"),
		payload["exit_code"],
		payload["exitCode"],
	)
	output := firstNonBlankString(
		asString(item["aggregated_output"]),
		asString(item["output"]),
		asString(lookupMap(item, "result", "aggregated_output")),
		asString(lookupMap(item, "result", "output")),
		asString(payload["aggregated_output"]),
		asString(payload["output"]),
	)

	ok := isSuccessfulCommandStatus(status)
	if hasExitCode {
		ok = ok && exitCode == 0
	}

	errText := firstNonBlankString(
		asString(item["error"]),
		asString(lookupMap(item, "result", "error")),
		asString(payload["error"]),
	)
	if !ok && errText == "" {
		if hasExitCode {
			errText = fmt.Sprintf("command exited with code %d", exitCode)
		} else {
			errText = fmt.Sprintf("command status %s", status)
		}
	}

	data := map[string]interface{}{}
	if command != "" {
		data["command"] = command
	}
	if output != "" {
		data["aggregated_output"] = output
	}
	if hasExitCode {
		data["exit_code"] = exitCode
	}

	return &llm.StreamEvent{
		Type: llm.StreamEventToolResult,
		ToolResult: &llm.ToolResult{
			CallID: callID,
			Tool:   "command_execution",
			Class:  llm.ToolClassDelegated,
			OK:     ok,
			Status: status,
			Error:  errText,
			Data:   data,
		},
	}
}

func parseDelegatedToolCompleted(payload map[string]interface{}) *llm.StreamEvent {
	item, _ := payload["item"].(map[string]interface{})
	itemType := strings.ToLower(strings.TrimSpace(asString(item["type"])))
	if itemType == "" || itemType == "command_execution" || itemType == "agent_message" || itemType == "reasoning" {
		return nil
	}

	callID := firstNonBlankString(asString(item["id"]), asString(payload["id"]))
	toolName := deriveDelegatedToolName(item)
	if toolName == "" {
		toolName = itemType
	}
	status := firstNonBlankString(asString(item["status"]), asString(payload["status"]), "completed")
	errText := firstNonBlankString(
		asLooseString(item["error"]),
		asLooseString(lookupMap(item, "result", "error")),
		asLooseString(payload["error"]),
	)
	ok := isSuccessfulCommandStatus(status) && strings.TrimSpace(errText) == ""
	if !ok && errText == "" {
		errText = fmt.Sprintf("%s status %s", toolName, status)
	}

	data := extractDelegatedToolResultData(item)

	return &llm.StreamEvent{
		Type: llm.StreamEventToolResult,
		ToolResult: &llm.ToolResult{
			CallID: callID,
			Tool:   toolName,
			Class:  llm.ToolClassDelegated,
			OK:     ok,
			Status: status,
			Error:  errText,
			Data:   data,
		},
	}
}

func deriveDelegatedToolName(item map[string]interface{}) string {
	itemType := strings.ToLower(strings.TrimSpace(asString(item["type"])))
	if itemType == "mcp_tool_call" {
		if tool := firstNonBlankString(
			asString(item["tool"]),
			asString(item["name"]),
			asString(lookupMap(item, "input", "tool")),
		); tool != "" {
			return tool
		}
	}
	if tool := firstNonBlankString(asString(item["tool"]), asString(item["name"])); tool != "" {
		return tool
	}
	return itemType
}

func extractDelegatedToolArgs(item map[string]interface{}) map[string]interface{} {
	args := map[string]interface{}{}
	for _, key := range []string{"server", "tool", "query", "action", "arguments"} {
		value, ok := item[key]
		if !ok || value == nil {
			continue
		}
		args[key] = value
	}
	if len(args) == 0 {
		return nil
	}
	return args
}

func extractDelegatedToolResultData(item map[string]interface{}) map[string]interface{} {
	data := map[string]interface{}{}
	for key, value := range item {
		switch key {
		case "id", "type", "status", "error":
			continue
		}
		if value == nil {
			continue
		}
		data[key] = value
	}
	if len(data) == 0 {
		return nil
	}
	return data
}

func extractCommandText(item map[string]interface{}) string {
	return firstNonBlankString(
		asString(item["command"]),
		asString(item["command_line"]),
		asString(lookupMap(item, "input", "command")),
		asString(lookupMap(item, "args", "command")),
	)
}

func lookupMap(value interface{}, keys ...string) interface{} {
	current := value
	for _, key := range keys {
		next, ok := current.(map[string]interface{})
		if !ok {
			return nil
		}
		current, ok = next[key]
		if !ok {
			return nil
		}
	}
	return current
}

func firstInt(values ...interface{}) (int, bool) {
	for _, value := range values {
		switch typed := value.(type) {
		case int:
			return typed, true
		case int32:
			return int(typed), true
		case int64:
			return int(typed), true
		case float64:
			return int(typed), true
		case float32:
			return int(typed), true
		case json.Number:
			if parsed, err := typed.Int64(); err == nil {
				return int(parsed), true
			}
		}
	}
	return 0, false
}

func asString(value interface{}) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case json.Number:
		return strings.TrimSpace(typed.String())
	default:
		return ""
	}
}

func asLooseString(value interface{}) string {
	if value == nil {
		return ""
	}
	if text := asString(value); text != "" {
		return text
	}
	text := strings.TrimSpace(fmt.Sprint(value))
	if text == "" || text == "<nil>" {
		return ""
	}
	return text
}

func firstNonBlankString(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func (c *LocalClient) resumeOutputMode() string {
	mode := strings.ToLower(strings.TrimSpace(c.settings.ResumeOutput))
	switch mode {
	case "json", "jsonl", "text":
		return mode
	default:
		return "text"
	}
}

func isSuccessfulCommandStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "completed", "success", "succeeded", "ok":
		return true
	default:
		return false
	}
}

func normalizeNonBlankArgs(args []string) []string {
	if len(args) == 0 {
		return nil
	}
	out := make([]string, 0, len(args))
	for _, raw := range args {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		out = append(out, value)
	}
	return out
}

func expandSessionArgs(args []string, sessionID string, defaults []string) []string {
	templates := args
	if len(templates) == 0 {
		templates = defaults
	}
	out := make([]string, 0, len(templates))
	for _, arg := range templates {
		expanded := strings.ReplaceAll(arg, "{sessionId}", sessionID)
		expanded = strings.TrimSpace(expanded)
		if expanded == "" {
			continue
		}
		out = append(out, expanded)
	}
	return out
}

func buildEnv(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}
	pairs := make([]string, 0, len(env))
	for key, value := range env {
		pairs = append(pairs, fmt.Sprintf("%s=%s", key, value))
	}
	return pairs
}

func emitEvent(ctx context.Context, ch chan<- llm.StreamEvent, evt llm.StreamEvent) bool {
	select {
	case ch <- evt:
		return true
	case <-ctx.Done():
		return false
	}
}

func emitError(ctx context.Context, ch chan<- error, err error) bool {
	if err == nil {
		return true
	}
	select {
	case ch <- err:
		return true
	case <-ctx.Done():
		return false
	}
}
