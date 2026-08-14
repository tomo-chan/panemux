package boardmcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
)

// protocolVersion is the MCP protocol date this server implements.
const protocolVersion = "2024-11-05"

// serverName/serverVersion identify this server in the initialize handshake.
const serverName = "panemux-board"

//nolint:gochecknoglobals // overridable by main.go at build/link time, mirrors main.go's own `version` var
var serverVersion = "dev"

const (
	toolBoardStatus    = "board_status"
	toolBoardMessages  = "board_messages"
	toolBoardBroadcast = "board_broadcast"
)

// broadcastBodyField is the board_broadcast tool's "body" argument/schema
// field name — shared between this file's tool schema, callBroadcast's
// argument struct, and client.go's outgoing request body.
const broadcastBodyField = "body"

const (
	jsonrpcParseError     = -32700
	jsonrpcMethodNotFound = -32601
	jsonrpcInvalidParams  = -32602
)

//nolint:govet // fieldalignment: field order kept grouped by meaning, padding cost is negligible
type jsonrpcRequest struct {
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	ID      json.RawMessage `json:"id,omitempty"`
	JSONRPC string          `json:"jsonrpc"`
}

//nolint:govet // fieldalignment: field order kept grouped by meaning, padding cost is negligible
type jsonrpcResponse struct {
	Result  any             `json:"result,omitempty"`
	Error   *jsonrpcError   `json:"error,omitempty"`
	ID      json.RawMessage `json:"id,omitempty"`
	JSONRPC string          `json:"jsonrpc"`
}

type jsonrpcError struct {
	Message string `json:"message"`
	Code    int    `json:"code"`
}

// Error implements the error interface so a *jsonrpcError can also be
// returned as callTool's ordinary error result — see handleToolsCall.
func (e *jsonrpcError) Error() string { return e.Message }

// Server is the command center's narrow MCP server: three tools
// (board_status, board_messages, board_broadcast), each a thin wrapper
// around a BoardAPIClient call. It never calls agmsg or anything else
// directly — see docs/agent-board.md's Command center section.
type Server struct {
	client BoardAPIClient
}

// NewServer constructs a Server backed by client.
func NewServer(client BoardAPIClient) *Server {
	return &Server{client: client}
}

// Serve reads newline-delimited JSON-RPC 2.0 requests from r and writes
// newline-delimited responses to w, per MCP's stdio transport. It processes
// one line at a time, in order; a notification (a request with no id)
// produces no output line. Serve returns when r is exhausted or a
// read/write error occurs — a malformed request line is reported back to
// the client as a JSON-RPC error response, not treated as fatal to the
// connection.
func (s *Server) Serve(ctx context.Context, r io.Reader, w io.Writer) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		resp := s.handleLine(ctx, line)
		if resp == nil {
			continue
		}
		data, err := json.Marshal(resp)
		if err != nil {
			return fmt.Errorf("encoding mcp response: %w", err)
		}
		if _, err := w.Write(append(data, '\n')); err != nil {
			return fmt.Errorf("writing mcp response: %w", err)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("reading mcp request: %w", err)
	}
	return nil
}

// handleLine returns nil for a notification (no id, or a JSON-null id) —
// per the JSON-RPC 2.0 spec, notifications never receive a response, even
// an error one, since there is no id to correlate it with. A notification
// is never dispatched at all, not just left unanswered: nothing here would
// ever see the result of a state-changing call like board_broadcast if it
// ran as a notification, so — unlike a plain request whose caller is
// choosing to ignore the response — there would be no way to tell the
// difference between "didn't run" and "ran and nobody found out."
func (s *Server) handleLine(ctx context.Context, line []byte) *jsonrpcResponse {
	var req jsonrpcRequest
	if err := json.Unmarshal(line, &req); err != nil {
		return &jsonrpcResponse{
			JSONRPC: "2.0",
			Error:   &jsonrpcError{Code: jsonrpcParseError, Message: "parse error: " + err.Error()},
		}
	}
	if len(req.ID) == 0 || string(req.ID) == "null" {
		return nil
	}

	result, rpcErr := s.dispatch(ctx, req.Method, req.Params)
	resp := &jsonrpcResponse{JSONRPC: "2.0", ID: req.ID}
	if rpcErr != nil {
		resp.Error = rpcErr
	} else {
		resp.Result = result
	}
	return resp
}

func (s *Server) dispatch(ctx context.Context, method string, params json.RawMessage) (any, *jsonrpcError) {
	switch method {
	case "initialize":
		return s.handleInitialize(), nil
	case "notifications/initialized":
		return nil, nil
	case "tools/list":
		return s.handleToolsList(), nil
	case "tools/call":
		return s.handleToolsCall(ctx, params)
	default:
		return nil, &jsonrpcError{Code: jsonrpcMethodNotFound, Message: "method not found: " + method}
	}
}

type initializeResult struct {
	Capabilities    map[string]any `json:"capabilities"`
	ProtocolVersion string         `json:"protocolVersion"`
	ServerInfo      serverInfo     `json:"serverInfo"`
}

type serverInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

func (s *Server) handleInitialize() initializeResult {
	return initializeResult{
		ProtocolVersion: protocolVersion,
		Capabilities:    map[string]any{"tools": map[string]any{}},
		ServerInfo:      serverInfo{Name: serverName, Version: serverVersion},
	}
}

