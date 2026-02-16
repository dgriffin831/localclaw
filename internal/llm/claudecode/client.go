package claudecode

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/dgriffin831/localclaw/internal/llm"
)

// Backward-compatible aliases for legacy package references.
type StreamEventType = llm.StreamEventType
type StreamEvent = llm.StreamEvent

const (
	StreamEventDelta = llm.StreamEventDelta
	StreamEventFinal = llm.StreamEventFinal
)

type Settings struct {
	BinaryPath           string
	Profile              string
	UseGovCloud          bool
	BedrockRegion        string
	AuthMode             string
	StrictMCPConfig      bool
	MCPConfigDir         string
	MCPServerBinaryPath  string
	MCPServerArgs        []string
	MCPServerEnvironment map[string]string
}

type LocalClient struct {
	settings Settings
}

func NewClient(settings Settings) *LocalClient {
	if strings.TrimSpace(settings.MCPServerBinaryPath) == "" {
		settings.MCPServerBinaryPath = "localclaw"
	}
	if len(settings.MCPServerArgs) == 0 {
		settings.MCPServerArgs = []string{"mcp", "serve"}
	}
	return &LocalClient{settings: settings}
}

func (c *LocalClient) ValidateMCPWiring() error {
	_, cleanup, err := c.prepareRunScopedMCPConfig()
	if cleanup != nil {
		cleanup()
	}
	if err != nil {
		return err
	}
	return nil
}

func (c *LocalClient) Capabilities() llm.Capabilities {
	return llm.Capabilities{
		SupportsRequestOptions: true,
		StructuredToolCalls:    false,
	}
}

