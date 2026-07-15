package team

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adriangitvitz/openspec-go/internal/change"
)

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func teamChange(t *testing.T) *change.Context {
	t.Helper()
	root := t.TempDir()
	writeFile(t, root, "openspec/changes/my-feature/.openspec.yaml", "schema: team-driven\n")
	writeFile(t, root, "openspec/changes/my-feature/research.md", "# Research\n\nGrounding.\n")
	writeFile(t, root, "internal/util/helper.go", "package util\n\nfunc Help() {}\n")
	writeFile(t, root, "big.txt", strings.Repeat("x", perFileBudget+100))
	writeFile(t, root, "openspec/changes/my-feature/notes/decision.md", "Change-local decision record.\n")
	writeFile(t, root, "openspec/changes/my-feature/proposal.md",
		"# Proposal\n\nThe helper lives in `internal/util/helper.go` and the old one in `internal/missing/gone.go`.\n"+
			"Bulk data sits in `big.txt`; see also `notes/decision.md` and the missing `missingfile.md`.\n"+
			"Run `openspec validate` with `grep` (prose, not paths).\n")

	ctx, err := change.LoadContext(root, "my-feature", "")
	if err != nil {
		t.Fatal(err)
	}
	return ctx
}

func TestAssembleUnknownPersona(t *testing.T) {
	ctx := teamChange(t)
	if _, err := Assemble(ctx, "designer", "specs"); err == nil || !strings.Contains(err.Error(), `unknown persona "designer"`) {
		t.Fatalf("err = %v", err)
	}
}

func TestAssembleUnknownArtifact(t *testing.T) {
	ctx := teamChange(t)
	if _, err := Assemble(ctx, "senior-staff", "blueprints"); err == nil || !strings.Contains(err.Error(), `unknown artifact "blueprints"`) {
		t.Fatalf("err = %v", err)
	}
}

func TestAssembleIncompleteDependency(t *testing.T) {
	ctx := teamChange(t)

	if _, err := Assemble(ctx, "qa", "test-matrix"); err == nil || !strings.Contains(err.Error(), `dependency artifact "specs" is not complete`) {
		t.Fatalf("err = %v", err)
	}
}

func TestAssembleSpecsForSeniorStaff(t *testing.T) {
	ctx := teamChange(t)
	a, err := Assemble(ctx, "senior-staff", "specs")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(a.SystemPrompt, "Senior Staff Engineer") {
		t.Fatal("system prompt is not the senior-staff persona")
	}
	if strings.Contains(a.SystemPrompt, "---\nname:") {
		t.Fatal("frontmatter leaked into the system prompt")
	}
	if !strings.Contains(a.Brief.Instruction, "senior-staff") {
		t.Fatal("brief lacks the specs artifact instruction")
	}
	if len(a.Dependencies) != 1 || a.Dependencies[0].ArtifactID != "proposal" ||
		!strings.Contains(a.Dependencies[0].Content, "The helper lives in") {
		t.Fatalf("dependencies = %+v", a.Dependencies)
	}
}

func TestEvidenceBundle(t *testing.T) {
	ctx := teamChange(t)
	a, err := Assemble(ctx, "senior-staff", "specs")
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]EvidenceFile{}
	for _, f := range a.Evidence.Files {
		files[f.Path] = f
	}
	helper, ok := files["internal/util/helper.go"]
	if !ok || !strings.Contains(helper.Content, "func Help()") || helper.Truncated {
		t.Fatalf("helper = %+v ok=%v", helper, ok)
	}
	big, ok := files["big.txt"]
	if !ok || !big.Truncated || len(big.Content) != perFileBudget {
		t.Fatalf("big = truncated=%v len=%d ok=%v", big.Truncated, len(big.Content), ok)
	}

	local, ok := files["notes/decision.md"]
	if !ok || !strings.Contains(local.Content, "Change-local decision record") {
		t.Fatalf("change-relative citation not inlined: %+v ok=%v", local, ok)
	}

	want := []string{"internal/missing/gone.go", "missingfile.md"}
	if len(a.Evidence.Unresolved) != 2 || a.Evidence.Unresolved[0] != want[0] || a.Evidence.Unresolved[1] != want[1] {
		t.Fatalf("unresolved = %v, want %v", a.Evidence.Unresolved, want)
	}

	for _, u := range a.Evidence.Unresolved {
		if u == "grep" {
			t.Fatal("prose token treated as citation")
		}
	}

	rendered := Render(a)
	for _, needle := range []string{
		"# Persona: senior-staff — artifact: specs",
		"## System prompt",
		"## Artifact brief",
		"## Dependency artifact: proposal (proposal.md)",
		"### internal/util/helper.go",
		"[truncated: big.txt exceeds the evidence budget]",
		"### Unresolved citations",
		"- internal/missing/gone.go",
	} {
		if !strings.Contains(rendered, needle) {
			t.Errorf("rendered prompt missing %q", needle)
		}
	}
}

