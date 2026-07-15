package team

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testOpts(url, root string) RunnerOptions {
	return RunnerOptions{
		Model:      "test/model",
		APIKey:     "sk-test",
		Root:       root,
		BaseURL:    url,
		RetryDelay: time.Millisecond,
		Timeout:    5 * time.Second,
	}
}

func finalResponse(content string) string {
	return fmt.Sprintf(`{"choices":[{"message":{"role":"assistant","content":%q}}]}`, content)
}

func toolCallResponse(id, name, args string) string {
	return fmt.Sprintf(`{"choices":[{"message":{"role":"assistant","tool_calls":[
		{"id":%q,"type":"function","function":{"name":%q,"arguments":%q}}]}}]}`, id, name, args)
}

func TestRunOpenRouterSuccess(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		fmt.Fprint(w, finalResponse("the answer"))
	}))
	defer srv.Close()

	out, err := RunOpenRouter(testOpts(srv.URL, t.TempDir()), "system", "user")
	if err != nil || out != "the answer" {
		t.Fatalf("out=%q err=%v", out, err)
	}
	if gotAuth != "Bearer sk-test" {
		t.Fatalf("auth = %q", gotAuth)
	}
}

func TestRunOpenRouterRetriesTransientFailures(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			http.Error(w, "upstream sad", http.StatusInternalServerError)
			return
		}
		fmt.Fprint(w, finalResponse("recovered"))
	}))
	defer srv.Close()

	out, err := RunOpenRouter(testOpts(srv.URL, t.TempDir()), "s", "u")
	if err != nil || out != "recovered" || attempts != 2 {
		t.Fatalf("out=%q attempts=%d err=%v", out, attempts, err)
	}
}

func TestRunOpenRouterGivesUpAfterRetries(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		http.Error(w, "always down", http.StatusBadGateway)
	}))
	defer srv.Close()

	_, err := RunOpenRouter(testOpts(srv.URL, t.TempDir()), "s", "u")
	if err == nil || !strings.Contains(err.Error(), "giving up after 3 attempts") || attempts != 3 {
		t.Fatalf("attempts=%d err=%v", attempts, err)
	}
}

func TestRunOpenRouterNonTransientFailsFast(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		http.Error(w, "bad key", http.StatusUnauthorized)
	}))
	defer srv.Close()

	_, err := RunOpenRouter(testOpts(srv.URL, t.TempDir()), "s", "u")
	if err == nil || !strings.Contains(err.Error(), "HTTP 401") || attempts != 1 {
		t.Fatalf("attempts=%d err=%v", attempts, err)
	}
}

func TestRunOpenRouterMissingModelOrKey(t *testing.T) {
	if _, err := RunOpenRouter(RunnerOptions{APIKey: "k"}, "s", "u"); err == nil || !strings.Contains(err.Error(), "requires a model") {
		t.Fatalf("err = %v", err)
	}
	if _, err := RunOpenRouter(RunnerOptions{Model: "m"}, "s", "u"); err == nil || !strings.Contains(err.Error(), "OPENROUTER_API_KEY") {
		t.Fatalf("err = %v", err)
	}
}

func TestRunOpenRouterToolLoopReadsFile(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "notes.md"), []byte("secret plans"), 0o644); err != nil {
		t.Fatal(err)
	}

	call := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call++
		body, _ := io.ReadAll(r.Body)
		switch call {
		case 1:
			fmt.Fprint(w, toolCallResponse("c1", "read_file", `{"path":"notes.md"}`))
		case 2:
			var req map[string]any
			json.Unmarshal(body, &req)
			msgs := req["messages"].([]any)
			last := msgs[len(msgs)-1].(map[string]any)
			if last["role"] != "tool" || !strings.Contains(last["content"].(string), "secret plans") {
				t.Errorf("tool result not fed back: %+v", last)
			}
			fmt.Fprint(w, finalResponse("done reading"))
		}
	}))
	defer srv.Close()

	out, err := RunOpenRouter(testOpts(srv.URL, root), "s", "u")
	if err != nil || out != "done reading" || call != 2 {
		t.Fatalf("out=%q call=%d err=%v", out, call, err)
	}
}

