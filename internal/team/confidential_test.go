package team

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adriangitvitz/openspec-go/internal/change"
)

func confidentialFixture(t *testing.T) *change.Context {
	t.Helper()
	root := t.TempDir()
	writeFile(t, root, "openspec/config.yaml", `schema: team-driven
team:
  personas:
    senior-staff:
      runner: openrouter
      model: test/model
  confidential:
    - INSTRUCTIONS.md
    - "secrets/**"
    - docs/EF.pdf
`)
	writeFile(t, root, "INSTRUCTIONS.md", "commercial figure: 42 pesos per unit")
	writeFile(t, root, "secrets/deep/keys.env", "API_KEY=xyz")
	writeFile(t, root, "internal/util/helper.go", "package util\n\nfunc Help() {}\n")
	writeFile(t, root, "docs/EF.pdf", "%PDF-1.4 raw binary bytes")
	writeFile(t, root, "docs/EF.pdf.md", "## Section 4.2\nExtracted pricing table.\n")
	writeFile(t, root, "openspec/changes/conf/.openspec.yaml", "schema: team-driven\n")
	writeFile(t, root, "openspec/changes/conf/research.md", "# R\nOK.\n")
	writeFile(t, root, "openspec/changes/conf/proposal.md",
		"# P\n\nRules live in `INSTRUCTIONS.md`, keys in `secrets/deep/keys.env`,\n"+
			"code in `internal/util/helper.go`, figures in `docs/EF.pdf`.\n")
	ctx, err := change.LoadContext(root, "conf", "")
	if err != nil {
		t.Fatal(err)
	}
	return ctx
}

func TestBundleWithholdsForExternalRunner(t *testing.T) {
	a, err := Assemble(confidentialFixture(t), "senior-staff", "specs")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"INSTRUCTIONS.md", "secrets/deep/keys.env", "docs/EF.pdf"}
	if len(a.Evidence.Withheld) != 3 {
		t.Fatalf("withheld = %v", a.Evidence.Withheld)
	}
	for i, w := range want {
		if a.Evidence.Withheld[i] != w {
			t.Fatalf("withheld = %v, want %v", a.Evidence.Withheld, want)
		}
	}

	if len(a.Evidence.NeedsExtraction) != 0 {
		t.Fatalf("needsExtraction = %v", a.Evidence.NeedsExtraction)
	}
	if len(a.Evidence.Files) != 1 || a.Evidence.Files[0].Path != "internal/util/helper.go" {
		t.Fatalf("files = %+v", a.Evidence.Files)
	}
	rendered := Render(a)
	if !strings.Contains(rendered, "### Withheld citations") ||
		!strings.Contains(rendered, "- INSTRUCTIONS.md") ||
		!strings.Contains(rendered, "ask the human at the gate") {
		t.Fatal("withheld section missing from render")
	}
	for _, leak := range []string{"42 pesos", "API_KEY", "%PDF", "pricing table"} {
		if strings.Contains(rendered, leak) {
			t.Fatalf("confidential content %q leaked into the prompt", leak)
		}
	}
}

func TestBundleFullViewForClaudeRunner(t *testing.T) {

	a, err := Assemble(confidentialFixture(t), "senior-engineer", "specs")
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Evidence.Withheld) != 0 {
		t.Fatalf("withheld = %v", a.Evidence.Withheld)
	}
	rendered := Render(a)
	for _, needle := range []string{"42 pesos", "API_KEY", "Extracted pricing table"} {
		if !strings.Contains(rendered, needle) {
			t.Fatalf("claude view missing %q", needle)
		}
	}
	if strings.Contains(rendered, "### Withheld citations") {
		t.Fatal("withheld section rendered for a trusted runner")
	}
}

