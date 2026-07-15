package core

import (
	"reflect"
	"testing"
)

func TestReadProjectConfigConfidential(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "openspec/config.yaml", `schema: team-driven
team:
  confidential:
    - INSTRUCTIONS.md
    - "secrets/**"
`)
	cfg := ReadProjectConfig(root)
	if want := []string{"INSTRUCTIONS.md", "secrets/**"}; !reflect.DeepEqual(cfg.Team.Confidential, want) {
		t.Fatalf("confidential = %v", cfg.Team.Confidential)
	}

	bare := t.TempDir()
	writeFile(t, bare, "openspec/config.yaml", "schema: team-driven\n")
	if got := ReadProjectConfig(bare).Team.Confidential; got != nil {
		t.Fatalf("confidential = %v", got)
	}
	if MatchesConfidential(nil, "INSTRUCTIONS.md") {
		t.Fatal("nil pattern list must match nothing")
	}
}

func TestMatchesConfidential(t *testing.T) {
	cases := []struct {
		pattern string
		path    string
		want    bool
	}{
		{"INSTRUCTIONS.md", "INSTRUCTIONS.md", true},
		{"INSTRUCTIONS.md", "docs/INSTRUCTIONS.md", false},
		{"*.env", "prod.env", true},
		{"*.env", "config/prod.env", false},
		{"secrets/*", "secrets/keys.env", true},
		{"secrets/*", "secrets/deep/keys.env", false},

		{"secrets/**", "secrets/keys.env", true},
		{"secrets/**", "secrets/a/b/c/keys.env", true},
		{"secrets/**", "public/keys.env", false},
		{"**/keys.env", "keys.env", true},
		{"**/keys.env", "a/b/keys.env", true},
		{"docs/**/figures.xlsx", "docs/figures.xlsx", true},
		{"docs/**/figures.xlsx", "docs/q1/data/figures.xlsx", true},
		{"env/[ps]rod.yaml", "env/prod.yaml", true},
		{"env/[ps]rod.yaml", "env/qrod.yaml", false},

		{"INSTRUCTIONS.md", "INSTRUCTIONS.md.md", true},
		{"docs/EF.pdf", "docs/EF.pdf.md", true},
		{"secrets/**", "secrets/report.pdf.md", true},
		{"README", "README.md", true},
		{"NOTES.md", "NOTES.txt", false},
	}
	for _, c := range cases {
		if got := MatchesConfidential([]string{c.pattern}, c.path); got != c.want {
			t.Errorf("MatchesConfidential(%q, %q) = %v, want %v", c.pattern, c.path, got, c.want)
		}
	}
}

func TestMatchesConfidentialFailsClosed(t *testing.T) {

	if !MatchesConfidential([]string{"secrets/[a-"}, "anything.txt") {
		t.Fatal("malformed pattern must fail closed (match)")
	}
	if err := CheckConfidentialPattern("secrets/[a-"); err == nil {
		t.Fatal("CheckConfidentialPattern must flag the malformed segment")
	}
	if err := CheckConfidentialPattern("secrets/**"); err != nil {
		t.Fatalf("well-formed pattern flagged: %v", err)
	}
}