type toolDef struct {
	InputSchema map[string]any `json:"inputSchema"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
}

type toolsListResult struct {
	Tools []toolDef `json:"tools"`
}

// jsonSchemaFieldType is the JSON Schema "type" keyword, shared by every
// helper below so it's a single literal rather than one per call site.
const jsonSchemaFieldType = "type"

// objectSchema builds a JSON Schema "object" InputSchema, optionally
// requiring the given property names. These helpers exist so the hand-built
// schemas below don't each repeat JSON Schema keyword string literals
// ("type", "properties", "description", ...) at every call site.
func objectSchema(properties map[string]any, required ...string) map[string]any {
	schema := map[string]any{jsonSchemaFieldType: "object", "properties": properties}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

// typedProperty builds a JSON Schema property of the given primitive kind
// ("integer", "string", ...) with a human-readable description.
func typedProperty(kind, description string) map[string]any {
	return map[string]any{jsonSchemaFieldType: kind, "description": description}
}

// arrayProperty builds a JSON Schema "array" property whose items are all
// of itemKind, with a human-readable description.
func arrayProperty(itemKind, description string) map[string]any {
	return map[string]any{
		jsonSchemaFieldType: "array",
		"items":             map[string]any{jsonSchemaFieldType: itemKind},
		"description":       description,
	}
}

func (s *Server) handleToolsList() toolsListResult {
	return toolsListResult{Tools: []toolDef{
		{
			Name:        toolBoardStatus,
			Description: "Get the current self-reported status of every board-enabled pane " + statusToolFieldsHint,
			InputSchema: objectSchema(map[string]any{}),
		},
		{
			Name:        toolBoardMessages,
			Description: "Get recent board message history. Optional 'since' returns only newer messages.",
			InputSchema: objectSchema(map[string]any{
				"since": typedProperty("integer", "Only return messages newer than this sequence number"),
			}),
		},
		{
			Name:        toolBoardBroadcast,
			Description: "Send a message to one or more board-enabled panes.",
			InputSchema: objectSchema(map[string]any{
				"to":               arrayProperty("string", "Pane IDs to send to"),
				broadcastBodyField: typedProperty("string", "Message text"),
			}, "to", broadcastBodyField),
		},
	}}
}

const statusToolFieldsHint = "(working directory, branch, PR, idle/working/waiting state)."

//nolint:govet // fieldalignment: padding cost is negligible for a two-field struct
type toolCallParams struct {
	Arguments json.RawMessage `json:"arguments"`
	Name      string          `json:"name"`
}

type toolContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type toolCallResult struct {
	Content []toolContent `json:"content"`
	IsError bool          `json:"isError"`
}

func (s *Server) handleToolsCall(ctx context.Context, params json.RawMessage) (any, *jsonrpcError) {
	var p toolCallParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &jsonrpcError{Code: jsonrpcInvalidParams, Message: "invalid params: " + err.Error()}
	}

	raw, err := s.callTool(ctx, p)
	if err != nil {
		var rpcErr *jsonrpcError
		if asJSONRPCError(err, &rpcErr) {
			return nil, rpcErr
		}
		return toolCallResult{Content: []toolContent{{Type: "text", Text: err.Error()}}, IsError: true}, nil
	}
	return toolCallResult{Content: []toolContent{{Type: "text", Text: string(raw)}}, IsError: false}, nil
}

// asJSONRPCError reports whether err is a *jsonrpcError produced by
// callTool's own argument validation (never one returned by BoardAPIClient,
// which only ever returns ordinary errors) and, if so, sets *out to it.
// A plain type assertion is intentionally not used here (errorlint would
// flag it): this checks for exactly one concrete internal sentinel type,
// not an arbitrary wrapped error chain.
func asJSONRPCError(err error, out **jsonrpcError) bool {
	//nolint:errorlint // see doc comment above: exact internal sentinel type, not a wrapped chain
	rpcErr, ok := err.(*jsonrpcError)
	if ok {
		*out = rpcErr
	}
	return ok
}

func (s *Server) callTool(ctx context.Context, p toolCallParams) (json.RawMessage, error) {
	switch p.Name {
	case toolBoardStatus:
		return s.callStatus(ctx)
	case toolBoardMessages:
		return s.callMessages(ctx, p.Arguments)
	case toolBoardBroadcast:
		return s.callBroadcast(ctx, p.Arguments)
	default:
		return nil, &jsonrpcError{Code: jsonrpcMethodNotFound, Message: "unknown tool: " + p.Name}
	}
}

func (s *Server) callStatus(ctx context.Context) (json.RawMessage, error) {
	raw, err := s.client.Status(ctx)
	if err != nil {
		return nil, fmt.Errorf("board_status: %w", err)
	}
	return raw, nil
}

func (s *Server) callMessages(ctx context.Context, rawArgs json.RawMessage) (json.RawMessage, error) {
	var args struct {
		Since int64 `json:"since"`
	}
	if len(rawArgs) > 0 {
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return nil, &jsonrpcError{Code: jsonrpcInvalidParams, Message: "invalid arguments: " + err.Error()}
		}
	}
	raw, err := s.client.Messages(ctx, args.Since)
	if err != nil {
		return nil, fmt.Errorf("board_messages: %w", err)
	}
	return raw, nil
}

func (s *Server) callBroadcast(ctx context.Context, rawArgs json.RawMessage) (json.RawMessage, error) {
	//nolint:govet // fieldalignment: local decode struct, padding cost is negligible
	var args struct {
		To   []string `json:"to"`
		Body string   `json:"body"`
	}
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return nil, &jsonrpcError{Code: jsonrpcInvalidParams, Message: "invalid arguments: " + err.Error()}
	}
	raw, err := s.client.Broadcast(ctx, args.To, args.Body)
	if err != nil {
		return nil, fmt.Errorf("board_broadcast: %w", err)
	}
	return raw, nil
}