func TestToolsConfidentialBoundary(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "INSTRUCTIONS.md", "commercial figure: 42 pesos")
	writeFile(t, root, "docs/EF.pdf", "%PDF raw")
	writeFile(t, root, "docs/EF.pdf.md", "extracted pricing table")
	writeFile(t, root, "public.md", "public mention of pesos")
	conf := []string{"INSTRUCTIONS.md", "docs/EF.pdf"}

	out := executeTool(root, conf, "read_file", `{"path":"INSTRUCTIONS.md"}`)
	if !strings.Contains(out, "confidential") || strings.Contains(out, "42 pesos") {
		t.Fatalf("out = %q", out)
	}

	out = executeTool(root, conf, "read_file", `{"path":"docs/EF.pdf"}`)
	if !strings.Contains(out, "confidential") || strings.Contains(out, "EF.pdf.md") {
		t.Fatalf("out = %q", out)
	}

	out = executeTool(root, conf, "read_file", `{"path":"docs/EF.pdf.md"}`)
	if !strings.Contains(out, "confidential") || strings.Contains(out, "pricing") {
		t.Fatalf("out = %q", out)
	}

	out = executeTool(root, conf, "grep", `{"pattern":"pesos"}`)
	if strings.Contains(out, "INSTRUCTIONS.md") || !strings.Contains(out, "public.md") {
		t.Fatalf("grep = %q", out)
	}
	if out := executeTool(root, conf, "grep", `{"pattern":"pricing"}`); out != "no matches" {
		t.Fatalf("grep leaked sibling content: %q", out)
	}

	if out := executeTool(root, conf, "list_dir", `{"path":""}`); !strings.Contains(out, "INSTRUCTIONS.md") {
		t.Fatalf("list = %q", out)
	}

	if out := executeTool(root, nil, "read_file", `{"path":"INSTRUCTIONS.md"}`); out != "commercial figure: 42 pesos" {
		t.Fatalf("trusted read = %q", out)
	}
}

func TestToolsConfidentialSymlinkedRoot(t *testing.T) {

	realRoot := t.TempDir()
	writeFile(t, realRoot, "INSTRUCTIONS.md", "commercial figure")
	linkRoot := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(realRoot, linkRoot); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	out := executeTool(linkRoot, []string{"INSTRUCTIONS.md"}, "read_file", `{"path":"INSTRUCTIONS.md"}`)
	if !strings.Contains(out, "confidential") || strings.Contains(out, "commercial figure") {
		t.Fatalf("symlinked root failed open: %q", out)
	}
}

func TestGrepScansOnlyRegularFiles(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "INSTRUCTIONS.md", "secret-token-xyz")
	if err := os.Symlink(filepath.Join(root, "INSTRUCTIONS.md"), filepath.Join(root, "alias.txt")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	out := executeTool(root, []string{"INSTRUCTIONS.md"}, "grep", `{"pattern":"secret-token"}`)
	if out != "no matches" {
		t.Fatalf("grep read through a symlink alias: %q", out)
	}
}

func TestRequestExtractionConfidentialEscalates(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "docs/EF.pdf", "%PDF")
	writeFile(t, root, "public/ok.pdf", "%PDF")
	e := &toolExecutor{
		root: root, changeDir: t.TempDir(),
		persona: "p", artifact: "a", maxRoundTrips: 2,
		confidential: []string{"docs/**"},
	}

	out := e.execute("request_extraction", `{"path":"docs/EF.pdf","detail":"d","rationale":"r"}`)
	if !strings.Contains(out, "confidential") || !strings.Contains(out, "ask the human") {
		t.Fatalf("out = %q", out)
	}
	if len(e.pending) != 0 {
		t.Fatalf("pending = %d", len(e.pending))
	}
	if _, err := os.Stat(filepath.Join(e.changeDir, NeedsFileName)); !os.IsNotExist(err) {
		t.Fatal("confidential escalation wrote a needs file")
	}

	out = e.execute("request_extraction", `{"path":"public/ok.pdf","detail":"d","rationale":"r"}`)
	if !strings.Contains(out, "recorded") || len(e.pending) != 1 {
		t.Fatalf("out=%q pending=%d", out, len(e.pending))
	}
}