func TestRunOpenRouterToolPathEscapeRejected(t *testing.T) {
	call := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call++
		body, _ := io.ReadAll(r.Body)
		switch call {
		case 1:
			fmt.Fprint(w, toolCallResponse("c1", "read_file", `{"path":"../../etc/passwd"}`))
		case 2:
			var req map[string]any
			json.Unmarshal(body, &req)
			msgs := req["messages"].([]any)
			last := msgs[len(msgs)-1].(map[string]any)
			content := last["content"].(string)
			if !strings.Contains(content, "error") || !strings.Contains(content, "escapes the project root") {
				t.Errorf("escape not rejected: %q", content)
			}
			fmt.Fprint(w, finalResponse("blocked"))
		}
	}))
	defer srv.Close()

	if out, err := RunOpenRouter(testOpts(srv.URL, t.TempDir()), "s", "u"); err != nil || out != "blocked" {
		t.Fatalf("out=%q err=%v", out, err)
	}
}

func TestRunOpenRouterIterationBound(t *testing.T) {
	call := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call++
		body, _ := io.ReadAll(r.Body)
		var req map[string]any
		json.Unmarshal(body, &req)
		if _, hasTools := req["tools"]; !hasTools {

			msgs := req["messages"].([]any)
			last := msgs[len(msgs)-1].(map[string]any)
			if !strings.Contains(last["content"].(string), "Tool budget exhausted") {
				t.Errorf("missing forced-answer instruction: %+v", last)
			}
			fmt.Fprint(w, finalResponse("forced answer"))
			return
		}
		fmt.Fprint(w, toolCallResponse(fmt.Sprintf("c%d", call), "list_dir", `{"path":""}`))
	}))
	defer srv.Close()

	opts := testOpts(srv.URL, t.TempDir())
	opts.MaxToolIterations = 2
	out, err := RunOpenRouter(opts, "s", "u")
	if err != nil || out != "forced answer" {
		t.Fatalf("out=%q err=%v", out, err)
	}

	if call != 3 {
		t.Fatalf("calls = %d", call)
	}
}

func TestExecuteToolGrepAndListDir(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "a.go"), []byte("package a\n// findme here\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	grep := executeTool(root, nil, "grep", `{"pattern":"findme"}`)
	if !strings.Contains(grep, "src/a.go:2:") {
		t.Fatalf("grep = %q", grep)
	}
	if out := executeTool(root, nil, "grep", `{"pattern":"nothing-matches-this"}`); out != "no matches" {
		t.Fatalf("grep empty = %q", out)
	}
	list := executeTool(root, nil, "list_dir", `{"path":""}`)
	if !strings.Contains(list, "src/") {
		t.Fatalf("list = %q", list)
	}
	if out := executeTool(root, nil, "list_dir", `{"path":"../.."}`); !strings.Contains(out, "error") {
		t.Fatalf("escape = %q", out)
	}
	if out := executeTool(root, nil, "nuke", `{}`); !strings.Contains(out, `unknown tool "nuke"`) {
		t.Fatalf("unknown tool = %q", out)
	}
}

func TestRunOpenRouterTimeout(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		time.Sleep(200 * time.Millisecond)
		fmt.Fprint(w, finalResponse("too late"))
	}))
	defer srv.Close()

	opts := testOpts(srv.URL, t.TempDir())
	opts.Timeout = 20 * time.Millisecond
	_, err := RunOpenRouter(opts, "s", "u")
	if err == nil || !strings.Contains(err.Error(), "giving up after 3 attempts") || attempts != 3 {
		t.Fatalf("attempts=%d err=%v", attempts, err)
	}
}

func TestExecuteToolSymlinkEscapeRejected(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("OUTSIDE"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "link.txt")); err != nil {
		t.Fatal(err)
	}
	out := executeTool(root, nil, "read_file", `{"path":"link.txt"}`)
	if !strings.Contains(out, "error") || strings.Contains(out, "OUTSIDE") {
		t.Fatalf("symlink escape not rejected: %q", out)
	}
}

func TestExecuteToolSkipDirsExcluded(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".git", "config"), []byte("[remote]"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, tool := range []string{"read_file", "list_dir"} {
		out := executeTool(root, nil, tool, `{"path":".git/config"}`)
		if !strings.Contains(out, "excluded") {
			t.Fatalf("%s on .git not excluded: %q", tool, out)
		}
	}
}
