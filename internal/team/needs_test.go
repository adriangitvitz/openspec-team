package team

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func needsFixture(t *testing.T, srvURL string) (RunnerOptions, string) {
	t.Helper()
	root := t.TempDir()
	changeDir := filepath.Join(root, "openspec", "changes", "demo")
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(changeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "EF.pdf"), []byte("%PDF"), 0o644); err != nil {
		t.Fatal(err)
	}
	opts := testOpts(srvURL, root)
	opts.ChangeName = "demo"
	opts.ChangeDir = changeDir
	opts.Persona = "product-owner"
	opts.Artifact = "research"
	opts.MaxExtractionRoundTrips = DefaultMaxExtractionRoundTrips
	return opts, changeDir
}

func extractionCall(id, path string) string {
	args := fmt.Sprintf(`{"path":%q,"detail":"field table 4.2","rationale":"exact lengths required"}`, path)
	return toolCallResponse(id, "request_extraction", args)
}

func TestExtractionRequestPausesRun(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, extractionCall("c1", "docs/EF.pdf"))
	}))
	defer srv.Close()
	opts, changeDir := needsFixture(t, srv.URL)

	_, err := RunOpenRouter(opts, "s", "u")
	var needs *ExtractionNeeded
	if !errors.As(err, &needs) {
		t.Fatalf("err = %v", err)
	}
	p := needs.Payload
	if p.ChangeName != "demo" || p.Persona != "product-owner" || p.Artifact != "research" ||
		p.RoundTrip != 1 || len(p.Requests) != 1 || p.Requests[0].Path != "docs/EF.pdf" {
		t.Fatalf("payload = %+v", p)
	}
	content, err := os.ReadFile(filepath.Join(changeDir, NeedsFileName))
	if err != nil {
		t.Fatal(err)
	}
	var nf needsFile
	json.Unmarshal(content, &nf)
	if nf.Counts["product-owner/research"] != 1 || len(nf.Pending["product-owner/research"]) != 1 {
		t.Fatalf("needs file = %+v", nf)
	}
}

func TestExtractionBatchedRequestsPauseOnce(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		args := `{"path":"docs/EF.pdf","detail":"d","rationale":"r"}`
		fmt.Fprintf(w, `{"choices":[{"message":{"role":"assistant","tool_calls":[
			{"id":"c1","type":"function","function":{"name":"request_extraction","arguments":%q}},
			{"id":"c2","type":"function","function":{"name":"request_extraction","arguments":%q}}]}}]}`, args, args)
	}))
	defer srv.Close()
	opts, changeDir := needsFixture(t, srv.URL)

	_, err := RunOpenRouter(opts, "s", "u")
	var needs *ExtractionNeeded
	if !errors.As(err, &needs) || len(needs.Payload.Requests) != 2 || needs.Payload.RoundTrip != 1 {
		t.Fatalf("err=%v payload=%+v", err, needs)
	}
	if got, _ := extractionRoundTrips(changeDir, "product-owner", "research"); got != 1 {
		t.Fatalf("round trips = %d", got)
	}
}

