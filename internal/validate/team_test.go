package validate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adriangitvitz/openspec-go/internal/core"
)

func TestTeamConfigValid(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "matrix.md"), []byte("| a |\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	team := core.TeamConfig{
		TestMatrix: "matrix.md",
		Personas: map[string]core.PersonaConfig{
			"product-owner":   {Runner: "openrouter", Model: "anthropic/claude-sonnet-4.5"},
			"senior-engineer": {Runner: "claude"},
		},
	}
	if issues := TeamConfigIssues(root, team); len(issues) != 0 {
		t.Fatalf("issues = %+v", issues)
	}
}

func TestTeamConfigUnknownPersona(t *testing.T) {
	team := core.TeamConfig{Personas: map[string]core.PersonaConfig{
		"designer": {Runner: "claude"},
	}}
	issues := TeamConfigIssues(t.TempDir(), team)
	if len(issues) != 1 || !strings.Contains(issues[0].Message, `"designer"`) {
		t.Fatalf("issues = %+v", issues)
	}
}

func TestTeamConfigOpenrouterRequiresModel(t *testing.T) {
	team := core.TeamConfig{Personas: map[string]core.PersonaConfig{
		"qa": {Runner: "openrouter"},
	}}
	issues := TeamConfigIssues(t.TempDir(), team)
	if len(issues) != 1 || !strings.Contains(issues[0].Message, "requires a model") {
		t.Fatalf("issues = %+v", issues)
	}
}

func TestTeamConfigUnknownRunner(t *testing.T) {
	team := core.TeamConfig{Personas: map[string]core.PersonaConfig{
		"qa": {Runner: "ollama"},
	}}
	issues := TeamConfigIssues(t.TempDir(), team)
	if len(issues) != 1 || !strings.Contains(issues[0].Message, `unknown runner "ollama"`) {
		t.Fatalf("issues = %+v", issues)
	}
}

func TestTeamConfigMissingMatrixPath(t *testing.T) {
	team := core.TeamConfig{TestMatrix: "docs/qa/matrix.md"}
	issues := TeamConfigIssues(t.TempDir(), team)
	if len(issues) != 1 || !strings.Contains(issues[0].Message, "does not exist") {
		t.Fatalf("issues = %+v", issues)
	}
}

func TestTeamConfigReport(t *testing.T) {
	if _, has := TeamConfigReport(t.TempDir(), core.TeamConfig{}, false); has {
		t.Fatal("empty team section must produce no report")
	}
	team := core.TeamConfig{Personas: map[string]core.PersonaConfig{
		"designer": {Runner: "claude"},
	}}
	report, has := TeamConfigReport(t.TempDir(), team, false)
	if !has || !report.Valid || report.Summary.Warnings != 1 {
		t.Fatalf("report = %+v has=%v", report, has)
	}
	strict, _ := TeamConfigReport(t.TempDir(), team, true)
	if strict.Valid {
		t.Fatal("strict report with warnings must be invalid")
	}
}

func TestTeamConfigConfidentialPatterns(t *testing.T) {
	root := t.TempDir()
	for _, p := range []string{"INSTRUCTIONS.md", "secrets/deep/keys.env", "node_modules/pkg/x.js"} {
		full := filepath.Join(root, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	ok := core.TeamConfig{Confidential: []string{"INSTRUCTIONS.md", "secrets/**"}}
	if issues := TeamConfigIssues(root, ok); len(issues) != 0 {
		t.Fatalf("issues = %+v", issues)
	}

	bad := core.TeamConfig{Confidential: []string{"secrets/[a-"}}
	issues := TeamConfigIssues(root, bad)
	if len(issues) != 1 || !strings.Contains(issues[0].Message, "malformed") {
		t.Fatalf("issues = %+v", issues)
	}

	miss := core.TeamConfig{Confidential: []string{"INSTRUCTONS.md", "node_modules/**"}}
	issues = TeamConfigIssues(root, miss)
	if len(issues) != 2 ||
		!strings.Contains(issues[0].Message, `"INSTRUCTONS.md" matches no existing file`) ||
		!strings.Contains(issues[1].Message, `"node_modules/**" matches no existing file`) {
		t.Fatalf("issues = %+v", issues)
	}

	report, has := TeamConfigReport(root, miss, false)
	if !has || report.Summary.Warnings != 2 {
		t.Fatalf("report = %+v has=%v", report, has)
	}
}

func TestTeamConfigMalformedSearchURL(t *testing.T) {
	team := core.TeamConfig{Search: core.SearchConfig{MCPURL: "::not-a-url"}}
	issues := TeamConfigIssues(t.TempDir(), team)
	if len(issues) != 1 || !strings.Contains(issues[0].Message, "team.search.mcp_url") {
		t.Fatalf("issues = %+v", issues)
	}

	report, has := TeamConfigReport(t.TempDir(), team, false)
	if !has || report.Summary.Warnings != 1 {
		t.Fatalf("report = %+v has=%v", report, has)
	}
	ok := core.TeamConfig{Search: core.SearchConfig{MCPURL: "http://localhost:8080/mcp"}}
	if issues := TeamConfigIssues(t.TempDir(), ok); len(issues) != 0 {
		t.Fatalf("issues = %+v", issues)
	}
}