func (c *LocalClient) Prompt(ctx context.Context, input string) (string, error) {
	events, errs := c.PromptStream(ctx, input)
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

func (c *LocalClient) PromptStreamRequest(ctx context.Context, req llm.Request) (<-chan llm.StreamEvent, <-chan error) {
	trimmedInput := strings.TrimSpace(req.Input)
	if trimmedInput == "" {
		events := make(chan llm.StreamEvent)
		errs := make(chan error, 1)
		defer close(events)
		defer close(errs)
		errs <- fmt.Errorf("input is required")
		return events, errs
	}

	mcpConfigPath, cleanup, err := c.prepareRunScopedMCPConfig()
	if err != nil {
		events := make(chan llm.StreamEvent)
		errs := make(chan error, 1)
		defer close(events)
		defer close(errs)
		errs <- fmt.Errorf("prepare claude mcp config: %w", err)
		return events, errs
	}

	return c.promptStreamWithArgs(ctx, c.buildCommandArgsForRequest(req, mcpConfigPath), cleanup)
}

func (c *LocalClient) PromptStream(ctx context.Context, input string) (<-chan llm.StreamEvent, <-chan error) {
	return c.PromptStreamRequest(ctx, llm.Request{Input: input})
}

func (c *LocalClient) promptStreamWithArgs(ctx context.Context, args []string, cleanup func()) (<-chan llm.StreamEvent, <-chan error) {
	cmd := exec.CommandContext(ctx, c.settings.BinaryPath, args...)
	cmd.Env = append(os.Environ(), c.buildEnv()...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		if cleanup != nil {
			cleanup()
		}
		events := make(chan llm.StreamEvent)
		errs := make(chan error, 1)
		defer close(events)
		defer close(errs)
		errs <- fmt.Errorf("create stdout pipe: %w", err)
		return events, errs
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		if cleanup != nil {
			cleanup()
		}
		events := make(chan llm.StreamEvent)
		errs := make(chan error, 1)
		defer close(events)
		defer close(errs)
		errs <- fmt.Errorf("create stderr pipe: %w", err)
		return events, errs
	}
	if err := cmd.Start(); err != nil {
		if cleanup != nil {
			cleanup()
		}
		events := make(chan llm.StreamEvent)
		errs := make(chan error, 1)
		defer close(events)
		defer close(errs)
		errs <- fmt.Errorf("start claude code cli: %w", err)
		return events, errs
	}

	events := make(chan llm.StreamEvent, 32)
	errs := make(chan error, 1)

	go func() {
		defer func() {
			if cleanup != nil {
				cleanup()
			}
		}()
		defer close(events)
		defer close(errs)

		stderrTextCh := make(chan string, 1)
		go func() {
			data, _ := io.ReadAll(stderr)
			stderrTextCh <- strings.TrimSpace(string(data))
		}()

		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)

		toolNames := map[string]string{}
		var deltaText strings.Builder
		finalSeen := false
		resultErr := ""

		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}

			parsedEvents, parseResultErr, parseErr := parseStreamJSONLine(line, toolNames)
			if parseErr != nil {
				// Compatibility fallback: if parsing fails, treat stdout as raw text delta.
				delta := line + "\n"
				deltaText.WriteString(delta)
				if !emitEvent(ctx, events, llm.StreamEvent{Type: llm.StreamEventDelta, Text: delta}) {
					_ = cmd.Process.Kill()
					_ = cmd.Wait()
					return
				}
				continue
			}
			if parseResultErr != "" {
				resultErr = parseResultErr
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
			emitError(ctx, errs, fmt.Errorf("read claude code output: %w", scanErr))
			return
		}

		waitErr := cmd.Wait()
		stderrText := <-stderrTextCh
		if waitErr != nil {
			if stderrText != "" {
				emitError(ctx, errs, fmt.Errorf("claude code cli execution failed: %w: %s", waitErr, stderrText))
				return
			}
			emitError(ctx, errs, fmt.Errorf("claude code cli execution failed: %w", waitErr))
			return
		}
		if resultErr != "" {
			emitError(ctx, errs, fmt.Errorf("claude code cli result error: %s", resultErr))
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

func (c *LocalClient) buildCommandArgs(input string) []string {
	return []string{
		"-p", input,
		"--output-format", "stream-json",
		"--verbose",
	}
}

func (c *LocalClient) buildCommandArgsForRequest(req llm.Request, mcpConfigPath string) []string {
	args := c.buildCommandArgs(strings.TrimSpace(req.Input))
	args = append(args, "--mcp-config", mcpConfigPath)
	if c.settings.StrictMCPConfig {
		args = append(args, "--strict-mcp-config")
	}
	if systemPrompt := buildAppendSystemPrompt(req); systemPrompt != "" {
		args = append(args, "--append-system-prompt", systemPrompt)
	}
	return args
}

func buildAppendSystemPrompt(req llm.Request) string {
	parts := make([]string, 0, 2)
	if system := strings.TrimSpace(req.SystemContext); system != "" {
		parts = append(parts, system)
	}
	if skill := strings.TrimSpace(req.SkillPrompt); skill != "" {
		parts = append(parts, skill)
	}
	return strings.Join(parts, "\n\n")
}

type claudeMCPConfig struct {
	MCPServers map[string]claudeMCPServer `json:"mcpServers"`
}

type claudeMCPServer struct {
	Type    string            `json:"type"`
	Command string            `json:"command"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

func (c *LocalClient) prepareRunScopedMCPConfig() (string, func(), error) {
	serverCommand := strings.TrimSpace(c.settings.MCPServerBinaryPath)
	if serverCommand == "" {
		return "", nil, fmt.Errorf("mcp server binary path is required")
	}
	serverArgs := normalizeNonBlankArgs(c.settings.MCPServerArgs)
	if !containsMCPServe(serverArgs) {
		return "", nil, fmt.Errorf("mcp server args must include \"mcp serve\"")
	}

	configDir := strings.TrimSpace(c.settings.MCPConfigDir)
	if configDir == "" {
		configDir = os.TempDir()
	}
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return "", nil, fmt.Errorf("create mcp config dir %q: %w", configDir, err)
	}

	file, err := os.CreateTemp(configDir, "localclaw-claude-mcp-*.json")
	if err != nil {
		return "", nil, fmt.Errorf("create run-scoped mcp config file: %w", err)
	}

	configPath := file.Name()
	cleanup := func() {
		_ = os.Remove(configPath)
	}

	payload := claudeMCPConfig{
		MCPServers: map[string]claudeMCPServer{
			"localclaw": {
				Type:    "stdio",
				Command: serverCommand,
				Args:    serverArgs,
				Env:     copyEnvMap(c.settings.MCPServerEnvironment),
			},
		},
	}
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(payload); err != nil {
		_ = file.Close()
		cleanup()
		return "", nil, fmt.Errorf("write mcp config %q: %w", configPath, err)
	}
	if err := file.Close(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("close mcp config %q: %w", configPath, err)
	}
	if !filepath.IsAbs(configPath) {
		cleanup()
		return "", nil, fmt.Errorf("run-scoped mcp config path must be absolute: %q", configPath)
	}
	return configPath, cleanup, nil
}

func normalizeNonBlankArgs(args []string) []string {
	if len(args) == 0 {
		return nil
	}
	out := make([]string, 0, len(args))
	for _, arg := range args {
		trimmed := strings.TrimSpace(arg)
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	return out
}

func containsMCPServe(args []string) bool {
	if len(args) < 2 {
		return false
	}
	for i := 0; i < len(args)-1; i++ {
		if strings.EqualFold(args[i], "mcp") && strings.EqualFold(args[i+1], "serve") {
			return true
		}
	}
	return false
}

func copyEnvMap(src map[string]string) map[string]string {
	if len(src) == 0 {
		return map[string]string{}
	}
	dst := make(map[string]string, len(src))
	for key, value := range src {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		dst[key] = value
	}
	return dst
}

type claudeStreamEnvelope struct {
	Type          string                 `json:"type"`
	Subtype       string                 `json:"subtype,omitempty"`
	IsError       bool                   `json:"is_error,omitempty"`
	Result        string                 `json:"result,omitempty"`
	Model         string                 `json:"model,omitempty"`
	Tools         []string               `json:"tools,omitempty"`
	Message       claudeStreamMessage    `json:"message,omitempty"`
	ToolUseResult map[string]interface{} `json:"tool_use_result,omitempty"`
}

type claudeStreamMessage struct {
	Role    string                `json:"role,omitempty"`
	Content []claudeStreamContent `json:"content,omitempty"`
}

type claudeStreamContent struct {
	Type      string                 `json:"type"`
	Text      string                 `json:"text,omitempty"`
	ID        string                 `json:"id,omitempty"`
	Name      string                 `json:"name,omitempty"`
	Input     map[string]interface{} `json:"input,omitempty"`
	ToolUseID string                 `json:"tool_use_id,omitempty"`
	Content   interface{}            `json:"content,omitempty"`
	IsError   bool                   `json:"is_error,omitempty"`
}

func parseStreamJSONLine(line string, toolNames map[string]string) ([]llm.StreamEvent, string, error) {
	var env claudeStreamEnvelope
	if err := json.Unmarshal([]byte(line), &env); err != nil {
		return nil, "", err
	}

	switch strings.ToLower(strings.TrimSpace(env.Type)) {
	case "system":
		return parseSystemEvents(env), "", nil
	case "assistant":
		return parseAssistantMessageEvents(env.Message, toolNames), "", nil
	case "user":
		return parseUserMessageToolResultEvents(env, toolNames), "", nil
	case "result":
		events := []llm.StreamEvent{}
		result := strings.TrimSpace(env.Result)
		if result != "" {
			events = append(events, llm.StreamEvent{
				Type: llm.StreamEventFinal,
				Text: result,
			})
		}
		if env.IsError {
			if result != "" {
				return events, result, nil
			}
			subtype := strings.TrimSpace(env.Subtype)
			if subtype == "" {
				subtype = "unknown"
			}
			return events, "result event reported error (" + subtype + ")", nil
		}
		return events, "", nil
	default:
		return nil, "", nil
	}
}

func parseSystemEvents(env claudeStreamEnvelope) []llm.StreamEvent {
	if strings.ToLower(strings.TrimSpace(env.Subtype)) != "init" {
		return nil
	}

	tools := normalizeProviderToolNames(env.Tools)
	if strings.TrimSpace(env.Model) == "" && len(tools) == 0 {
		return nil
	}
	return []llm.StreamEvent{
		{
			Type: llm.StreamEventProviderMetadata,
			ProviderMetadata: &llm.ProviderMetadata{
				Provider: "claudecode",
				Model:    strings.TrimSpace(env.Model),
				Tools:    tools,
			},
		},
	}
}

func parseAssistantMessageEvents(msg claudeStreamMessage, toolNames map[string]string) []llm.StreamEvent {
	if len(msg.Content) == 0 {
		return nil
	}
	events := make([]llm.StreamEvent, 0, len(msg.Content))
	for _, item := range msg.Content {
		switch strings.ToLower(strings.TrimSpace(item.Type)) {
		case "text":
			text := item.Text
			if strings.TrimSpace(text) == "" {
				continue
			}
			events = append(events, llm.StreamEvent{
				Type: llm.StreamEventDelta,
				Text: text,
			})
		case "tool_use":
			callID := strings.TrimSpace(item.ID)
			toolName := strings.TrimSpace(item.Name)
			if callID != "" && toolName != "" && toolNames != nil {
				toolNames[callID] = toolName
			}
			args := map[string]interface{}{}
			for key, value := range item.Input {
				args[key] = value
			}
			events = append(events, llm.StreamEvent{
				Type: llm.StreamEventToolCall,
				ToolCall: &llm.ToolCall{
					ID:    callID,
					Name:  toolName,
					Args:  args,
					Class: llm.ToolClassDelegated,
				},
			})
		}
	}
	return events
}

func parseUserMessageToolResultEvents(env claudeStreamEnvelope, toolNames map[string]string) []llm.StreamEvent {
	content := env.Message.Content
	if len(content) == 0 {
		return nil
	}
	events := make([]llm.StreamEvent, 0, len(content))
	for _, item := range content {
		if strings.ToLower(strings.TrimSpace(item.Type)) != "tool_result" {
			continue
		}
		callID := strings.TrimSpace(item.ToolUseID)
		toolName := ""
		if toolNames != nil && callID != "" {
			toolName = strings.TrimSpace(toolNames[callID])
		}
		if toolName == "" {
			toolName = "tool"
		}

		result := llm.ToolResult{
			CallID: callID,
			Tool:   toolName,
			Class:  llm.ToolClassDelegated,
			OK:     !item.IsError,
		}
		if item.IsError {
			result.Status = "error"
			result.Error = renderToolResultText(item.Content)
			if strings.TrimSpace(result.Error) == "" {
				result.Error = "tool returned error"
			}
		} else {
			result.Status = "completed"
			result.Data = map[string]interface{}{}
			if text := renderToolResultText(item.Content); text != "" {
				result.Data["content"] = text
			}
			if len(env.ToolUseResult) > 0 {
				result.Data["provider_result"] = env.ToolUseResult
			}
			if len(result.Data) == 0 {
				result.Data = nil
			}
		}

		events = append(events, llm.StreamEvent{
			Type:       llm.StreamEventToolResult,
			ToolResult: &result,
		})
	}
	return events
}

func renderToolResultText(raw interface{}) string {
	switch v := raw.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(v)
	case []interface{}:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			text := renderToolResultText(item)
			if text != "" {
				parts = append(parts, text)
			}
		}
		return strings.TrimSpace(strings.Join(parts, "\n"))
	case map[string]interface{}:
		if text, ok := v["text"].(string); ok && strings.TrimSpace(text) != "" {
			return strings.TrimSpace(text)
		}
		if content, ok := v["content"]; ok {
			return renderToolResultText(content)
		}
		buf, err := json.Marshal(v)
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(buf))
	case bool:
		return strconv.FormatBool(v)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	default:
		return strings.TrimSpace(fmt.Sprint(v))
	}
}

func normalizeProviderToolNames(raw []string) []string {
	if len(raw) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(raw))
	for _, name := range raw {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		key := strings.ToLower(trimmed)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func emitEvent(ctx context.Context, events chan<- llm.StreamEvent, evt llm.StreamEvent) bool {
	select {
	case events <- evt:
		return true
	case <-ctx.Done():
		return false
	}
}

func emitError(ctx context.Context, errs chan<- error, err error) {
	select {
	case errs <- err:
	case <-ctx.Done():
	}
}

func (c *LocalClient) buildEnv() []string {
	env := []string{}

	if c.settings.Profile != "" {
		env = append(env, "AWS_PROFILE="+c.settings.Profile)
	}
	if c.settings.BedrockRegion != "" {
		env = append(env, "AWS_REGION="+c.settings.BedrockRegion)
		env = append(env, "AWS_DEFAULT_REGION="+c.settings.BedrockRegion)
	}
	if c.settings.UseGovCloud {
		// TODO: replace with exact Claude Code CLI GovCloud knobs once pinned.
		env = append(env, "LOCALCLAW_GOVCLOUD_MODE=1")
	}

	return env
}
