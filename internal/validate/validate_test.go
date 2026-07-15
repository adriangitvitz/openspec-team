package validate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const validSpec = `# auth Specification

## Purpose
Users authenticate with the system so their data stays private forever.

## Requirements

### Requirement: User Login
The system SHALL allow users to log in.

#### Scenario: Valid credentials
- **WHEN** a user submits valid credentials
- **THEN** a session is created
`

func TestSpecContentValid(t *testing.T) {
	r := SpecContent("auth", validSpec, false)
	if !r.Valid || r.Summary.Errors != 0 {
		t.Fatalf("report = %+v", r)
	}
}

func TestSpecContentMissingSections(t *testing.T) {
	r := SpecContent("auth", "# x\njust text\n", false)
	if r.Valid || len(r.Issues) != 1 {
		t.Fatalf("report = %+v", r)
	}
	if !strings.Contains(r.Issues[0].Message, `Expected headers: "## Purpose" and "## Requirements"`) {
		t.Fatalf("message = %q", r.Issues[0].Message)
	}
}

func TestSpecContentBriefPurposeWarns(t *testing.T) {
	content := strings.Replace(validSpec,
		"Users authenticate with the system so their data stays private forever.",
		"Short.", 1)
	r := SpecContent("auth", content, false)
	if !r.Valid || r.Summary.Warnings != 1 {
		t.Fatalf("report = %+v", r)
	}
	if SpecContent("auth", content, true).Valid {
		t.Fatal("strict should fail on warnings")
	}
}

func TestSpecContentNoScenarios(t *testing.T) {
	content := `## Purpose
Users authenticate with the system so their data stays private forever.

## Requirements

### Requirement: User Login
The system SHALL allow users to log in.
`
	r := SpecContent("auth", content, false)
	if r.Valid {
		t.Fatalf("report = %+v", r)
	}

	if r.Summary.Errors != 1 || r.Summary.Warnings != 1 {
		t.Fatalf("summary = %+v issues=%+v", r.Summary, r.Issues)
	}
}

func TestSpecContentShallInHeaderOnlyHint(t *testing.T) {
	content := `## Purpose
Users authenticate with the system so their data stays private forever.

## Requirements

### Requirement: The system SHALL log users in
Logging in works with email and password.

#### Scenario: Login
- **WHEN** x
- **THEN** y
`
	r := SpecContent("auth", content, false)
	if r.Valid {
		t.Fatalf("report = %+v", r)
	}
	found := false
	for _, i := range r.Issues {
		if strings.Contains(i.Message, "not only in the header") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected header-only hint, issues = %+v", r.Issues)
	}
}

func TestSpecContentDeltaHeaderStructuralError(t *testing.T) {
	content := validSpec + "\n## ADDED Requirements\n"
	r := SpecContent("auth", content, false)
	if r.Valid {
		t.Fatalf("report = %+v", r)
	}
	found := false
	for _, i := range r.Issues {
		if strings.Contains(i.Message, "delta header") && i.Line > 0 {
			found = true
		}
	}
	if !found {
		t.Fatalf("issues = %+v", r.Issues)
	}
}

