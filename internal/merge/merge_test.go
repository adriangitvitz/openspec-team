package merge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adriangitvitz/openspec-team/internal/parser"
)

const baseSpec = `# auth Specification

## Purpose
Users authenticate with the system so their data stays private forever.

## Requirements

### Requirement: User Login
The system SHALL allow users to log in with email and password.

#### Scenario: Valid credentials
- **WHEN** a user submits valid credentials
- **THEN** a session is created

### Requirement: Session Expiry
The session MUST expire after 30 minutes.

#### Scenario: Timeout
- **WHEN** 30 minutes pass
- **THEN** the session is invalidated

## Notes
Keep this section.
`

func setup(t *testing.T, deltaContent, targetContent string) SpecUpdate {
	t.Helper()
	dir := t.TempDir()
	source := filepath.Join(dir, "change", "specs", "auth", "spec.md")
	target := filepath.Join(dir, "main", "auth", "spec.md")
	if err := os.MkdirAll(filepath.Dir(source), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte(deltaContent), 0o644); err != nil {
		t.Fatal(err)
	}
	exists := false
	if targetContent != "" {
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte(targetContent), 0o644); err != nil {
			t.Fatal(err)
		}
		exists = true
	}
	return SpecUpdate{Source: source, Target: target, Exists: exists}
}

func TestBuildUpdatedSpecAdded(t *testing.T) {
	delta := `## ADDED Requirements

### Requirement: Two-Factor Auth
The system SHALL require a second factor.

#### Scenario: TOTP
- **WHEN** a user logs in
- **THEN** a TOTP prompt is shown
`
	update := setup(t, delta, baseSpec)
	result, err := BuildUpdatedSpec(update, "add-2fa")
	if err != nil {
		t.Fatal(err)
	}
	if result.Counts != (Counts{Added: 1}) {
		t.Fatalf("counts = %+v", result.Counts)
	}
	if !strings.Contains(result.Rebuilt, "### Requirement: Two-Factor Auth") {
		t.Fatalf("rebuilt missing added requirement:\n%s", result.Rebuilt)
	}
	for _, want := range []string{"### Requirement: User Login", "### Requirement: Session Expiry", "## Notes"} {
		if !strings.Contains(result.Rebuilt, want) {
			t.Fatalf("rebuilt lost %q:\n%s", want, result.Rebuilt)
		}
	}
	if strings.Index(result.Rebuilt, "Two-Factor Auth") < strings.Index(result.Rebuilt, "Session Expiry") {
		t.Fatal("added requirement should be appended at the end")
	}
}

func TestBuildUpdatedSpecModified(t *testing.T) {
	delta := `## MODIFIED Requirements

### Requirement: User Login
The system SHALL allow users to log in with email, password, and passkey.

#### Scenario: Valid credentials
- **WHEN** a user submits valid credentials
- **THEN** a session is created

#### Scenario: Passkey
- **WHEN** a passkey is offered
- **THEN** login succeeds
`
	update := setup(t, delta, baseSpec)
	result, err := BuildUpdatedSpec(update, "modify-login")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Rebuilt, "passkey") {
		t.Fatal("modified content missing")
	}
	if strings.Index(result.Rebuilt, "User Login") > strings.Index(result.Rebuilt, "Session Expiry") {
		t.Fatal("modified requirement should keep its position")
	}
}

func TestBuildUpdatedSpecModifiedDropsScenarioBlocked(t *testing.T) {

	delta := `## MODIFIED Requirements

### Requirement: User Login
The system SHALL allow users to log in via SSO only.

#### Scenario: SSO
- **WHEN** SSO is configured
- **THEN** login goes through the IdP
`
	update := setup(t, delta, baseSpec)
	_, err := BuildUpdatedSpec(update, "modify-login")
	if err == nil || !strings.Contains(err.Error(), "not present in the modified block") {
		t.Fatalf("err = %v", err)
	}
}

func TestBuildUpdatedSpecRemovedAndRenamed(t *testing.T) {
	delta := "## REMOVED Requirements\n\n- `### Requirement: User Login`\n\n## RENAMED Requirements\n\n- FROM: `### Requirement: Session Expiry`\n- TO: `### Requirement: Session Timeout`\n"
	update := setup(t, delta, baseSpec)
	result, err := BuildUpdatedSpec(update, "cleanup")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(result.Rebuilt, "User Login") {
		t.Fatal("removed requirement still present")
	}
	if !strings.Contains(result.Rebuilt, "### Requirement: Session Timeout") {
		t.Fatal("renamed header missing")
	}
	if strings.Contains(result.Rebuilt, "Session Expiry") {
		t.Fatal("old name still present")
	}
	if result.Counts != (Counts{Removed: 1, Renamed: 1}) {
		t.Fatalf("counts = %+v", result.Counts)
	}
}