func TestAssembleDeterministic(t *testing.T) {
	ctx := teamChange(t)
	a1, err := Assemble(ctx, "senior-staff", "specs")
	if err != nil {
		t.Fatal(err)
	}
	a2, err := Assemble(ctx, "senior-staff", "specs")
	if err != nil {
		t.Fatal(err)
	}
	if Render(a1) != Render(a2) {
		t.Fatal("assembly is not deterministic")
	}
}

func TestEvidenceBundleTotalBudget(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "openspec/changes/big/.openspec.yaml", "schema: team-driven\n")
	writeFile(t, root, "openspec/changes/big/research.md", "# Research\n\nOK.\n")

	cites := ""
	for i := 0; i < 7; i++ {
		name := fmt.Sprintf("chunk%d.txt", i)
		writeFile(t, root, name, strings.Repeat("y", perFileBudget))
		cites += "See `" + name + "`. "
	}
	writeFile(t, root, "openspec/changes/big/proposal.md", "# P\n\n"+cites+"\n")

	ctx, err := change.LoadContext(root, "big", "")
	if err != nil {
		t.Fatal(err)
	}
	a, err := Assemble(ctx, "senior-staff", "specs")
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Evidence.Files) != 7 {
		t.Fatalf("files = %d", len(a.Evidence.Files))
	}
	total := 0
	truncated := 0
	for _, f := range a.Evidence.Files {
		total += len(f.Content)
		if f.Truncated {
			truncated++
		}
	}
	if total > totalBudget {
		t.Fatalf("total inlined %d exceeds budget %d", total, totalBudget)
	}
	if truncated == 0 {
		t.Fatal("no file marked truncated despite exhausted total budget")
	}
}

func TestWriteArtifactOutput(t *testing.T) {
	dir := t.TempDir()
	if _, err := WriteArtifactOutput(dir, "specs", "specs/**/*.md", "content"); err == nil || !strings.Contains(err.Error(), "multi-file") {
		t.Fatalf("glob err = %v", err)
	}
	if _, err := WriteArtifactOutput(dir, "design", "design.md", "  \n"); err == nil || !strings.Contains(err.Error(), "empty output") {
		t.Fatalf("empty err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "design.md")); !os.IsNotExist(err) {
		t.Fatal("empty output created a file")
	}
	path, err := WriteArtifactOutput(dir, "design", "design.md", "# Design\n")
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil || string(content) != "# Design\n" {
		t.Fatalf("content=%q err=%v", content, err)
	}
}

func binaryFixture(t *testing.T, withSibling, stale bool) *change.Context {
	t.Helper()
	root := t.TempDir()
	writeFile(t, root, "openspec/changes/bin/.openspec.yaml", "schema: team-driven\n")
	writeFile(t, root, "openspec/changes/bin/research.md", "# R\nOK.\n")
	writeFile(t, root, "docs/EF.pdf", "%PDF-1.4 raw binary bytes")
	writeFile(t, root, "openspec/changes/bin/proposal.md",
		"# P\n\nSee `docs/EF.pdf` and the ghost `docs/ghost.pdf`.\n")
	if withSibling {
		hash := fmt.Sprintf("%x", sha256.Sum256([]byte("%PDF-1.4 raw binary bytes")))
		if stale {
			hash = strings.Repeat("0", 64)
		}
		writeFile(t, root, "docs/EF.pdf.md",
			"<!-- extraction of: docs/EF.pdf -->\n<!-- source-sha256: "+hash+" -->\n<!-- source-modified: 2026-07-14T00:00:00Z -->\n\n## Section 4.2\nExtracted field table.\n")
	}
	ctx, err := change.LoadContext(root, "bin", "")
	if err != nil {
		t.Fatal(err)
	}
	return ctx
}

func TestBundleBinaryCitationNeedsExtraction(t *testing.T) {
	a, err := Assemble(binaryFixture(t, false, false), "senior-staff", "specs")
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Evidence.NeedsExtraction) != 1 || a.Evidence.NeedsExtraction[0] != "docs/EF.pdf" {
		t.Fatalf("needsExtraction = %v", a.Evidence.NeedsExtraction)
	}
	rendered := Render(a)
	if strings.Contains(rendered, "%PDF-1.4") {
		t.Fatal("raw binary bytes leaked into the prompt")
	}
	if !strings.Contains(rendered, "### Citations needing extraction") {
		t.Fatal("needs-extraction section missing from render")
	}

	if len(a.Evidence.Unresolved) != 1 || a.Evidence.Unresolved[0] != "docs/ghost.pdf" {
		t.Fatalf("unresolved = %v", a.Evidence.Unresolved)
	}
}