func writeChange(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for rel, content := range files {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

const validDelta = `## ADDED Requirements

### Requirement: Data Export
The system SHALL export data as CSV.

#### Scenario: Export
- **WHEN** the user clicks export
- **THEN** a CSV downloads
`

func TestChangeDeltaSpecsValid(t *testing.T) {
	dir := writeChange(t, map[string]string{
		"proposal.md":          "## Why\nBecause.",
		"specs/export/spec.md": validDelta,
	})
	r := ChangeDeltaSpecs(dir, false)
	if !r.Valid {
		t.Fatalf("report = %+v", r)
	}
}

func TestChangeDeltaSpecsNoDeltas(t *testing.T) {
	dir := writeChange(t, map[string]string{"proposal.md": "## Why\nBecause."})
	r := ChangeDeltaSpecs(dir, false)
	if r.Valid {
		t.Fatal("expected invalid")
	}
	found := false
	for _, i := range r.Issues {
		if strings.Contains(i.Message, "Change must have at least one delta") {
			found = true
		}
	}
	if !found {
		t.Fatalf("issues = %+v", r.Issues)
	}
}

func TestChangeDeltaSpecsMissingScenario(t *testing.T) {
	dir := writeChange(t, map[string]string{
		"proposal.md": "x",
		"specs/export/spec.md": `## ADDED Requirements

### Requirement: Data Export
The system SHALL export data as CSV.
`,
	})
	r := ChangeDeltaSpecs(dir, false)
	if r.Valid {
		t.Fatal("expected invalid")
	}
	found := false
	for _, i := range r.Issues {
		if strings.Contains(i.Message, "must include at least one scenario") {
			found = true
		}
	}
	if !found {
		t.Fatalf("issues = %+v", r.Issues)
	}
}

func TestChangeDeltaSpecsCrossSectionConflicts(t *testing.T) {
	dir := writeChange(t, map[string]string{
		"proposal.md": "x",
		"specs/export/spec.md": `## MODIFIED Requirements

### Requirement: Data Export
The system SHALL export data as CSV and JSON.

#### Scenario: Export
- **WHEN** x
- **THEN** y

## REMOVED Requirements

- ` + "`### Requirement: Data Export`" + `
`,
	})
	r := ChangeDeltaSpecs(dir, false)
	found := false
	for _, i := range r.Issues {
		if strings.Contains(i.Message, "both MODIFIED and REMOVED") {
			found = true
		}
	}
	if !found {
		t.Fatalf("issues = %+v", r.Issues)
	}
}

func TestChangeDeltaSpecsDuplicates(t *testing.T) {
	dir := writeChange(t, map[string]string{
		"proposal.md": "x",
		"specs/export/spec.md": `## ADDED Requirements

### Requirement: Same
The system SHALL a.

#### Scenario: A
- **WHEN** x
- **THEN** y

### Requirement: Same
The system SHALL b.

#### Scenario: B
- **WHEN** x
- **THEN** y
`,
	})
	r := ChangeDeltaSpecs(dir, false)
	found := false
	for _, i := range r.Issues {
		if strings.Contains(i.Message, `Duplicate requirement in ADDED: "Same"`) {
			found = true
		}
	}
	if !found {
		t.Fatalf("issues = %+v", r.Issues)
	}
}

func TestChangeDeltaSpecsEmptySections(t *testing.T) {
	dir := writeChange(t, map[string]string{
		"proposal.md":          "x",
		"specs/export/spec.md": "## ADDED Requirements\n\nprose only\n",
	})
	r := ChangeDeltaSpecs(dir, false)
	found := false
	for _, i := range r.Issues {
		if strings.Contains(i.Message, "no requirement entries parsed") {
			found = true
		}
	}
	if !found {
		t.Fatalf("issues = %+v", r.Issues)
	}
}

func TestChangeDeltaSpecsSkippedHeaderInfo(t *testing.T) {
	dir := writeChange(t, map[string]string{
		"proposal.md": "x",
		"specs/export/spec.md": validDelta + `
### Documentation Requirements
notes
`,
	})
	r := ChangeDeltaSpecs(dir, false)
	if !r.Valid {
		t.Fatalf("INFO must not fail validation: %+v", r)
	}
	if r.Summary.Info != 1 {
		t.Fatalf("summary = %+v issues=%+v", r.Summary, r.Issues)
	}
}

func TestChangeDeltaSpecsNestedLayout(t *testing.T) {
	dir := writeChange(t, map[string]string{
		"proposal.md":               "x",
		"specs/area/export/spec.md": validDelta,
	})
	r := ChangeDeltaSpecs(dir, false)
	if !r.Valid {
		t.Fatalf("report = %+v", r)
	}
}

func TestChangeDeltaSpecsMissingProposalWarns(t *testing.T) {
	dir := writeChange(t, map[string]string{"specs/export/spec.md": validDelta})
	r := ChangeDeltaSpecs(dir, false)
	if !r.Valid || r.Summary.Warnings != 1 {
		t.Fatalf("report = %+v", r)
	}
}