func TestBuildUpdatedSpecErrorBranches(t *testing.T) {
	cases := []struct {
		name    string
		delta   string
		target  string
		wantErr string
	}{
		{
			name:    "no operations",
			delta:   "just prose\n",
			target:  baseSpec,
			wantErr: "found no operations",
		},
		{
			name:    "modified not found",
			delta:   "## MODIFIED Requirements\n\n### Requirement: Ghost\nThe system SHALL x.\n\n#### Scenario: X\n- WHEN a\n- THEN b\n",
			target:  baseSpec,
			wantErr: "MODIFIED failed",
		},
		{
			name:    "removed not found",
			delta:   "## REMOVED Requirements\n\n- `### Requirement: Ghost`\n",
			target:  baseSpec,
			wantErr: "REMOVED failed",
		},
		{
			name:    "renamed source missing",
			delta:   "## RENAMED Requirements\n\n- FROM: `### Requirement: Ghost`\n- TO: `### Requirement: New Name`\n",
			target:  baseSpec,
			wantErr: "source not found",
		},
		{
			name:    "renamed target exists",
			delta:   "## RENAMED Requirements\n\n- FROM: `### Requirement: User Login`\n- TO: `### Requirement: Session Expiry`\n",
			target:  baseSpec,
			wantErr: "target already exists",
		},
		{
			name:    "added already exists",
			delta:   "## ADDED Requirements\n\n### Requirement: User Login\nThe system SHALL x.\n\n#### Scenario: X\n- WHEN a\n- THEN b\n",
			target:  baseSpec,
			wantErr: "already exists",
		},
		{
			name:    "modified on new spec",
			delta:   "## MODIFIED Requirements\n\n### Requirement: User Login\nThe system SHALL x.\n\n#### Scenario: X\n- WHEN a\n- THEN b\n",
			target:  "",
			wantErr: "only ADDED requirements are allowed",
		},
		{
			name:    "duplicate in ADDED",
			delta:   "## ADDED Requirements\n\n### Requirement: Same\nThe system SHALL a.\n\n#### Scenario: A\n- WHEN a\n- THEN b\n\n### Requirement: Same\nThe system SHALL b.\n\n#### Scenario: B\n- WHEN a\n- THEN b\n",
			target:  baseSpec,
			wantErr: "duplicate requirement in ADDED",
		},
		{
			name:    "cross-section MODIFIED and REMOVED",
			delta:   "## MODIFIED Requirements\n\n### Requirement: User Login\nThe system SHALL x.\n\n#### Scenario: Valid credentials\n- WHEN a\n- THEN b\n\n## REMOVED Requirements\n\n- `### Requirement: User Login`\n",
			target:  baseSpec,
			wantErr: "multiple sections",
		},
		{
			name:    "structurally invalid target",
			delta:   "## ADDED Requirements\n\n### Requirement: X\nThe system SHALL x.\n\n#### Scenario: X\n- WHEN a\n- THEN b\n",
			target:  baseSpec + "\n## ADDED Requirements\n",
			wantErr: "structurally invalid",
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			update := setup(t, tt.delta, tt.target)
			_, err := BuildUpdatedSpec(update, "test-change")
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestBuildUpdatedSpecNewSpecSkeleton(t *testing.T) {
	delta := `## ADDED Requirements

### Requirement: Data Export
The system SHALL export data as CSV.

#### Scenario: Export
- **WHEN** the user clicks export
- **THEN** a CSV downloads

## REMOVED Requirements

- ` + "`### Requirement: Not There`" + `
`
	update := setup(t, delta, "")
	result, err := BuildUpdatedSpec(update, "add-export")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Rebuilt, "# auth Specification") {
		t.Fatalf("skeleton missing:\n%s", result.Rebuilt)
	}
	if len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "REMOVED requirement(s) ignored") {
		t.Fatalf("warnings = %v", result.Warnings)
	}
}

const modifyLoginDelta = `## MODIFIED Requirements

### Requirement: User Login
The system SHALL allow users to log in with email, password, and passkey.

#### Scenario: Valid credentials
- **WHEN** a user submits valid credentials
- **THEN** a session is created
`

func TestFingerprintCaptureAndVerifyClean(t *testing.T) {
	update := setup(t, modifyLoginDelta, baseSpec)
	changeDir := filepath.Dir(filepath.Dir(filepath.Dir(update.Source)))
	mainSpecsDir := filepath.Dir(filepath.Dir(update.Target))

	if err := CaptureFingerprints(changeDir, mainSpecsDir, false); err != nil {
		t.Fatal(err)
	}
	meta, err := LoadMeta(changeDir)
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := meta.Fingerprints["auth"]
	if !ok || !strings.HasPrefix(entry.Requirements["User Login"], "sha256:") {
		t.Fatalf("meta = %+v", meta)
	}

	content, _ := os.ReadFile(update.Source)
	conflicts, checked, err := VerifyFingerprints(changeDir, update, parser.ParseDeltaSpec(string(content)))
	if err != nil || !checked || len(conflicts) != 0 {
		t.Fatalf("conflicts=%v checked=%v err=%v", conflicts, checked, err)
	}
}

func TestFingerprintConflictDetected(t *testing.T) {
	update := setup(t, modifyLoginDelta, baseSpec)
	changeDir := filepath.Dir(filepath.Dir(filepath.Dir(update.Source)))
	mainSpecsDir := filepath.Dir(filepath.Dir(update.Target))

	if err := CaptureFingerprints(changeDir, mainSpecsDir, false); err != nil {
		t.Fatal(err)
	}

	edited := strings.Replace(baseSpec,
		"The system SHALL allow users to log in with email and password.",
		"The system SHALL allow users to log in with SSO.", 1)
	if err := os.WriteFile(update.Target, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}

	content, _ := os.ReadFile(update.Source)
	conflicts, checked, err := VerifyFingerprints(changeDir, update, parser.ParseDeltaSpec(string(content)))
	if err != nil || !checked {
		t.Fatalf("checked=%v err=%v", checked, err)
	}
	if len(conflicts) != 1 || conflicts[0].Requirement != "User Login" {
		t.Fatalf("conflicts = %+v", conflicts)
	}
}

func TestFingerprintFirstTouchWins(t *testing.T) {
	update := setup(t, modifyLoginDelta, baseSpec)
	changeDir := filepath.Dir(filepath.Dir(filepath.Dir(update.Source)))
	mainSpecsDir := filepath.Dir(filepath.Dir(update.Target))

	if err := CaptureFingerprints(changeDir, mainSpecsDir, false); err != nil {
		t.Fatal(err)
	}
	before, _ := LoadMeta(changeDir)

	edited := strings.Replace(baseSpec, "email and password", "SSO", 1)
	os.WriteFile(update.Target, []byte(edited), 0o644)
	if err := CaptureFingerprints(changeDir, mainSpecsDir, false); err != nil {
		t.Fatal(err)
	}
	after, _ := LoadMeta(changeDir)
	if before.Fingerprints["auth"].Requirements["User Login"] != after.Fingerprints["auth"].Requirements["User Login"] {
		t.Fatal("capture overwrote an existing fingerprint")
	}

	if err := CaptureFingerprints(changeDir, mainSpecsDir, true); err != nil {
		t.Fatal(err)
	}
	refreshed, _ := LoadMeta(changeDir)
	if before.Fingerprints["auth"].Requirements["User Login"] == refreshed.Fingerprints["auth"].Requirements["User Login"] {
		t.Fatal("refresh did not re-baseline")
	}
}

func TestFingerprintAlreadySyncedNoConflict(t *testing.T) {
	update := setup(t, modifyLoginDelta, baseSpec)
	changeDir := filepath.Dir(filepath.Dir(filepath.Dir(update.Source)))
	mainSpecsDir := filepath.Dir(filepath.Dir(update.Target))
	CaptureFingerprints(changeDir, mainSpecsDir, false)

	content, _ := os.ReadFile(update.Source)
	plan := parser.ParseDeltaSpec(string(content))
	synced := strings.Replace(baseSpec,
		"### Requirement: User Login\nThe system SHALL allow users to log in with email and password.\n\n#### Scenario: Valid credentials\n- **WHEN** a user submits valid credentials\n- **THEN** a session is created",
		plan.Modified[0].Raw, 1)
	if !strings.Contains(synced, "passkey") {
		t.Fatal("test setup: replacement failed")
	}
	os.WriteFile(update.Target, []byte(synced), 0o644)

	conflicts, checked, err := VerifyFingerprints(changeDir, update, plan)
	if err != nil || !checked {
		t.Fatalf("checked=%v err=%v", checked, err)
	}
	if len(conflicts) != 0 {
		t.Fatalf("already-synced base flagged as conflict: %+v", conflicts)
	}
}

func TestVerifyWithoutMetaNotChecked(t *testing.T) {
	update := setup(t, modifyLoginDelta, baseSpec)
	changeDir := filepath.Dir(filepath.Dir(filepath.Dir(update.Source)))
	content, _ := os.ReadFile(update.Source)
	_, checked, err := VerifyFingerprints(changeDir, update, parser.ParseDeltaSpec(string(content)))
	if err != nil || checked {
		t.Fatalf("checked=%v err=%v", checked, err)
	}
}

func TestHashRequirementNormalizes(t *testing.T) {
	a := HashRequirement("### Requirement: X\r\nBody.  \n")
	b := HashRequirement("### Requirement: X\nBody.")
	if a != b {
		t.Fatal("hash should normalize line endings and trailing whitespace")
	}
}

func TestVerifyAddedOnlyVacuouslyChecked(t *testing.T) {
	addedOnly := "## ADDED Requirements\n\n### Requirement: New Thing\nThe system SHALL new.\n\n#### Scenario: X\n- WHEN a\n- THEN b\n"
	update := setup(t, addedOnly, baseSpec)
	changeDir := filepath.Dir(filepath.Dir(filepath.Dir(update.Source)))
	content, _ := os.ReadFile(update.Source)
	conflicts, checked, err := VerifyFingerprints(changeDir, update, parser.ParseDeltaSpec(string(content)))
	if err != nil || !checked || len(conflicts) != 0 {
		t.Fatalf("conflicts=%v checked=%v err=%v", conflicts, checked, err)
	}
}
