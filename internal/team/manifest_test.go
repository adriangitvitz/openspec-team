package team

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/adriangitvitz/openspec-team/internal/change"
)

func manifestChange(t *testing.T, manifest string) *change.Context {
	t.Helper()
	root := t.TempDir()
	writeFile(t, root, "openspec/changes/mf/.openspec.yaml", "schema: team-driven\n")
	writeFile(t, root, "openspec/changes/mf/research.md", "# R\nOK.\n")
	writeFile(t, root, "openspec/changes/mf/proposal.md", "# P\n\nSee `notes/plan.md`.\n")
	writeFile(t, root, "openspec/changes/mf/notes/plan.md", "plan detail\n")
	writeFile(t, root, "data/budget.csv", "concept,amount\nlicenses,1200\n")
	if manifest != "" {
		writeFile(t, root, "openspec/changes/mf/sources.md", manifest)
	}
	ctx, err := change.LoadContext(root, "mf", "")
	if err != nil {
		t.Fatal(err)
	}
	return ctx
}

func TestManifestCitationInlined(t *testing.T) {
	ctx := manifestChange(t, "## Sources\n\n- `data/budget.csv` sha256:abc extraction:n/a\n")
	a, err := Assemble(ctx, "senior-staff", "specs")
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, f := range a.Evidence.Files {
		if f.Path == "data/budget.csv" && strings.Contains(f.Content, "licenses,1200") {
			found = true
		}
	}
	if !found {
		t.Fatalf("manifest citation not inlined: %+v", a.Evidence.Files)
	}
	rendered := Render(a)
	if !strings.Contains(rendered, "## Source manifest") ||
		!strings.Contains(rendered, "- `data/budget.csv` sha256:abc extraction:n/a") {
		t.Fatal("source manifest section missing from render")
	}
}

func TestManifestScannedBeforeDependencies(t *testing.T) {
	ctx := manifestChange(t, "")
	cites := "## Sources\n\n"
	for i := 0; i < 6; i++ {
		name := fmt.Sprintf("chunk%d.txt", i)
		writeFile(t, ctx.Root, name, strings.Repeat("m", perFileBudget))
		cites += "- `" + name + "`\n"
	}
	writeFile(t, ctx.Root, "openspec/changes/mf/sources.md", cites)
	writeFile(t, ctx.Root, "late.txt", "dependency-cited content")
	writeFile(t, ctx.Root, "openspec/changes/mf/proposal.md", "# P\n\nSee `late.txt`.\n")
	ctx, err := change.LoadContext(ctx.Root, "mf", "")
	if err != nil {
		t.Fatal(err)
	}
	a, err := Assemble(ctx, "senior-staff", "specs")
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]EvidenceFile{}
	for _, f := range a.Evidence.Files {
		files[f.Path] = f
	}
	for i := 0; i < 6; i++ {
		name := fmt.Sprintf("chunk%d.txt", i)
		if f := files[name]; f.Truncated || len(f.Content) != perFileBudget {
			t.Fatalf("manifest citation %s lost budget: truncated=%v len=%d", name, f.Truncated, len(f.Content))
		}
	}
	if late := files["late.txt"]; !late.Truncated || late.Content != "" {
		t.Fatalf("dependency citation should carry the truncation: %+v", late)
	}
}

func TestManifestConfidentialWithheld(t *testing.T) {
	ctx := manifestChange(t, "## Sources\n\n- `INSTRUCTIONS.md`\n")
	writeFile(t, ctx.Root, "INSTRUCTIONS.md", "commercial figure: 42 pesos")
	writeFile(t, ctx.Root, "openspec/config.yaml", `schema: team-driven
team:
  personas:
    senior-staff:
      runner: openrouter
      model: test/model
  confidential:
    - INSTRUCTIONS.md
`)
	a, err := Assemble(ctx, "senior-staff", "specs")
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Evidence.Withheld) != 1 || a.Evidence.Withheld[0] != "INSTRUCTIONS.md" {
		t.Fatalf("withheld = %v", a.Evidence.Withheld)
	}
	if strings.Contains(Render(a), "42 pesos") {
		t.Fatal("confidential manifest citation leaked")
	}
}

func TestAbsentManifestSilent(t *testing.T) {
	a, err := Assemble(manifestChange(t, ""), "senior-staff", "specs")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(Render(a), "## Source manifest") {
		t.Fatal("manifest section rendered without sources.md")
	}
	raw, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "sourceManifest") {
		t.Fatal("sourceManifest serialized when absent")
	}
}

func coverageFixture(t *testing.T, header string) *change.Context {
	t.Helper()
	root := t.TempDir()
	writeFile(t, root, "openspec/changes/cov/.openspec.yaml", "schema: team-driven\n")
	writeFile(t, root, "openspec/changes/cov/research.md", "# R\nOK.\n")
	writeFile(t, root, "openspec/changes/cov/proposal.md", "# P\n\nFigures in `docs/EF.xlsx`.\n")
	writeFile(t, root, "docs/EF.xlsx", "binary bytes")
	writeFile(t, root, "docs/EF.xlsx.md", header+"\n## Sheet 1\ndata\n")
	ctx, err := change.LoadContext(root, "cov", "")
	if err != nil {
		t.Fatal(err)
	}
	return ctx
}

func coverageOf(t *testing.T, ctx *change.Context) (EvidenceFile, string) {
	t.Helper()
	a, err := Assemble(ctx, "senior-staff", "specs")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range a.Evidence.Files {
		if f.ExtractionOf == "docs/EF.xlsx" {
			return f, Render(a)
		}
	}
	t.Fatal("sibling not inlined")
	return EvidenceFile{}, ""
}

func TestPartialCoverageNoted(t *testing.T) {
	f, rendered := coverageOf(t, coverageFixture(t, "<!-- coverage: sheets 2 of 5 -->"))
	if f.Coverage != "sheets 2 of 5" {
		t.Fatalf("coverage = %q", f.Coverage)
	}
	if !strings.Contains(rendered, "[partial extraction: covers sheets 2 of 5") {
		t.Fatal("partial note missing from render")
	}
}

func TestFullCoverageSilent(t *testing.T) {
	f, rendered := coverageOf(t, coverageFixture(t, "<!-- coverage: sheets 5 of 5 -->"))
	if f.Coverage != "" || strings.Contains(rendered, "partial extraction") {
		t.Fatalf("full coverage flagged: %q", f.Coverage)
	}
}

func TestAbsentCoverageSilent(t *testing.T) {
	f, rendered := coverageOf(t, coverageFixture(t, "<!-- extraction of: docs/EF.xlsx -->"))
	if f.Coverage != "" || strings.Contains(rendered, "partial extraction") {
		t.Fatalf("absent coverage flagged: %q", f.Coverage)
	}
}

func TestMalformedCoverageIgnored(t *testing.T) {
	f, _ := coverageOf(t, coverageFixture(t, "<!-- coverage: sheets two of 5 -->"))
	if f.Coverage != "" {
		t.Fatalf("malformed coverage parsed: %q", f.Coverage)
	}
}
