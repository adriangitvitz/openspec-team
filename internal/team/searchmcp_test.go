package team

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

type mcpServer struct {
	t           *testing.T
	sessionID   string
	sse         bool
	requests    []string
	gotAuth     []string
	gotAccept   []string
	gotSessions []string
	searchText  string
	extractText string
}

func (m *mcpServer) handler(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	var msg struct {
		Method string `json:"method"`
		Params struct {
			ProtocolVersion string         `json:"protocolVersion"`
			Name            string         `json:"name"`
			Arguments       map[string]any `json:"arguments"`
		} `json:"params"`
	}
	json.Unmarshal(body, &msg)
	m.requests = append(m.requests, msg.Method)
	m.gotAuth = append(m.gotAuth, r.Header.Get("Authorization"))
	m.gotAccept = append(m.gotAccept, r.Header.Get("Accept"))
	if msg.Method != "initialize" {
		m.gotSessions = append(m.gotSessions, r.Header.Get("Mcp-Session-Id"))
	}

	respond := func(result string) {
		payload := fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"result":%s}`, result)
		if m.sse {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, "event: message\ndata: %s\n\n", payload)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, payload)
	}

	switch msg.Method {
	case "initialize":
		if msg.Params.ProtocolVersion == "" {
			m.t.Error("initialize missing protocolVersion")
		}
		if m.sessionID != "" {
			w.Header().Set("Mcp-Session-Id", m.sessionID)
		}
		respond(`{"protocolVersion":"2025-03-26","capabilities":{},"serverInfo":{"name":"mock"}}`)
	case "notifications/initialized":
		w.WriteHeader(http.StatusAccepted)
	case "tools/call":
		text := m.searchText
		if msg.Params.Name == "extract_content" {
			text = m.extractText
		}
		respond(fmt.Sprintf(`{"content":[{"type":"text","text":%q}],"isError":false}`, text))
	default:
		m.t.Errorf("unexpected method %q", msg.Method)
	}
}

func newSearchClientForTest(t *testing.T, m *mcpServer) (*searchClient, *httptest.Server) {
	t.Helper()
	m.t = t
	srv := httptest.NewServer(http.HandlerFunc(m.handler))
	t.Cleanup(srv.Close)
	return newSearchClient(&http.Client{Timeout: 5 * time.Second}, time.Millisecond, srv.URL, "sk-search"), srv
}

func TestSearchClientLifecycle(t *testing.T) {
	m := &mcpServer{sessionID: "sess-42", searchText: "ranked results"}
	c, _ := newSearchClientForTest(t, m)

	out, err := c.callTool("web_search", map[string]any{"query": "url shorteners"})
	if err != nil || out != "ranked results" {
		t.Fatalf("out=%q err=%v", out, err)
	}

	if want := []string{"initialize", "notifications/initialized", "tools/call"}; !equalStrings(m.requests, want) {
		t.Fatalf("requests = %v", m.requests)
	}

	for _, sid := range m.gotSessions {
		if sid != "sess-42" {
			t.Fatalf("sessions = %v", m.gotSessions)
		}
	}
	for i := range m.requests {
		if m.gotAuth[i] != "Bearer sk-search" {
			t.Fatalf("auth[%d] = %q", i, m.gotAuth[i])
		}
		if !strings.Contains(m.gotAccept[i], "text/event-stream") || !strings.Contains(m.gotAccept[i], "application/json") {
			t.Fatalf("accept[%d] = %q", i, m.gotAccept[i])
		}
	}

	if _, err := c.callTool("web_search", map[string]any{"query": "again"}); err != nil {
		t.Fatal(err)
	}
	if want := []string{"initialize", "notifications/initialized", "tools/call", "tools/call"}; !equalStrings(m.requests, want) {
		t.Fatalf("requests = %v", m.requests)
	}
}

func TestSearchClientSSEFramedResponses(t *testing.T) {
	m := &mcpServer{sse: true, extractText: "extracted page body"}
	c, _ := newSearchClientForTest(t, m)
	out, err := c.callTool("extract_content", map[string]any{"url": "https://example.com"})
	if err != nil || out != "extracted page body" {
		t.Fatalf("out=%q err=%v", out, err)
	}
}

func TestSearchClientServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusBadGateway)
	}))
	defer srv.Close()
	c := newSearchClient(&http.Client{Timeout: time.Second}, time.Millisecond, srv.URL, "sk-secret")
	_, err := c.callTool("web_search", map[string]any{"query": "x"})
	if err == nil || !strings.Contains(err.Error(), "search mcp") {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), "sk-secret") {
		t.Fatal("token leaked into error")
	}
}

func TestSearchClientProtocolGarbage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "this is not json-rpc")
	}))
	defer srv.Close()
	c := newSearchClient(&http.Client{Timeout: time.Second}, time.Millisecond, srv.URL, "")
	if _, err := c.callTool("web_search", map[string]any{"query": "x"}); err == nil {
		t.Fatal("expected protocol error")
	}
}

func TestExecutorRoutesSearchTools(t *testing.T) {
	m := &mcpServer{searchText: strings.Repeat("r", toolReadCap+50)}
	c, _ := newSearchClientForTest(t, m)
	e := &toolExecutor{root: t.TempDir(), search: c}

	out := e.execute("web_search", `{"query":"shorteners"}`)
	if !strings.HasPrefix(out, untrustedNote) {
		t.Fatalf("missing untrusted note: %q", out[:80])
	}
	if !strings.HasSuffix(out, "[truncated]") || len(out) > len(untrustedNote)+toolReadCap+len("\n[truncated]") {
		t.Fatalf("size cap not applied: len=%d", len(out))
	}

	if got := e.execute("list_dir", `{"path":""}`); strings.Contains(got, "error") {
		t.Fatalf("repo tool broken: %q", got)
	}

	bad := &toolExecutor{root: t.TempDir(), search: newSearchClient(&http.Client{Timeout: 100 * time.Millisecond}, time.Millisecond, "http://127.0.0.1:1", "tok")}
	if got := bad.execute("web_search", `{"query":"x"}`); !strings.HasPrefix(got, "error:") || strings.Contains(got, "tok") {
		t.Fatalf("degraded result = %q", got)
	}
}

func TestExecutorWithoutSearchRejectsSearchTools(t *testing.T) {
	e := &toolExecutor{root: t.TempDir()}
	if out := e.execute("web_search", `{"query":"x"}`); !strings.Contains(out, `unknown tool "web_search"`) {
		t.Fatalf("out = %q", out)
	}
}

func TestToolSchemaAdvertisement(t *testing.T) {
	countNames := func(raw json.RawMessage) []string {
		var defs []struct {
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		}
		if err := json.Unmarshal(raw, &defs); err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, d := range defs {
			names = append(names, d.Function.Name)
		}
		return names
	}
	if names := countNames(toolSchemasFor(false)); !equalStrings(names, []string{"read_file", "grep", "list_dir", "request_extraction"}) {
		t.Fatalf("unconfigured schema = %v", names)
	}
	if names := countNames(toolSchemasFor(true)); !equalStrings(names, []string{"read_file", "grep", "list_dir", "web_search", "fetch_page", "request_extraction"}) {
		t.Fatalf("configured schema = %v", names)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestSearchClientLiveSmoke(t *testing.T) {
	url := os.Getenv("OPENSPEC_SEARCH_LIVE_URL")
	if url == "" {
		t.Skip("set OPENSPEC_SEARCH_LIVE_URL to run the live smoke")
	}
	c := newSearchClient(&http.Client{Timeout: 30 * time.Second}, 500*time.Millisecond, url, os.Getenv("OPENSPEC_SEARCH_TOKEN"))
	results, err := c.callTool("web_search", map[string]any{"query": "self-hosted URL shortener"})
	if err != nil || results == "" {
		t.Fatalf("web_search: err=%v len=%d", err, len(results))
	}
	t.Logf("web_search returned %d bytes", len(results))
	page, err := c.callTool("extract_content", map[string]any{"url": "https://go.dev/doc/"})
	if err != nil || page == "" {
		t.Fatalf("extract_content: err=%v len=%d", err, len(page))
	}
	t.Logf("extract_content returned %d bytes", len(page))
}
