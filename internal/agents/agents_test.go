package agents

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adriangitvitz/openspec-team/internal/core"
)

func TestGenerateWritesAllFiles(t *testing.T) {
	root := t.TempDir()
	written, err := Generate(root, "1.2.3")
	if err != nil {
		t.Fatal(err)
	}

	if len(written) != 31 {
		t.Fatalf("written = %d files", len(written))
	}

	skill, err := os.ReadFile(filepath.Join(root, ".claude", "skills", "openspec-archive-change", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(skill)
	if !strings.Contains(content, `version: "1.2.3"`) {
		t.Fatal("version not rendered")
	}
	if strings.Contains(content, "{{.Version}}") {
		t.Fatal("template placeholder leaked into output")
	}
	if !strings.Contains(content, "allowed-tools: Bash(openspec:*)") {
		t.Fatal("allowed-tools missing")
	}

	if !strings.Contains(content, "openspec archive") {
		t.Fatal("archive skill does not call openspec archive")
	}
	if !strings.Contains(content, "--refresh-fingerprints") {
		t.Fatal("archive skill lacks conflict remediation")
	}

	cmdFile, err := os.ReadFile(filepath.Join(root, ".claude", "commands", "opsx", "propose.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cmdFile), "/opsx:propose") {
		t.Fatal("command frontmatter missing name")
	}

	crushCmd, err := os.ReadFile(filepath.Join(root, ".crush", "commands", "opsx", "propose.md"))
	if err != nil {
		t.Fatal(err)
	}
	crush := string(crushCmd)
	if strings.HasPrefix(crush, "---") {
		t.Fatal("crush command still has frontmatter")
	}
	if !strings.Contains(crush, "openspec new change") {
		t.Fatal("crush command body missing")
	}
	if strings.Contains(crush, "{{.Version}}") {
		t.Fatal("template placeholder leaked into crush output")
	}

	agent, err := os.ReadFile(filepath.Join(root, ".claude", "agents", "opsx-qa.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(agent), "name: opsx-qa") {
		t.Fatal("persona agent frontmatter missing name")
	}
	if strings.Contains(string(agent), "{{.Version}}") {
		t.Fatal("template placeholder leaked into agent output")
	}
	if _, err := os.Stat(filepath.Join(root, ".crush", "agents")); !os.IsNotExist(err) {
		t.Fatal("crush must not get agent files")
	}
	for _, persona := range core.PersonaIDs {
		if _, err := os.Stat(filepath.Join(root, ".claude", "agents", "opsx-"+persona+".md")); err != nil {
			t.Fatalf("missing agent for persona %s: %v", persona, err)
		}
	}

	if _, err := Generate(root, "1.2.4"); err != nil {
		t.Fatal(err)
	}
	updated, _ := os.ReadFile(filepath.Join(root, ".claude", "skills", "openspec-propose", "SKILL.md"))
	if !strings.Contains(string(updated), `version: "1.2.4"`) {
		t.Fatal("regeneration did not overwrite")
	}
}

func TestAssetsReferenceOnlySupportedCommands(t *testing.T) {

	supported := map[string]bool{
		"new": true, "status": true, "instructions": true,
		"list": true, "validate": true, "archive": true,
		"init": true, "update": true, "team": true,
	}
	entries := []string{"assets/skills", "assets/commands", "assets/agents"}
	for _, dir := range entries {
		files, err := assets.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			content, err := assets.ReadFile(dir + "/" + f.Name())
			if err != nil {
				t.Fatal(err)
			}
			for _, line := range strings.Split(string(content), "\n") {
				idx := strings.Index(line, "openspec ")
				if idx < 0 {
					continue
				}
				rest := strings.Fields(line[idx+len("openspec "):])
				if len(rest) == 0 {
					continue
				}
				sub := strings.Trim(rest[0], "`\"'.,)(:;")
				if sub == "" || strings.HasPrefix(sub, "-") || strings.HasPrefix(sub, "<") {
					continue
				}
				if strings.ToLower(sub) != sub {
					continue
				}
				if !supported[sub] {
					t.Errorf("%s/%s references unsupported command %q in line: %s", dir, f.Name(), sub, strings.TrimSpace(line))
				}
			}
		}
	}
}

func TestAssetsCarrySearchRoutingDiscipline(t *testing.T) {
	root := t.TempDir()
	if _, err := Generate(root, "1.0.0"); err != nil {
		t.Fatal(err)
	}

	for _, persona := range core.PersonaIDs {
		content, err := os.ReadFile(filepath.Join(root, ".claude", "agents", "opsx-"+persona+".md"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(content), "search MCP") {
			t.Errorf("persona %s lacks the search-routing discipline", persona)
		}
	}

	for _, rel := range []string{
		".claude/skills/openspec-team/SKILL.md",
		".claude/commands/opsx/team.md",
		".crush/commands/opsx/team.md",
	} {
		content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		for _, needle := range []string{"search MCP", "OPENSPEC_SEARCH_TOKEN", "untrusted"} {
			if !strings.Contains(string(content), needle) {
				t.Errorf("%s missing %q", rel, needle)
			}
		}
	}
}

func TestAssetsCarryExtractionLoop(t *testing.T) {
	root := t.TempDir()
	if _, err := Generate(root, "1.0.0"); err != nil {
		t.Fatal(err)
	}

	for _, rel := range []string{
		".claude/skills/openspec-team/SKILL.md",
		".claude/commands/opsx/team.md",
		".crush/commands/opsx/team.md",
	} {
		content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		for _, needle := range []string{
			"source-sha256:", "source-modified:", "extraction-needs.json", "exits with code 7",
			"openspec team tools --json", "surface the request to the human",
			"human-reviewed extraction outranks",
		} {
			if !strings.Contains(string(content), needle) {
				t.Errorf("%s missing %q", rel, needle)
			}
		}
	}

	for _, persona := range core.PersonaIDs {
		content, err := os.ReadFile(filepath.Join(root, ".claude", "agents", "opsx-"+persona+".md"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(content), "section/page anchors") {
			t.Errorf("persona %s lacks the anchor-citation line", persona)
		}
	}
}

func TestAssetsCarryConfidentialBoundary(t *testing.T) {
	root := t.TempDir()
	if _, err := Generate(root, "1.0.0"); err != nil {
		t.Fatal(err)
	}

	for _, rel := range []string{
		".claude/skills/openspec-team/SKILL.md",
		".claude/commands/opsx/team.md",
		".crush/commands/opsx/team.md",
	} {
		content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		for _, needle := range []string{

			"Harness data boundary",
			"team.confidential",
			"withheld",
			"Sibling extractions inherit their source's confidentiality",

			"deliberate human act",
			"outside* the confidential set",

			"never extracted on behalf of an external-runner persona",

			"never as a readable path",

			"confidential citations listed as withheld",
		} {
			if !strings.Contains(string(content), needle) {
				t.Errorf("%s missing %q", rel, needle)
			}
		}
	}
}

func TestAssetsCarryGroundingDiscipline(t *testing.T) {
	root := t.TempDir()
	if _, err := Generate(root, "1.0.0"); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{
		".claude/skills/openspec-team/SKILL.md",
		".claude/commands/opsx/team.md",
		".crush/commands/opsx/team.md",
	} {
		content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		for _, needle := range []string{
			"Source manifest",
			"sources.md",
			"confirm the list with the user",
			"discrepancy to report, never an assumed source",
			"coverage: sheets 3 of 3",
			"enumerate every sheet or page",
			"carries the source manifest section and its cited sources",
			"confidential citations listed as withheld",
			"duplicated task text are validation errors",
		} {
			if !strings.Contains(string(content), needle) {
				t.Errorf("%s missing %q", rel, needle)
			}
		}
	}
	engineer, err := os.ReadFile(filepath.Join(root, ".claude", "agents", "opsx-senior-engineer.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, needle := range []string{
		"Verify every task",
		"already implemented",
		"not derivable from a requirement",
	} {
		if !strings.Contains(string(engineer), needle) {
			t.Errorf("senior-engineer missing %q", needle)
		}
	}
}

func TestAssetsCarryUxSchema(t *testing.T) {
	root := t.TempDir()
	if _, err := Generate(root, "1.0.0"); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{
		".claude/skills/openspec-team/SKILL.md",
		".claude/commands/opsx/team.md",
		".crush/commands/opsx/team.md",
	} {
		content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		for _, needle := range []string{
			"team-driven or team-driven-ux",
			"say so and stop",
			"ux-review (team-driven-ux only)",
			"`ui-ux` (dispatched only on team-driven-ux)",
			"ui-ux persona always keeps ux-review",
		} {
			if !strings.Contains(string(content), needle) {
				t.Errorf("%s missing %q", rel, needle)
			}
		}
	}
}

func TestUIUXAgentCarriesExpertise(t *testing.T) {
	root := t.TempDir()
	if _, err := Generate(root, "1.0.0"); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(root, ".claude", "agents", "opsx-ui-ux.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, needle := range []string{
		"Material 3", "Apple HIG", "Fluent", "Carbon", "Polaris", "Ant Design",
		"token roles", "adopt vs\n   adapt vs build",
		"internal-versus-external consistency",
		"isomorphic",
		"Hook Model", "B=MAP", "reflective-endorsement",
		"Dark patterns", "findings, never suggestions",
		"WCAG 2.2 AA", "keyboard-", "screen-reader", "COGA", "reach amplifier",
		"Nielsen", "cognitive\n   walkthrough",
		"computed, never estimated", "colorsenv",
		"calculate_contrast_ratio", "generate_suitable_variations",
		"report ratios as unverified",
		"vanity metrics",
		"named-organization sources",
	} {
		if !strings.Contains(string(content), needle) {
			t.Errorf("opsx-ui-ux.md missing %q", needle)
		}
	}
}

func TestAssetsCarryIntakeDiscipline(t *testing.T) {
	root := t.TempDir()
	if _, err := Generate(root, "1.0.0"); err != nil {
		t.Fatal(err)
	}

	for _, rel := range []string{
		".claude/skills/openspec-team/SKILL.md",
		".claude/commands/opsx/team.md",
		".crush/commands/opsx/team.md",
	} {
		content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		for _, needle := range []string{

			"never perform the domain analysis yourself",

			"dispatch the product-owner's research phase scoped to the question",
			"/opsx:explore",

			"no directory layout is ever assumed",
			"ask the user for its location before dispatching any persona",
			"create the grounding from scratch",

			"before any codebase exploration",

			"Intake addendum",
			"only permitted addition to a verbatim assembly",

			"extracted before the first persona dispatch",
		} {
			if !strings.Contains(string(content), needle) {
				t.Errorf("%s missing %q", rel, needle)
			}
		}
	}
}
