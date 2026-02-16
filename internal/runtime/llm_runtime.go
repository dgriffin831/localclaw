package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/dgriffin831/localclaw/internal/llm"
)

func (a *App) promptStreamFromClient(ctx context.Context, req llm.Request) (<-chan llm.StreamEvent, <-chan error) {
	if client, ok := a.llm.(llm.RequestClient); ok && a.llm.Capabilities().SupportsRequestOptions {
		return client.PromptStreamRequest(ctx, req)
	}
	return a.llm.PromptStream(ctx, llm.ComposePromptFallback(req))
}

func (a *App) runStructuredToolLoop(
	ctx context.Context,
	resolution SessionResolution,
	inEvents <-chan llm.StreamEvent,
	inErrs <-chan error,
) (<-chan llm.StreamEvent, <-chan error) {
	outEvents := make(chan llm.StreamEvent, 32)
	outErrs := make(chan error, 1)

	go func() {
		defer close(outEvents)
		defer close(outErrs)

		eventsOpen := true
		errsOpen := true
		for eventsOpen || errsOpen {
			select {
			case evt, ok := <-inEvents:
				if !ok {
					eventsOpen = false
					continue
				}
				if evt.Type == llm.StreamEventToolCall {
					if !emitRuntimeEvent(ctx, outEvents, evt) {
						return
					}
					if !a.handleToolCallEvent(ctx, resolution, evt.ToolCall, outEvents, outErrs) {
						return
					}
					continue
				}
				if !emitRuntimeEvent(ctx, outEvents, evt) {
					return
				}
			case err, ok := <-inErrs:
				if !ok {
					errsOpen = false
					continue
				}
				if err != nil {
					if !emitRuntimeError(ctx, outErrs, err) {
						return
					}
				}
			case <-ctx.Done():
				_ = emitRuntimeError(ctx, outErrs, ctx.Err())
				return
			}
		}
	}()

	return outEvents, outErrs
}

func (a *App) handleToolCallEvent(
	ctx context.Context,
	resolution SessionResolution,
	call *llm.ToolCall,
	outEvents chan<- llm.StreamEvent,
	outErrs chan<- error,
) bool {
	if call == nil {
		return emitRuntimeError(ctx, outErrs, errors.New("structured tool call event missing payload"))
	}

	a.appendToolTranscriptEvent(ctx, resolution, "tool_call_started", map[string]interface{}{
		"id":    call.ID,
		"tool":  call.Name,
		"class": call.Class,
	})

	result := a.ExecuteTool(ctx, ToolExecutionRequest{
		AgentID:   resolution.AgentID,
		SessionID: resolution.SessionID,
		Name:      call.Name,
		Class:     call.Class,
		Args:      call.Args,
	})

	toolResult := llm.ToolResult{
		CallID: call.ID,
		Tool:   result.Tool,
		OK:     result.OK,
		Data:   result.Data,
		Error:  strings.TrimSpace(result.Error),
		Status: summarizeToolStatus(result),
	}

	if call.Respond != nil {
		if err := call.Respond(ctx, toolResult); err != nil {
			toolResult.OK = false
			if toolResult.Error == "" {
				toolResult.Error = err.Error()
			} else {
				toolResult.Error = toolResult.Error + "; respond failed: " + err.Error()
			}
			toolResult.Status = "error"
		}
	}

	eventName := "tool_call_completed"
	if !toolResult.OK && toolResult.Status == "blocked" {
		eventName = "tool_call_blocked"
	}
	if !toolResult.OK && toolResult.Status == "error" {
		eventName = "tool_call_failed"
	}
	a.appendToolTranscriptEvent(ctx, resolution, eventName, map[string]interface{}{
		"id":     call.ID,
		"tool":   toolResult.Tool,
		"ok":     toolResult.OK,
		"status": toolResult.Status,
		"error":  toolResult.Error,
	})

	return emitRuntimeEvent(ctx, outEvents, llm.StreamEvent{
		Type:       llm.StreamEventToolResult,
		ToolResult: &toolResult,
	})
}

func summarizeToolStatus(result ToolExecutionResult) string {
	if result.OK {
		return "completed"
	}
	if strings.Contains(strings.ToLower(result.Error), "policy blocked") {
		return "blocked"
	}
	return "error"
}

func (a *App) appendToolTranscriptEvent(ctx context.Context, resolution SessionResolution, event string, payload map[string]interface{}) {
	if a.transcript == nil {
		return
	}
	record := map[string]interface{}{
		"type":       event,
		"agentId":    resolution.AgentID,
		"sessionId":  resolution.SessionID,
		"sessionKey": resolution.SessionKey,
	}
	for key, value := range payload {
		record[key] = value
	}
	data, err := json.Marshal(record)
	if err != nil {
		return
	}
	_ = a.AppendSessionTranscriptMessage(ctx, resolution.AgentID, resolution.SessionID, "tool", string(data))
}

func emitRuntimeEvent(ctx context.Context, ch chan<- llm.StreamEvent, evt llm.StreamEvent) bool {
	select {
	case ch <- evt:
		return true
	case <-ctx.Done():
		return false
	}
}

func emitRuntimeError(ctx context.Context, ch chan<- error, err error) bool {
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