func TestExtractionCapDegradesInRun(t *testing.T) {
	call := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call++
		if call == 1 {
			fmt.Fprint(w, extractionCall("c1", "docs/EF.pdf"))
			return
		}
		fmt.Fprint(w, finalResponse("answered with existing evidence"))
	}))
	defer srv.Close()
	opts, changeDir := needsFixture(t, srv.URL)

	if _, err := recordPause(changeDir, "product-owner", "research", []ExtractionRequest{{Path: "docs/EF.pdf", Detail: "d", Rationale: "r"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := recordPause(changeDir, "product-owner", "research", []ExtractionRequest{{Path: "docs/EF.pdf", Detail: "d", Rationale: "r"}}); err != nil {
		t.Fatal(err)
	}

	out, err := RunOpenRouter(opts, "s", "u")
	if err != nil || out != "answered with existing evidence" {
		t.Fatalf("out=%q err=%v", out, err)
	}
	if got, _ := extractionRoundTrips(changeDir, "product-owner", "research"); got != 2 {
		t.Fatalf("cap incremented past limit: %d", got)
	}
}

func TestExtractionMalformedRejectedInRun(t *testing.T) {
	variants := []string{
		`{"path":"docs/ghost.pdf","detail":"d","rationale":"r"}`,
		`{"path":"README.md","detail":"d","rationale":"r"}`,
		`{"path":"docs/EF.pdf","detail":"","rationale":"r"}`,
		`{"path":"docs/EF.pdf","detail":"d","rationale":""}`,
	}
	for i, args := range variants {
		call := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			call++
			if call == 1 {
				fmt.Fprint(w, toolCallResponse("c1", "request_extraction", args))
				return
			}
			fmt.Fprint(w, finalResponse("continued"))
		}))
		opts, changeDir := needsFixture(t, srv.URL)
		if _, err := os.Stat(filepath.Join(opts.Root, "README.md")); os.IsNotExist(err) {
			os.WriteFile(filepath.Join(opts.Root, "README.md"), []byte("readme"), 0o644)
		}

		out, err := RunOpenRouter(opts, "s", "u")
		if err != nil || out != "continued" {
			t.Fatalf("variant %d: out=%q err=%v", i, out, err)
		}
		if got, _ := extractionRoundTrips(changeDir, "product-owner", "research"); got != 0 {
			t.Fatalf("variant %d: malformed request incremented round trips", i)
		}
		srv.Close()
	}
}

func TestExtractionSuccessfulRunClearsPending(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, finalResponse("done"))
	}))
	defer srv.Close()
	opts, changeDir := needsFixture(t, srv.URL)

	if _, err := recordPause(changeDir, "product-owner", "research", []ExtractionRequest{{Path: "docs/EF.pdf", Detail: "d", Rationale: "r"}}); err != nil {
		t.Fatal(err)
	}

	if out, err := RunOpenRouter(opts, "s", "u"); err != nil || out != "done" {
		t.Fatalf("out=%q err=%v", out, err)
	}
	if err := ClearPendingExtractions(changeDir, "product-owner", "research"); err != nil {
		t.Fatal(err)
	}
	nf, err := loadNeeds(changeDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(nf.Pending["product-owner/research"]) != 0 {
		t.Fatalf("pending not cleared: %+v", nf.Pending)
	}
	if nf.Counts["product-owner/research"] != 1 {
		t.Fatalf("counts not retained: %+v", nf.Counts)
	}
}

func TestIntegrationContractDeterministic(t *testing.T) {
	a, err := json.MarshalIndent(IntegrationContract(), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.MarshalIndent(IntegrationContract(), "", "  ")
	if string(a) != string(b) {
		t.Fatal("contract output is not deterministic")
	}
	out := string(a)
	for _, needle := range []string{
		`"exitCode": 7`,
		`"payloadExample"`,
		"extraction-needs.json",
		"request_extraction",
		"web_search",
		"--max-extraction-roundtrips",
		"team.search.mcp_url",
		"needs extraction",
	} {
		if !strings.Contains(out, needle) {
			t.Errorf("contract missing %q", needle)
		}
	}
	c := IntegrationContract()
	if len(c.Tools) != 6 {
		t.Fatalf("tools = %d", len(c.Tools))
	}
	for _, tool := range c.Tools {
		if tool.Precondition == "" || len(tool.Schema) == 0 {
			t.Fatalf("tool %s missing precondition or schema", tool.Name)
		}
	}
}

func TestExtractionPauseFulfillRerunEndToEnd(t *testing.T) {

	phase := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		phase++
		if phase == 1 {
			fmt.Fprint(w, extractionCall("c1", "docs/EF.pdf"))
			return
		}

		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), "Fulfilled section 4.2 content") {
			t.Errorf("re-run prompt missing the fulfilled extraction")
		}
		fmt.Fprint(w, finalResponse("built on the extraction"))
	}))
	defer srv.Close()
	opts, changeDir := needsFixture(t, srv.URL)

	_, err := RunOpenRouter(opts, "s", "cite `docs/EF.pdf`")
	var needs *ExtractionNeeded
	if !errors.As(err, &needs) {
		t.Fatalf("expected pause, got %v", err)
	}

	sibling := filepath.Join(opts.Root, "docs", "EF.pdf.md")
	if err := os.WriteFile(sibling, []byte("<!-- extraction of: docs/EF.pdf -->\n\nFulfilled section 4.2 content\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := RunOpenRouter(opts, "s", "cite `docs/EF.pdf` — extraction: Fulfilled section 4.2 content")
	if err != nil || out != "built on the extraction" {
		t.Fatalf("out=%q err=%v", out, err)
	}
	if err := ClearPendingExtractions(changeDir, "product-owner", "research"); err != nil {
		t.Fatal(err)
	}
	nf, err := loadNeeds(changeDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(nf.Pending["product-owner/research"]) != 0 || nf.Counts["product-owner/research"] != 1 {
		t.Fatalf("needs state after fulfillment = %+v", nf)
	}
}

func TestExtractionCapZeroForbidsPauses(t *testing.T) {
	call := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call++
		if call == 1 {
			fmt.Fprint(w, extractionCall("c1", "docs/EF.pdf"))
			return
		}
		fmt.Fprint(w, finalResponse("no pause allowed"))
	}))
	defer srv.Close()
	opts, changeDir := needsFixture(t, srv.URL)
	opts.MaxExtractionRoundTrips = 0

	out, err := RunOpenRouter(opts, "s", "u")
	if err != nil || out != "no pause allowed" {
		t.Fatalf("out=%q err=%v", out, err)
	}
	if got, _ := extractionRoundTrips(changeDir, "product-owner", "research"); got != 0 {
		t.Fatalf("round trips = %d", got)
	}
}

