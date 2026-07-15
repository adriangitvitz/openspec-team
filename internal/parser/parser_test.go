package parser

import (
	"reflect"
	"strings"
	"testing"
)

func TestBuildCodeFenceMask(t *testing.T) {
	lines := []string{
		"text",
		"```go",
		"## fake",
		"```",
		"~~~~",
		"~~~",
		"~~~~",
		"### visible",
	}
	mask := BuildCodeFenceMask(lines)
	want := []bool{false, true, true, true, true, true, true, false}
	if !reflect.DeepEqual(mask, want) {
		t.Fatalf("mask = %v, want %v", mask, want)
	}
}

func TestFenceCloseRequiresSameMarker(t *testing.T) {
	lines := []string{"```", "~~~", "still fenced", "```", "out"}
	mask := BuildCodeFenceMask(lines)
	want := []bool{true, true, true, true, false}
	if !reflect.DeepEqual(mask, want) {
		t.Fatalf("mask = %v, want %v", mask, want)
	}
}

func TestExtractRequirementBody(t *testing.T) {
	tests := []struct {
		name string
		body []string
		want string
	}{
		{
			name: "multi-line body joined, blanks skipped",
			body: []string{"The system SHALL", "", "  do the thing.  "},
			want: "The system SHALL\ndo the thing.",
		},
		{
			name: "stops at first header",
			body: []string{"Body line.", "#### Scenario: X", "- WHEN foo"},
			want: "Body line.",
		},
		{
			name: "fenced lines invisible, header after fence still breaks",
			body: []string{"```", "fenced text", "#### fake", "```", "Real body MUST hold.", "#### Scenario: Y"},
			want: "Real body MUST hold.",
		},
		{
			name: "metadata skipped when body text exists",
			body: []string{"**ID**: R-1", "The system SHALL foo."},
			want: "The system SHALL foo.",
		},
		{
			name: "metadata-only body keeps metadata",
			body: []string{"**Constraint**: The system MUST respond."},
			want: "**Constraint**: The system MUST respond.",
		},
		{
			name: "empty",
			body: []string{"", "   "},
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExtractRequirementBody(tt.body); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractRequirementTextHeaderFallback(t *testing.T) {
	got := ExtractRequirementText("The system SHALL log in users  ", []string{""})
	if got != "The system SHALL log in users" {
		t.Fatalf("got %q", got)
	}
}

func TestCountScenarios(t *testing.T) {
	body := []string{
		"Text",
		"#### Scenario: real",
		"- WHEN x",
		"```",
		"#### Scenario: fenced fake",
		"```",
		"#### Any level-4 header counts",
	}
	if got := CountScenarios(body); got != 2 {
		t.Fatalf("got %d, want 2", got)
	}
}

func TestContainsShallOrMust(t *testing.T) {
	for text, want := range map[string]bool{
		"The system SHALL foo": true,
		"It MUST bar":          true,
		"must lower":           false,
		"MARSHALL plan":        false,
		"MUSTARD":              false,
	} {
		if got := ContainsShallOrMust(text); got != want {
			t.Errorf("ContainsShallOrMust(%q) = %v, want %v", text, got, want)
		}
	}
}

const sampleSpec = `# user-auth Specification

## Purpose
Users authenticate with the system so their data stays private.

## Requirements

### Requirement: User Login
The system SHALL allow users to log in with email and password.

#### Scenario: Valid credentials
- **WHEN** a user submits valid credentials
- **THEN** a session is created

#### Scenario: Empty scenario dropped

### Requirement: Session Expiry
The session MUST expire after 30 minutes.

#### Scenario: Timeout
- **WHEN** 30 minutes pass
- **THEN** the session is invalidated

## Notes
Trailing section.
`

func TestParseSpec(t *testing.T) {
	spec, err := ParseSpec("user-auth", sampleSpec)
	if err != nil {
		t.Fatal(err)
	}
	if spec.Purpose != "Users authenticate with the system so their data stays private." {
		t.Fatalf("purpose = %q", spec.Purpose)
	}
	if len(spec.Requirements) != 2 {
		t.Fatalf("requirements = %d, want 2", len(spec.Requirements))
	}
	r0 := spec.Requirements[0]
	if r0.Name != "User Login" {
		t.Fatalf("name = %q", r0.Name)
	}
	if r0.Text != "The system SHALL allow users to log in with email and password." {
		t.Fatalf("text = %q", r0.Text)
	}

	if len(r0.Scenarios) != 1 {
		t.Fatalf("scenarios = %d, want 1", len(r0.Scenarios))
	}
	if spec.Requirements[1].Name != "Session Expiry" {
		t.Fatalf("name = %q", spec.Requirements[1].Name)
	}
}

func TestParseSpecErrors(t *testing.T) {
	if _, err := ParseSpec("x", "## Requirements\n### Requirement: A\nSHALL.\n"); err != ErrNoPurpose {
		t.Fatalf("want ErrNoPurpose, got %v", err)
	}
	if _, err := ParseSpec("x", "## Purpose\nSomething long enough here.\n"); err != ErrNoRequirements {
		t.Fatalf("want ErrNoRequirements, got %v", err)
	}
}

func TestFindMainSpecStructureIssues(t *testing.T) {
	content := strings.Join([]string{
		"# spec",
		"## ADDED Requirements",
		"## Requirements",
		"### Requirement: Inside",
		"## Other",
		"### Requirement: Outside",
		"```",
		"## MODIFIED Requirements",
		"### Requirement: FencedOut",
		"```",
	}, "\n")
	issues := FindMainSpecStructureIssues(content)
	if len(issues) != 2 {
		t.Fatalf("issues = %d (%+v), want 2", len(issues), issues)
	}
	if issues[0].Kind != IssueDeltaHeader || issues[0].Line != 2 {
		t.Fatalf("issue0 = %+v", issues[0])
	}
	if issues[1].Kind != IssueRequirementOutsideRequirements || issues[1].Line != 6 {
		t.Fatalf("issue1 = %+v", issues[1])
	}
}

func TestExtractRequirementsSectionCanonical(t *testing.T) {
	res := ExtractRequirementsSection("## Requirements\n### Requirement: Foo\nThe system SHALL foo.\n")
	if len(res.Blocks) != 1 || res.Blocks[0].Name != "Foo" {
		t.Fatalf("blocks = %+v", res.Blocks)
	}
}

func TestExtractRequirementsSectionHeaderVariants(t *testing.T) {
	for header, wantName := range map[string]string{
		"### requirement: Lowercase": "Lowercase",
		"### REQUIREMENT: Uppercase": "Uppercase",
		"### Requirement: Canonical": "Canonical",
		"###Requirement: NoSpace":    "NoSpace",
	} {
		res := ExtractRequirementsSection("## Requirements\n" + header + "\nThe system SHALL foo.\n")
		if len(res.Blocks) != 1 || res.Blocks[0].Name != wantName {
			t.Errorf("header %q: blocks = %+v", header, res.Blocks)
		}
	}
}

func TestExtractRequirementsSectionMultipleBlocksNoSpaceFirst(t *testing.T) {
	content := "## Requirements\n###Requirement: First\nThe system SHALL first.\n\n### Requirement: Second\nThe system SHALL second.\n"
	res := ExtractRequirementsSection(content)
	if len(res.Blocks) != 2 || res.Blocks[0].Name != "First" || res.Blocks[1].Name != "Second" {
		t.Fatalf("blocks = %+v", res.Blocks)
	}
}

func TestExtractRequirementsSectionMissing(t *testing.T) {
	res := ExtractRequirementsSection("# spec\n\n## Purpose\nSomething.\n")
	if res.HeaderLine != "## Requirements" || len(res.Blocks) != 0 {
		t.Fatalf("res = %+v", res)
	}
	if !strings.HasSuffix(res.Before, "\n\n") {
		t.Fatalf("before = %q", res.Before)
	}
	if res.After != "\n" {
		t.Fatalf("after = %q", res.After)
	}
}

func TestExtractRequirementsSectionRoundTripParts(t *testing.T) {
	res := ExtractRequirementsSection(sampleSpec)
	if res.Preamble != "" {
		t.Fatalf("preamble = %q", res.Preamble)
	}
	if len(res.Blocks) != 2 {
		t.Fatalf("blocks = %d", len(res.Blocks))
	}
	if !strings.Contains(res.After, "## Notes") {
		t.Fatalf("after = %q", res.After)
	}
	if !strings.HasPrefix(res.Blocks[0].Raw, "### Requirement: User Login") {
		t.Fatalf("raw = %q", res.Blocks[0].Raw)
	}
	if !strings.Contains(res.Blocks[0].Raw, "#### Scenario: Valid credentials") {
		t.Fatalf("raw should include scenarios: %q", res.Blocks[0].Raw)
	}
}

const sampleDelta = `# Delta for user-auth

## ADDED Requirements

### Requirement: Two-Factor Auth
The system SHALL require a second factor.

#### Scenario: TOTP
- **WHEN** a user logs in
- **THEN** a TOTP prompt is shown

### Not A Requirement Header
stray divider content

## MODIFIED Requirements

### Requirement: User Login
The system SHALL allow users to log in with email, password, and passkey.

#### Scenario: Passkey
- **WHEN** a passkey is offered
- **THEN** login succeeds

## REMOVED Requirements

- ` + "`### Requirement: Legacy Export`" + `
### Requirement: Plain Removed

## RENAMED Requirements

- FROM: ` + "`### Requirement: Session Expiry`" + `
- TO: ` + "`### Requirement: Session Timeout`" + `
`

func TestParseDeltaSpec(t *testing.T) {
	plan := ParseDeltaSpec(sampleDelta)

	if len(plan.Added) != 1 || plan.Added[0].Name != "Two-Factor Auth" {
		t.Fatalf("added = %+v", plan.Added)
	}
	if len(plan.Modified) != 1 || plan.Modified[0].Name != "User Login" {
		t.Fatalf("modified = %+v", plan.Modified)
	}
	if want := []string{"Legacy Export", "Plain Removed"}; !reflect.DeepEqual(plan.Removed, want) {
		t.Fatalf("removed = %v, want %v", plan.Removed, want)
	}
	if len(plan.Renamed) != 1 || plan.Renamed[0] != (Rename{From: "Session Expiry", To: "Session Timeout"}) {
		t.Fatalf("renamed = %+v", plan.Renamed)
	}
	if !plan.SectionPresence.Added || !plan.SectionPresence.Modified ||
		!plan.SectionPresence.Removed || !plan.SectionPresence.Renamed {
		t.Fatalf("presence = %+v", plan.SectionPresence)
	}
	if len(plan.SkippedHeaders) != 1 {
		t.Fatalf("skipped = %+v", plan.SkippedHeaders)
	}
	sk := plan.SkippedHeaders[0]
	if sk.Header != "Not A Requirement Header" || sk.Section != "ADDED Requirements" {
		t.Fatalf("skipped = %+v", sk)
	}

	if sk.Line != 12 {
		t.Fatalf("skipped line = %d, want 12", sk.Line)
	}

	if !strings.Contains(plan.Added[0].Raw, "stray divider content") {
		t.Fatalf("added raw = %q", plan.Added[0].Raw)
	}
}

func TestParseDeltaSpecNoSpaceHeaderRegression(t *testing.T) {
	plan := ParseDeltaSpec("## ADDED Requirements\n###Requirement: NoSpace\nThe system SHALL foo.\n")
	if len(plan.Added) != 1 || plan.Added[0].Name != "NoSpace" {
		t.Fatalf("added = %+v", plan.Added)
	}
}

func TestParseDeltaSpecCaseInsensitiveSectionTitles(t *testing.T) {
	plan := ParseDeltaSpec("## added requirements\n### Requirement: A\nThe system SHALL a.\n")
	if len(plan.Added) != 1 || !plan.SectionPresence.Added {
		t.Fatalf("plan = %+v", plan)
	}
}

func TestParseDeltaSpecFencedSkippedHeadersExcluded(t *testing.T) {
	content := "## ADDED Requirements\n### Requirement: A\nThe system SHALL a.\n```\n### fenced header\n```\n"
	plan := ParseDeltaSpec(content)
	if len(plan.SkippedHeaders) != 0 {
		t.Fatalf("skipped = %+v", plan.SkippedHeaders)
	}
}

func TestParseDeltaSpecEmptySections(t *testing.T) {
	plan := ParseDeltaSpec("## ADDED Requirements\n\nno blocks here\n")
	if len(plan.Added) != 0 || !plan.SectionPresence.Added || plan.SectionPresence.Modified {
		t.Fatalf("plan = %+v", plan)
	}
}

func TestCountTasks(t *testing.T) {
	content := strings.Join([]string{
		"- [ ] open task",
		"- [x] done task",
		"* [X] uppercase done",
		"  - [ ] indented: not counted",
		"- [] malformed: not counted",
		"regular line",
	}, "\n")
	p := CountTasks(content)
	if p.Total != 3 || p.Completed != 2 {
		t.Fatalf("progress = %+v", p)
	}
}

func TestParseTasks(t *testing.T) {
	tasks := ParseTasks("- [ ] 1.1 First\n- [x] 1.2 Second\n")
	if len(tasks) != 2 {
		t.Fatalf("tasks = %+v", tasks)
	}
	if tasks[0].Done || tasks[0].Description != "1.1 First" {
		t.Fatalf("task0 = %+v", tasks[0])
	}
	if !tasks[1].Done {
		t.Fatalf("task1 = %+v", tasks[1])
	}
}

func TestFormatTaskStatus(t *testing.T) {
	if got := FormatTaskStatus(TaskProgress{}); got != "No tasks" {
		t.Fatal(got)
	}
	if got := FormatTaskStatus(TaskProgress{Total: 2, Completed: 2}); got != "✓ Complete" {
		t.Fatal(got)
	}
	if got := FormatTaskStatus(TaskProgress{Total: 5, Completed: 2}); got != "2/5 tasks" {
		t.Fatal(got)
	}
}

func TestNormalizeLineEndings(t *testing.T) {
	if got := NormalizeLineEndings("a\r\nb\rc\n"); got != "a\nb\nc\n" {
		t.Fatalf("got %q", got)
	}
}
