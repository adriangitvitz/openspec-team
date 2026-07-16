package validate

import (
	"strings"
	"testing"

	"github.com/adriangitvitz/openspec-team/internal/schema"
)

const exportDelta = `## ADDED Requirements

### Requirement: User can export data
The system SHALL allow users to export their data in CSV format.

#### Scenario: Successful export
- **WHEN** user clicks Export
- **THEN** a CSV downloads
`

func TestTraceabilityWarnsOnUnreferencedRequirement(t *testing.T) {
	dir := writeChange(t, map[string]string{
		"specs/data-export/spec.md": exportDelta,
		"tasks.md":                  "## 1. Work\n\n- [ ] 1.1 Something unrelated\n",
	})
	issues := TaskTraceability(dir, "tasks.md")
	if len(issues) != 2 || issues[0].Level != Error || issues[1].Level != Warning {
		t.Fatalf("issues = %+v", issues)
	}
	if !strings.Contains(issues[1].Message, `"User can export data"`) ||
		!strings.Contains(issues[1].Message, "data-export") {
		t.Fatalf("message = %q", issues[1].Message)
	}
}

func TestTraceabilityAllReferenced(t *testing.T) {
	dir := writeChange(t, map[string]string{
		"specs/data-export/spec.md": exportDelta,
		"tasks.md":                  "## 1. Work\n\n- [ ] 1.1 Implement export (req: User can export data)\n",
	})
	if issues := TaskTraceability(dir, "tasks.md"); len(issues) != 0 {
		t.Fatalf("issues = %+v", issues)
	}
}

func TestTraceabilityWhitespaceNormalizedMatch(t *testing.T) {
	dir := writeChange(t, map[string]string{
		"specs/data-export/spec.md": exportDelta,
		"tasks.md":                  "## 1. Work\n\n- [ ] 1.1 Implement export (req:  User   can export data )\n",
	})
	if issues := TaskTraceability(dir, "tasks.md"); len(issues) != 0 {
		t.Fatalf("issues = %+v", issues)
	}
}

func TestTraceabilityCaseSensitive(t *testing.T) {
	dir := writeChange(t, map[string]string{
		"specs/data-export/spec.md": exportDelta,
		"tasks.md":                  "## 1. Work\n\n- [ ] 1.1 Implement export (req: user can export data)\n",
	})

	if issues := TaskTraceability(dir, "tasks.md"); len(issues) != 2 {
		t.Fatalf("issues = %+v", issues)
	}
}

func TestTraceabilityDanglingMarker(t *testing.T) {
	dir := writeChange(t, map[string]string{
		"specs/data-export/spec.md": exportDelta,
		"tasks.md": "## 1. Work\n\n- [ ] 1.1 Implement export (req: User can export data)\n" +
			"- [ ] 1.2 Ghost work (req: X)\n",
	})
	issues := TaskTraceability(dir, "tasks.md")
	if len(issues) != 1 || !strings.Contains(issues[0].Message, "(req: X)") {
		t.Fatalf("issues = %+v", issues)
	}
}

func TestTraceabilityMissingTasksFileIsSilent(t *testing.T) {
	dir := writeChange(t, map[string]string{
		"specs/data-export/spec.md": exportDelta,
	})
	if issues := TaskTraceability(dir, "tasks.md"); issues != nil {
		t.Fatalf("issues = %+v", issues)
	}
}

func TestTraceabilityModifiedBlocksCount(t *testing.T) {
	dir := writeChange(t, map[string]string{
		"specs/auth/spec.md": `## MODIFIED Requirements

### Requirement: User Login
The system SHALL allow login with MFA.

#### Scenario: MFA required
- **WHEN** a user logs in
- **THEN** MFA is requested
`,
		"tasks.md": "## 1. Work\n\n- [ ] 1.1 Nothing here\n",
	})
	issues := TaskTraceability(dir, "tasks.md")
	if len(issues) != 2 || !strings.Contains(issues[1].Message, `"User Login"`) {
		t.Fatalf("issues = %+v", issues)
	}
}