func TestNeedsConcurrentRecordPause(t *testing.T) {
	changeDir := t.TempDir()
	const workers = 8
	done := make(chan error, workers)
	for i := 0; i < workers; i++ {
		go func(n int) {
			persona := fmt.Sprintf("persona-%d", n)
			_, err := recordPause(changeDir, persona, "research", []ExtractionRequest{{Path: "docs/EF.pdf", Detail: "d", Rationale: "r"}})
			done <- err
		}(i)
	}
	for i := 0; i < workers; i++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	nf, err := loadNeeds(changeDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(nf.Counts) != workers || len(nf.Pending) != workers {
		t.Fatalf("lost updates: counts=%d pending=%d want %d", len(nf.Counts), len(nf.Pending), workers)
	}
}

func TestNeedsCorruptFileFailsClosed(t *testing.T) {
	changeDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(changeDir, NeedsFileName), []byte("<<<<<<< merge conflict"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := extractionRoundTrips(changeDir, "p", "a"); err == nil || !strings.Contains(err.Error(), "corrupt") {
		t.Fatalf("err = %v", err)
	}
	if _, err := recordPause(changeDir, "p", "a", []ExtractionRequest{{Path: "x.pdf", Detail: "d", Rationale: "r"}}); err == nil {
		t.Fatal("recordPause must fail closed on a corrupt needs file")
	}

	e := &toolExecutor{root: t.TempDir(), changeDir: changeDir, persona: "p", artifact: "a", maxRoundTrips: 2}
	pdf := filepath.Join(e.root, "doc.pdf")
	os.WriteFile(pdf, []byte("%PDF"), 0o644)
	out := e.execute("request_extraction", `{"path":"doc.pdf","detail":"d","rationale":"r"}`)
	if !strings.Contains(out, "corrupt") || len(e.pending) != 0 {
		t.Fatalf("out=%q pending=%d", out, len(e.pending))
	}
}

func TestNeedsFileRemovedWhenEmpty(t *testing.T) {
	changeDir := t.TempDir()
	if err := saveNeeds(changeDir, needsFile{Counts: map[string]int{}, Pending: map[string][]ExtractionRequest{}}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(changeDir, NeedsFileName)); !os.IsNotExist(err) {
		t.Fatal("empty needs file not removed")
	}
}

func TestIntegrationContractGolden(t *testing.T) {
	got, err := json.MarshalIndent(IntegrationContract(), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	golden := filepath.Join("testdata", "contract.golden")
	if os.Getenv("UPDATE_GOLDEN") != "" {
		if err := os.WriteFile(golden, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatal("contract drifted from testdata/contract.golden; if intentional, regenerate with UPDATE_GOLDEN=1")
	}
}
