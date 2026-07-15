package team

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// searchClient is a minimal MCP streamable-HTTP client for a self-hosted search server; one instance and one handshake per persona run.
type searchClient struct {
	httpClient *http.Client
	retryDelay time.Duration
	url        string
	token      string
	sessionID  string
	ready      bool
}

func newSearchClient(client *http.Client, retryDelay time.Duration, url, token string) *searchClient {
	return &searchClient{httpClient: client, retryDelay: retryDelay, url: url, token: token}
}

func (s *searchClient) headers() map[string]string {
	h := map[string]string{"Accept": "application/json, text/event-stream"}
	if s.token != "" {
		h["Authorization"] = "Bearer " + s.token
	}
	if s.sessionID != "" {
		h["Mcp-Session-Id"] = s.sessionID
	}
	return h
}

// rpc posts one JSON-RPC message; id nil sends a notification.
func (s *searchClient) rpc(id any, method string, params any) ([]byte, http.Header, error) {
	msg := map[string]any{"jsonrpc": "2.0", "method": method}
	if id != nil {
		msg["id"] = id
	}
	if params != nil {
		msg["params"] = params
	}
	payload, err := json.Marshal(msg)
	if err != nil {
		return nil, nil, err
	}
	status, body, hdr, err := postJSON(s.httpClient, s.retryDelay, s.url, s.headers(), payload)
	if err != nil {
		return nil, nil, fmt.Errorf("search mcp: %w", err)
	}
	if status < 200 || status >= 300 {
		return nil, nil, fmt.Errorf("search mcp: HTTP %d: %s", status, truncateForError(body))
	}
	return body, hdr, nil
}

// jsonRPCResult extracts the result from a plain-JSON or SSE-framed body; streamable-HTTP servers may answer either way.
func jsonRPCResult(body []byte, contentType string) (json.RawMessage, error) {
	raw := body
	if strings.Contains(contentType, "text/event-stream") {
		raw = nil
		for line := range strings.SplitSeq(string(body), "\n") {
			if data, ok := strings.CutPrefix(line, "data:"); ok {
				raw = []byte(strings.TrimSpace(data))
				break
			}
		}
		if raw == nil {
			return nil, fmt.Errorf("empty SSE response")
		}
	}
	var parsed struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, err
	}
	if parsed.Error != nil {
		return nil, fmt.Errorf("%s", parsed.Error.Message)
	}
	return parsed.Result, nil
}

func (s *searchClient) ensureInitialized() error {
	if s.ready {
		return nil
	}
	body, hdr, err := s.rpc(1, "initialize", map[string]any{
		"protocolVersion": "2025-03-26",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "openspec-team", "version": "1"},
	})
	if err != nil {
		return err
	}
	if _, err := jsonRPCResult(body, hdr.Get("Content-Type")); err != nil {
		return fmt.Errorf("search mcp initialize: %w", err)
	}

	if sid := hdr.Get("Mcp-Session-Id"); sid != "" {
		s.sessionID = sid
	}
	if _, _, err := s.rpc(nil, "notifications/initialized", nil); err != nil {
		return err
	}
	s.ready = true
	return nil
}

func (s *searchClient) callTool(name string, args map[string]any) (string, error) {
	if err := s.ensureInitialized(); err != nil {
		return "", err
	}
	body, hdr, err := s.rpc(2, "tools/call", map[string]any{"name": name, "arguments": args})
	if err != nil {
		return "", err
	}
	result, err := jsonRPCResult(body, hdr.Get("Content-Type"))
	if err != nil {
		return "", fmt.Errorf("search mcp %s: %w", name, err)
	}
	var res struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(result, &res); err != nil {
		return "", fmt.Errorf("search mcp %s: invalid result: %w", name, err)
	}
	var parts []string
	for _, c := range res.Content {
		if c.Text != "" {
			parts = append(parts, c.Text)
		}
	}
	text := strings.Join(parts, "\n")
	if res.IsError {
		return "", fmt.Errorf("search mcp %s: %s", name, truncateForError([]byte(text)))
	}
	return text, nil
}
