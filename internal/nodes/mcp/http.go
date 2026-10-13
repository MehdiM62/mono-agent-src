package mcpnode

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"
)

// httpTransport speaks MCP Streamable HTTP (2025-06-18): every message is a
// POST of one JSON-RPC message; the server answers a request with either a
// JSON body or an SSE stream that carries the response.
type httpTransport struct {
	client          *http.Client
	url             string
	headers         map[string]string
	sessionID       string
	protocolVersion string // set after initialize
}

func newHTTPTransport(url string, headers map[string]string) *httpTransport {
	t := &httpTransport{url: url, headers: headers}
	t.client = &http.Client{
		// A configured header (an API key, say) never follows a redirect
		// to another host; net/http already drops Authorization and Cookie.
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("stopped after 10 redirects")
			}
			if req.URL.Host != via[0].URL.Host {
				for k := range t.headers {
					req.Header.Del(k)
				}
			}
			return nil
		},
	}
	return t
}

func (t *httpTransport) newRequest(ctx context.Context, method string, body []byte) (*http.Request, error) {
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, t.url, r)
	if err != nil {
		return nil, err
	}
	for k, v := range t.headers {
		req.Header.Set(k, v)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json, text/event-stream")
	if t.sessionID != "" {
		req.Header.Set("Mcp-Session-Id", t.sessionID)
	}
	if t.protocolVersion != "" {
		req.Header.Set("MCP-Protocol-Version", t.protocolVersion)
	}
	return req, nil
}

func (t *httpTransport) post(ctx context.Context, body []byte) (*http.Response, error) {
	req, err := t.newRequest(ctx, http.MethodPost, body)
	if err != nil {
		return nil, err
	}
	resp, err := t.client.Do(req)
	if err != nil {
		return nil, err
	}
	if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
		t.sessionID = sid
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		defer resp.Body.Close()
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		msg := fmt.Sprintf("MCP server answered HTTP %d", resp.StatusCode)
		if resp.StatusCode == http.StatusNotFound && t.sessionID != "" {
			msg += " (session expired)"
		}
		if s := strings.TrimSpace(string(snippet)); s != "" {
			msg += ": " + s
		}
		return nil, errors.New(msg)
	}
	return resp, nil
}

func (t *httpTransport) request(ctx context.Context, id string, msg []byte) (*rpcMessage, error) {
	resp, err := t.post(ctx, msg)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusAccepted {
		return nil, errors.New("MCP server accepted the request but sent no response")
	}
	ct, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if ct == "text/event-stream" {
		return t.readSSE(ctx, resp.Body, id)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxMessageBytes+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxMessageBytes {
		return nil, fmt.Errorf("MCP server sent a response over %d MB", maxMessageBytes>>20)
	}
	return findResponse(b, id)
}

// findResponse picks the response with id out of a JSON body holding one
// message or a batch.
func findResponse(b []byte, id string) (*rpcMessage, error) {
	b = bytes.TrimSpace(b)
	var msgs []rpcMessage
	if len(b) > 0 && b[0] == '[' {
		if err := json.Unmarshal(b, &msgs); err != nil {
			return nil, fmt.Errorf("decode MCP response: %w", err)
		}
	} else {
		var m rpcMessage
		if err := json.Unmarshal(b, &m); err != nil {
			return nil, fmt.Errorf("decode MCP response: %w", err)
		}
		msgs = []rpcMessage{m}
	}
	for i := range msgs {
		if msgs[i].isResponse() && idKey(msgs[i].ID) == id {
			return &msgs[i], nil
		}
	}
	return nil, errors.New("MCP server's reply holds no response to the request")
}

// readSSE reads events until the one carrying the response to id. Server
// requests on the stream (ping) are answered with a separate POST.
func (t *httpTransport) readSSE(ctx context.Context, body io.Reader, id string) (*rpcMessage, error) {
	sc := bufio.NewScanner(body)
	sc.Buffer(make([]byte, 0, 64<<10), maxMessageBytes)
	var data strings.Builder
	flush := func() (*rpcMessage, bool) {
		if data.Len() == 0 {
			return nil, false
		}
		payload := data.String()
		data.Reset()
		var m rpcMessage
		if json.Unmarshal([]byte(payload), &m) != nil {
			return nil, false
		}
		switch {
		case m.isResponse() && idKey(m.ID) == id:
			return &m, true
		case m.isRequest():
			_ = t.notify(ctx, replyToServer(&m))
		}
		return nil, false
	}
	for sc.Scan() {
		line := strings.TrimSuffix(sc.Text(), "\r")
		if line == "" {
			if m, ok := flush(); ok {
				return m, nil
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue // comment / keep-alive
		}
		field, value, _ := strings.Cut(line, ":")
		value = strings.TrimPrefix(value, " ")
		if field == "data" {
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(value)
		}
	}
	if m, ok := flush(); ok {
		return m, nil
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, errors.New("MCP server's event stream ended without a response")
}

func (t *httpTransport) notify(ctx context.Context, msg []byte) error {
	resp, err := t.post(ctx, msg)
	if err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	resp.Body.Close()
	return nil
}

// close ends the session with a DELETE when the server issued one.
func (t *httpTransport) close() {
	if t.sessionID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := t.newRequest(ctx, http.MethodDelete, nil)
	if err != nil {
		return
	}
	if resp, err := t.client.Do(req); err == nil {
		resp.Body.Close()
	}
	t.client.CloseIdleConnections()
}
