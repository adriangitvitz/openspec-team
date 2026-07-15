package core

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
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

func TestReadProjectConfigKnowledge(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "openspec/config.yaml", `schema: spec-driven
context: Multi-repo lab.
rules:
  specs:
    - "US and MX consolidation differ"
knowledge:
  - topic: ITK / material resolution
    note: read before touching materials
    paths:
      - docs/adr/adr-0001.md
      - src/material_xref.py
`)
	cfg := ReadProjectConfig(root)
	if cfg.Schema != "spec-driven" || cfg.Context != "Multi-repo lab." {
		t.Fatalf("cfg = %+v", cfg)
	}
	if len(cfg.Knowledge) != 1 {
		t.Fatalf("knowledge = %+v", cfg.Knowledge)
	}
	k := cfg.Knowledge[0]
	if k.Topic != "ITK / material resolution" || k.Note == "" {
		t.Fatalf("entry = %+v", k)
	}
	if want := []string{"docs/adr/adr-0001.md", "src/material_xref.py"}; !reflect.DeepEqual(k.Paths, want) {
		t.Fatalf("paths = %v", k.Paths)
	}
	if cfg.Rules["specs"][0] != "US and MX consolidation differ" {
		t.Fatalf("rules = %+v", cfg.Rules)
	}
}

func TestReadProjectConfigTeam(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "openspec/config.yaml", `schema: team-driven
team:
  test_matrix: docs/qa/matrix.md
  personas:
    product-owner:
      runner: openrouter
      model: anthropic/claude-sonnet-4.5
    senior-engineer:
      runner: claude
`)
	cfg := ReadProjectConfig(root)
	if cfg.Team.TestMatrix != "docs/qa/matrix.md" {
		t.Fatalf("test_matrix = %q", cfg.Team.TestMatrix)
	}
	if runner, model := cfg.Team.PersonaRunner("product-owner"); runner != "openrouter" || model != "anthropic/claude-sonnet-4.5" {
		t.Fatalf("product-owner = %s/%s", runner, model)
	}
	if runner, _ := cfg.Team.PersonaRunner("senior-engineer"); runner != "claude" {
		t.Fatalf("senior-engineer runner = %s", runner)
	}

	if runner, _ := cfg.Team.PersonaRunner("qa"); runner != "claude" {
		t.Fatalf("qa runner = %s", runner)
	}
}

func TestReadProjectConfigAbsentTeamSection(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "openspec/config.yaml", "schema: spec-driven\n")
	cfg := ReadProjectConfig(root)
	if len(cfg.Team.Personas) != 0 || cfg.Team.TestMatrix != "" {
		t.Fatalf("team = %+v", cfg.Team)
	}
	if runner, _ := cfg.Team.PersonaRunner("qa"); runner != "claude" {
		t.Fatalf("default runner = %s", runner)
	}
}

func TestDiscoverDocHubs(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "middleware/README.md", "x")
	writeFile(t, root, "middleware/docs-integracion/src/SUMMARY.md", "x")
	writeFile(t, root, "middleware/docs-integracion/src/adr/adr-0001.md", "x")
	writeFile(t, root, "labs/CLAUDE.md", "x")
	writeFile(t, root, "svc/ARCHITECTURE.md", "x")
	writeFile(t, root, "svc/docs/guide.md", "x")
	writeFile(t, root, "node_modules/pkg/CLAUDE.md", "x")
	writeFile(t, root, "a/b/c/d/e/f/CLAUDE.md", "x")

	entries := DiscoverDocHubs(root)
	if len(entries) != 2 {
		t.Fatalf("entries = %+v", entries)
	}
	adr, hubs := entries[0], entries[1]
	if adr.Topic != "Architecture decision records" {
		t.Fatalf("entry0 = %+v", adr)
	}
	if want := []string{"middleware/docs-integracion/src/adr"}; !reflect.DeepEqual(adr.Paths, want) {
		t.Fatalf("adr paths = %v", adr.Paths)
	}
	want := []string{
		"labs/CLAUDE.md",
		"middleware/docs-integracion",
		"middleware/docs-integracion/src/SUMMARY.md",
		"svc/ARCHITECTURE.md",
		"svc/docs",
	}
	if !reflect.DeepEqual(hubs.Paths, want) {
		t.Fatalf("hub paths = %v, want %v", hubs.Paths, want)
	}
}

func TestDiscoverDocHubsEmptyProject(t *testing.T) {
	if entries := DiscoverDocHubs(t.TempDir()); entries != nil {
		t.Fatalf("entries = %+v", entries)
	}
}

func TestReadProjectConfigSearch(t *testing.T) {
	root := t.TempDir()

	writeFile(t, root, "openspec/config.yaml", `schema: team-driven
team:
  search:
    mcp_url: http://localhost:8080/mcp
    token: should-be-ignored
`)
	cfg := ReadProjectConfig(root)
	if cfg.Team.Search.MCPURL != "http://localhost:8080/mcp" {
		t.Fatalf("mcp_url = %q", cfg.Team.Search.MCPURL)
	}
}
