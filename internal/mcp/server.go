package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"

	"github.com/dgriffin831/localclaw/internal/mcp/protocol"
)

type ToolHandler func(ctx context.Context, args map[string]interface{}) protocol.CallToolResult

type ToolRegistration struct {
	Definition protocol.Tool
	Handler    ToolHandler
}

type Settings struct {
	ServerName    string
	ServerVersion string
	Tools         []ToolRegistration
}

type Server struct {
	serverName    string
	serverVersion string
	tools         map[string]ToolRegistration
	orderedTools  []protocol.Tool
	writeMu       sync.Mutex
}

func NewServer(settings Settings) *Server {
	name := strings.TrimSpace(settings.ServerName)
	if name == "" {
		name = "localclaw"
	}
	version := strings.TrimSpace(settings.ServerVersion)
	if version == "" {
		version = "dev"
	}
	registered := make(map[string]ToolRegistration, len(settings.Tools))
	toolIndexes := make(map[string]int, len(settings.Tools))
	ordered := make([]protocol.Tool, 0, len(settings.Tools))
	for _, tool := range settings.Tools {
		toolName := strings.TrimSpace(tool.Definition.Name)
		if toolName == "" || tool.Handler == nil {
			continue
		}
		tool.Definition.Name = toolName
		registered[toolName] = tool
		if idx, exists := toolIndexes[toolName]; exists {
			ordered[idx] = tool.Definition
			continue
		}
		toolIndexes[toolName] = len(ordered)
		ordered = append(ordered, tool.Definition)
	}
	sort.Slice(ordered, func(i, j int) bool {
		return ordered[i].Name < ordered[j].Name
	})
	return &Server{
		serverName:    name,
		serverVersion: version,
		tools:         registered,
		orderedTools:  ordered,
	}
}

func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	if in == nil {
		return fmt.Errorf("input reader is required")
	}
	if out == nil {
		return fmt.Errorf("output writer is required")
	}

	type decodeResult struct {
		req protocol.Request
		err error
	}
	decodeCh := make(chan decodeResult, 1)
	go func() {
		decoder := json.NewDecoder(in)
		for {
			var req protocol.Request
			err := decoder.Decode(&req)
			decodeCh <- decodeResult{req: req, err: err}
			if err != nil {
				return
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return nil
		case decoded := <-decodeCh:
			if decoded.err != nil {
				err := decoded.err
				if errors.Is(err, io.EOF) {
					return nil
				}
				_ = s.writeError(out, nil, protocol.JSONRPCParseError, "invalid JSON-RPC request", err.Error())
				return nil
			}
			req := decoded.req

			if req.JSONRPC != "" && req.JSONRPC != protocol.JSONRPCVersion {
				if err := s.writeError(out, req.ID, protocol.JSONRPCInvalidRequest, "unsupported jsonrpc version", req.JSONRPC); err != nil {
					return err
				}
				continue
			}
			if strings.TrimSpace(req.Method) == "" {
				if err := s.writeError(out, req.ID, protocol.JSONRPCInvalidRequest, "method is required", nil); err != nil {
					return err
				}
				continue
			}

			resp, err := s.handleRequest(ctx, req)
			if err != nil {
				if writeErr := s.writeError(out, req.ID, protocol.JSONRPCInternalError, "internal error", err.Error()); writeErr != nil {
					return writeErr
				}
				continue
			}
			if err := s.writeResponse(out, resp); err != nil {
				return err
			}
		}
	}
}

func (s *Server) handleRequest(ctx context.Context, req protocol.Request) (protocol.Response, error) {
	switch req.Method {
	case "initialize":
		return s.handleInitialize(req.ID), nil
	case "tools/list":
		return s.handleToolsList(req.ID), nil
	case "tools/call":
		return s.handleToolsCall(ctx, req.ID, req.Params)
	default:
		return protocol.Response{
			JSONRPC: protocol.JSONRPCVersion,
			ID:      req.ID,
			Error: &protocol.Error{
				Code:    protocol.JSONRPCMethodNotFound,
				Message: fmt.Sprintf("unknown method %q", req.Method),
			},
		}, nil
	}
}

func (s *Server) handleInitialize(id interface{}) protocol.Response {
	result, _ := json.Marshal(protocol.InitializeResult{
		ProtocolVersion: protocol.JSONRPCVersion,
		ServerInfo: protocol.ServerInfo{
			Name:    s.serverName,
			Version: s.serverVersion,
		},
		Capabilities: map[string]interface{}{
			"tools": map[string]interface{}{},
		},
	})
	return protocol.Response{JSONRPC: protocol.JSONRPCVersion, ID: id, Result: result}
}

func (s *Server) handleToolsList(id interface{}) protocol.Response {
	result, _ := json.Marshal(protocol.ListToolsResult{Tools: append([]protocol.Tool{}, s.orderedTools...)})
	return protocol.Response{JSONRPC: protocol.JSONRPCVersion, ID: id, Result: result}
}

func (s *Server) handleToolsCall(ctx context.Context, id interface{}, rawParams json.RawMessage) (protocol.Response, error) {
	var params protocol.CallToolParams
	if len(rawParams) > 0 {
		if err := json.Unmarshal(rawParams, &params); err != nil {
			return protocol.Response{
				JSONRPC: protocol.JSONRPCVersion,
				ID:      id,
				Error: &protocol.Error{
					Code:    protocol.JSONRPCInvalidParams,
					Message: "invalid tools/call params",
					Data:    err.Error(),
				},
			}, nil
		}
	}
	name := strings.TrimSpace(params.Name)
	if name == "" {
		return protocol.Response{
			JSONRPC: protocol.JSONRPCVersion,
			ID:      id,
			Error: &protocol.Error{
				Code:    protocol.JSONRPCInvalidParams,
				Message: "tool name is required",
			},
		}, nil
	}
	tool, ok := s.tools[name]
	if !ok {
		result, _ := json.Marshal(protocol.CallToolResult{
			IsError: true,
			StructuredContent: map[string]interface{}{
				"ok":    false,
				"error": fmt.Sprintf("unknown tool %q", name),
			},
		})
		return protocol.Response{JSONRPC: protocol.JSONRPCVersion, ID: id, Result: result}, nil
	}

	args := params.Arguments
	if args == nil {
		args = map[string]interface{}{}
	}
	resultObj := tool.Handler(ctx, args)
	result, err := json.Marshal(resultObj)
	if err != nil {
		return protocol.Response{}, err
	}
	return protocol.Response{JSONRPC: protocol.JSONRPCVersion, ID: id, Result: result}, nil
}

func (s *Server) writeError(out io.Writer, id interface{}, code int, message string, data interface{}) error {
	return s.writeResponse(out, protocol.Response{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      id,
		Error: &protocol.Error{
			Code:    code,
			Message: message,
			Data:    data,
		},
	})
}

func (s *Server) writeResponse(out io.Writer, resp protocol.Response) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	enc := json.NewEncoder(out)
	return enc.Encode(resp)
}
