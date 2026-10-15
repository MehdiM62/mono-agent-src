package mcpnode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime/debug"
	"strconv"
	"sync/atomic"
	"time"
)

// ProtocolVersion is the MCP revision this client speaks.
const ProtocolVersion = "2025-06-18"

// maxMessageBytes bounds one JSON-RPC message read from a server, like the
// HTTP request node's default response cap.
const maxMessageBytes = 64 << 20

// rpcMessage is any JSON-RPC 2.0 message: a request (method + id), a
// notification (method, no id) or a response (id + result or error).
type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

func (m *rpcMessage) isRequest() bool  { return m.Method != "" && len(m.ID) > 0 }
func (m *rpcMessage) isResponse() bool { return m.Method == "" && len(m.ID) > 0 }

type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *rpcError) Error() string { return fmt.Sprintf("JSON-RPC error %d: %s", e.Code, e.Message) }

// outgoing is a message this client sends.
type outgoing struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id,omitempty"`
	Method  string      `json:"method,omitempty"`
	Params  interface{} `json:"params,omitempty"`
	Result  interface{} `json:"result,omitempty"`
	Error   *rpcError   `json:"error,omitempty"`
}

// transport carries JSON-RPC messages to one MCP server.
type transport interface {
	// request sends a request whose id is id and returns its response.
	request(ctx context.Context, id string, msg []byte) (*rpcMessage, error)
	// notify sends a notification (no response expected).
	notify(ctx context.Context, msg []byte) error
	// close ends the session and releases everything it holds.
	close()
}

// replyToServer answers a request the server sent to this client. Only
// ping is supported; anything else gets "method not found".
func replyToServer(m *rpcMessage) []byte {
	reply := outgoing{JSONRPC: "2.0", ID: m.ID}
	if m.Method == "ping" {
		reply.Result = map[string]interface{}{}
	} else {
		reply.Error = &rpcError{Code: -32601, Message: "method not supported by this client: " + m.Method}
	}
	b, _ := json.Marshal(reply)
	return b
}

// client is an MCP client session over a transport.
type client struct {
	t       transport
	timeout time.Duration
	nextID  atomic.Int64
}

func clientVersion() string {
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return "dev"
}

// call sends method with params and decodes the result into out.
func (c *client) call(ctx context.Context, method string, params, out interface{}) error {
	id := c.nextID.Add(1)
	b, err := json.Marshal(outgoing{JSONRPC: "2.0", ID: id, Method: method, Params: params})
	if err != nil {
		return err
	}
	if c.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.timeout)
		defer cancel()
	}
	resp, err := c.t.request(ctx, strconv.FormatInt(id, 10), b)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) && c.timeout > 0 {
			return fmt.Errorf("%s: no response within %s", method, c.timeout)
		}
		return fmt.Errorf("%s: %w", method, err)
	}
	if resp.Error != nil {
		return fmt.Errorf("%s: %w", method, resp.Error)
	}
	if out != nil && len(resp.Result) > 0 {
		if err := json.Unmarshal(resp.Result, out); err != nil {
			return fmt.Errorf("%s: decode result: %w", method, err)
		}
	}
	return nil
}

// initialize runs the MCP handshake: initialize, then
// notifications/initialized.
func (c *client) initialize(ctx context.Context) error {
	var res struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	err := c.call(ctx, "initialize", map[string]interface{}{
		"protocolVersion": ProtocolVersion,
		"capabilities":    map[string]interface{}{},
		"clientInfo":      map[string]interface{}{"name": "monoagent", "version": clientVersion()},
	}, &res)
	if err != nil {
		return err
	}
	if h, ok := c.t.(*httpTransport); ok && res.ProtocolVersion != "" {
		h.protocolVersion = res.ProtocolVersion
	}
	b, _ := json.Marshal(outgoing{JSONRPC: "2.0", Method: "notifications/initialized"})
	nctx := ctx
	if c.timeout > 0 {
		var cancel context.CancelFunc
		nctx, cancel = context.WithTimeout(ctx, c.timeout)
		defer cancel()
	}
	if err := c.t.notify(nctx, b); err != nil {
		return fmt.Errorf("notifications/initialized: %w", err)
	}
	return nil
}

// listTools returns every tool the server offers, following pagination.
func (c *client) listTools(ctx context.Context) ([]map[string]interface{}, error) {
	var all []map[string]interface{}
	cursor := ""
	for page := 0; page < 1000; page++ {
		params := map[string]interface{}{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		var res struct {
			Tools      []map[string]interface{} `json:"tools"`
			NextCursor string                   `json:"nextCursor"`
		}
		if err := c.call(ctx, "tools/list", params, &res); err != nil {
			return nil, err
		}
		all = append(all, res.Tools...)
		if res.NextCursor == "" || res.NextCursor == cursor {
			break
		}
		cursor = res.NextCursor
	}
	return all, nil
}

// toolResult is a tools/call result.
type toolResult struct {
	Content           []map[string]interface{} `json:"content"`
	StructuredContent interface{}              `json:"structuredContent"`
	IsError           bool                     `json:"isError"`
}

func (c *client) callTool(ctx context.Context, name string, args map[string]interface{}) (*toolResult, error) {
	if args == nil {
		args = map[string]interface{}{}
	}
	var res toolResult
	if err := c.call(ctx, "tools/call", map[string]interface{}{"name": name, "arguments": args}, &res); err != nil {
		return nil, err
	}
	return &res, nil
}