func TestBundleExtractionSiblingPreferred(t *testing.T) {
	a, err := Assemble(binaryFixture(t, true, false), "senior-staff", "specs")
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Evidence.NeedsExtraction) != 0 {
		t.Fatalf("needsExtraction = %v", a.Evidence.NeedsExtraction)
	}
	var sib *EvidenceFile
	for i := range a.Evidence.Files {
		if a.Evidence.Files[i].ExtractionOf == "docs/EF.pdf" {
			sib = &a.Evidence.Files[i]
		}
	}
	if sib == nil || !strings.Contains(sib.Content, "Extracted field table") || sib.Stale {
		t.Fatalf("sibling = %+v", sib)
	}
	rendered := Render(a)
	if !strings.Contains(rendered, "[extraction of docs/EF.pdf") || strings.Contains(rendered, "%PDF-1.4") {
		t.Fatal("provenance note missing or raw bytes leaked")
	}
}

func TestBundleStaleExtractionNoted(t *testing.T) {
	a, err := Assemble(binaryFixture(t, true, true), "senior-staff", "specs")
	if err != nil {
		t.Fatal(err)
	}
	var stale bool
	for _, f := range a.Evidence.Files {
		if f.ExtractionOf == "docs/EF.pdf" && f.Stale {
			stale = true
		}
	}
	if !stale {
		t.Fatal("stale extraction not detected")
	}
	if !strings.Contains(Render(a), "[stale extraction:") {
		t.Fatal("stale note missing from render")
	}
}

func TestBundleSiblingOverBudgetTruncated(t *testing.T) {
	ctx := binaryFixture(t, true, false)

	sib := filepath.Join(ctx.Root, "docs", "EF.pdf.md")
	head, err := os.ReadFile(sib)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sib, append(head, []byte(strings.Repeat("z", perFileBudget+100))...), 0o644); err != nil {
		t.Fatal(err)
	}
	a, err := Assemble(ctx, "senior-staff", "specs")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range a.Evidence.Files {
		if f.ExtractionOf == "docs/EF.pdf" {
			if !f.Truncated || len(f.Content) != perFileBudget {
				t.Fatalf("sibling budget not applied: truncated=%v len=%d", f.Truncated, len(f.Content))
			}

			if f.Stale {
				t.Fatal("truncation broke the stale check")
			}
			return
		}
	}
	t.Fatal("sibling not inlined")
}

func TestReadFileRefusesBinaryDocuments(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "docs/EF.pdf", "%PDF raw")

	out := executeTool(root, nil, "read_file", `{"path":"docs/EF.pdf"}`)
	if !strings.Contains(out, "error") || !strings.Contains(out, "request_extraction") || strings.Contains(out, "%PDF") {
		t.Fatalf("out = %q", out)
	}

	writeFile(t, root, "docs/EF.pdf.md", "extracted text")
	out = executeTool(root, nil, "read_file", `{"path":"docs/EF.pdf"}`)
	if !strings.Contains(out, "docs/EF.pdf.md") || strings.Contains(out, "%PDF") {
		t.Fatalf("out = %q", out)
	}
	if out := executeTool(root, nil, "read_file", `{"path":"docs/EF.pdf.md"}`); out != "extracted text" {
		t.Fatalf("sibling read = %q", out)
	}
}