func TestTraceabilityStrictFailsReportButKeepsWarningLevel(t *testing.T) {
	issues := []Issue{{Level: Warning, Path: "task-traceability", Message: "x"}}
	lax := NewReport(issues, false)
	strict := NewReport(issues, true)
	if !lax.Valid || strict.Valid {
		t.Fatalf("lax=%v strict=%v", lax.Valid, strict.Valid)
	}
	if strict.Issues[0].Level != Warning {
		t.Fatalf("strict relabeled the issue: %v", strict.Issues[0].Level)
	}
}

func TestTraceabilityRemovedAndRenamedMarkersNotDangling(t *testing.T) {
	dir := writeChange(t, map[string]string{
		"specs/auth/spec.md": `## REMOVED Requirements

### Requirement: Old export
**Reason**: obsolete
**Migration**: none

## RENAMED Requirements

- FROM: ` + "`### Requirement: Login`" + `
- TO: ` + "`### Requirement: Sign in`" + `
`,
		"tasks.md": "## 1. Work\n\n- [ ] 1.1 Delete endpoint (req: Old export)\n- [ ] 1.2 Rename docs (req: Sign in)\n",
	})
	if issues := TaskTraceability(dir, "tasks.md"); len(issues) != 0 {
		t.Fatalf("issues = %+v", issues)
	}
}

func TestTaskHygieneMarkerlessLine(t *testing.T) {
	dir := writeChange(t, map[string]string{
		"specs/data-export/spec.md": exportDelta,
		"tasks.md": "## 1. Work\n\n- [ ] 1.1 Implement export (req: User can export data)\n" +
			"- [x] 1.2 Wire the endpoint\n",
	})
	issues := TaskTraceability(dir, "tasks.md")
	if len(issues) != 1 || issues[0].Level != Error {
		t.Fatalf("issues = %+v", issues)
	}
	if !strings.Contains(issues[0].Message, "line 4") || !strings.Contains(issues[0].Message, "Wire the endpoint") {
		t.Fatalf("message = %q", issues[0].Message)
	}
}

func TestTaskHygieneDuplicateText(t *testing.T) {
	dir := writeChange(t, map[string]string{
		"specs/data-export/spec.md": exportDelta,
		"tasks.md": "## 1. Work\n\n- [ ] 1.1 Add retry logic (req: User can export data)\n" +
			"\n## 3. Later\n\n- [ ] 3.2 Add retry logic (req: User can export data)\n",
	})
	issues := TaskTraceability(dir, "tasks.md")
	if len(issues) != 1 || issues[0].Level != Error {
		t.Fatalf("issues = %+v", issues)
	}
	if !strings.Contains(issues[0].Message, `"Add retry logic"`) ||
		!strings.Contains(issues[0].Message, "3, 7") {
		t.Fatalf("message = %q", issues[0].Message)
	}
}

func TestTaskHygieneDistinctTasksClean(t *testing.T) {
	dir := writeChange(t, map[string]string{
		"specs/data-export/spec.md": exportDelta,
		"tasks.md": "## 1. Work\n\nProse here stays exempt.\n\n- [ ] 1.1 Add retry logic (req: User can export data)\n" +
			"- [ ] 1.2 Document retry logic (req: User can export data)\n",
	})
	if issues := TaskTraceability(dir, "tasks.md"); len(issues) != 0 {
		t.Fatalf("issues = %+v", issues)
	}
}

func TestTraceabilityForSchemaGating(t *testing.T) {
	dir := writeChange(t, map[string]string{
		"specs/data-export/spec.md": exportDelta,
		"tasks.md":                  "## 1. Work\n\n- [ ] 1.1 No markers here\n",
	})

	specDriven, err := schema.Resolve("spec-driven", "")
	if err != nil {
		t.Fatal(err)
	}
	if issues := TaskTraceabilityForSchema(specDriven, dir); issues != nil {
		t.Fatalf("spec-driven issues = %+v", issues)
	}
	teamDriven, err := schema.Resolve("team-driven", "")
	if err != nil {
		t.Fatal(err)
	}
	if issues := TaskTraceabilityForSchema(teamDriven, dir); len(issues) != 2 {
		t.Fatalf("team-driven issues = %+v", issues)
	}
	if TaskTraceabilityForSchema(nil, dir) != nil {
		t.Fatal("nil schema must be silent")
	}
}
