package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"

	"github.com/dgriffin831/localclaw/internal/mcp/protocol"
)

type blockingReader struct {
	started chan struct{}
	release chan struct{}
}

func (r *blockingReader) Read(p []byte) (int, error) {
	select {
	case <-r.started:
	default:
		close(r.started)
	}
	<-r.release
	return 0, io.EOF
}

func TestServerListsAndCallsTools(t *testing.T) {
	server := NewServer(Settings{Tools: []ToolRegistration{{
		Definition: protocol.Tool{Name: "localclaw_memory_search"},
		Handler: func(ctx context.Context, args map[string]interface{}) protocol.CallToolResult {
			return protocol.CallToolResult{StructuredContent: map[string]interface{}{"ok": true}}
		},
	}}})

	input := bytes.NewBufferString("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"tools/list\",\"params\":{}}\n{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"tools/call\",\"params\":{\"name\":\"localclaw_memory_search\",\"arguments\":{\"query\":\"needle\"}}}\n")
	var out bytes.Buffer
	if err := server.Serve(context.Background(), input, &out); err != nil {
		t.Fatalf("Serve error: %v", err)
	}

	decoder := json.NewDecoder(&out)
	var listResp protocol.Response
	if err := decoder.Decode(&listResp); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	var listResult protocol.ListToolsResult
	if err := json.Unmarshal(listResp.Result, &listResult); err != nil {
		t.Fatalf("unmarshal list result: %v", err)
	}
	if len(listResult.Tools) != 1 || listResult.Tools[0].Name != "localclaw_memory_search" {
		t.Fatalf("unexpected tools list: %+v", listResult.Tools)
	}

	var callResp protocol.Response
	if err := decoder.Decode(&callResp); err != nil {
		t.Fatalf("decode call response: %v", err)
	}
	var callResult protocol.CallToolResult
	if err := json.Unmarshal(callResp.Result, &callResult); err != nil {
		t.Fatalf("unmarshal call result: %v", err)
	}
	if callResult.IsError {
		t.Fatalf("expected non-error call result")
	}
	if callResult.StructuredContent["ok"] != true {
		t.Fatalf("unexpected call payload: %+v", callResult.StructuredContent)
	}
}

func TestServerReturnsMethodNotFound(t *testing.T) {
	server := NewServer(Settings{})
	input := bytes.NewBufferString("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"not_real\",\"params\":{}}\n")
	var out bytes.Buffer
	if err := server.Serve(context.Background(), input, &out); err != nil {
		t.Fatalf("Serve error: %v", err)
	}
	var resp protocol.Response
	if err := json.NewDecoder(&out).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error == nil || resp.Error.Code != protocol.JSONRPCMethodNotFound {
		t.Fatalf("unexpected error response: %+v", resp.Error)
	}
}

func TestServerReturnsOnContextCancelWhileWaitingForInput(t *testing.T) {
	server := NewServer(Settings{})
	reader := &blockingReader{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	var out bytes.Buffer

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- server.Serve(ctx, reader, &out)
	}()

	select {
	case <-reader.started:
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("Serve did not start reading input")
	}
	cancel()
	close(reader.release)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve error: %v", err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("Serve did not return after context cancellation")
	}
}

func TestServerListToolsDeduplicatesToolNames(t *testing.T) {
	server := NewServer(Settings{Tools: []ToolRegistration{
		{
			Definition: protocol.Tool{Name: "localclaw_memory_search", Description: "first"},
			Handler: func(ctx context.Context, args map[string]interface{}) protocol.CallToolResult {
				return protocol.CallToolResult{StructuredContent: map[string]interface{}{"ok": true}}
			},
		},
		{
			Definition: protocol.Tool{Name: "localclaw_memory_search", Description: "second"},
			Handler: func(ctx context.Context, args map[string]interface{}) protocol.CallToolResult {
				return protocol.CallToolResult{StructuredContent: map[string]interface{}{"ok": true}}
			},
		},
	}})

	input := bytes.NewBufferString("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"tools/list\",\"params\":{}}\n")
	var out bytes.Buffer
	if err := server.Serve(context.Background(), input, &out); err != nil {
		t.Fatalf("Serve error: %v", err)
	}

	var resp protocol.Response
	if err := json.NewDecoder(&out).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	var listResult protocol.ListToolsResult
	if err := json.Unmarshal(resp.Result, &listResult); err != nil {
		t.Fatalf("unmarshal list result: %v", err)
	}
	if len(listResult.Tools) != 1 {
		t.Fatalf("expected 1 deduplicated tool, got %d", len(listResult.Tools))
	}
	if listResult.Tools[0].Description != "second" {
		t.Fatalf("expected latest registration to win, got %+v", listResult.Tools[0])
	}
}
