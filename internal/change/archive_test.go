package change

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/adriangitvitz/openspec-go/internal/core"
	"github.com/adriangitvitz/openspec-go/internal/merge"
)

const archBaseSpec = `# auth Specification

## Purpose
Users authenticate with the system so their data stays private forever.

## Requirements

### Requirement: User Login
The system SHALL allow users to log in with email and password.

#### Scenario: Valid credentials
- **WHEN** a user submits valid credentials
- **THEN** a session is created
`

const archDelta = `## MODIFIED Requirements

### Requirement: User Login
The system SHALL allow users to log in with email, password, and passkey.

#### Scenario: Valid credentials
- **WHEN** a user submits valid credentials
- **THEN** a session is created
`

func fixedNow() time.Time {
	return time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
}

func setupArchiveProject(t *testing.T) (root string, ctx *Context) {
	t.Helper()
	root = scaffoldProject(t)
	write(t, root, "openspec/specs/auth/spec.md", archBaseSpec)
	if _, err := Create(root, "modify-login", CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	write(t, root, "openspec/changes/modify-login/proposal.md", "## Why\nBecause.")
	write(t, root, "openspec/changes/modify-login/tasks.md", "- [x] 1.1 Done\n")
	write(t, root, "openspec/changes/modify-login/specs/auth/spec.md", archDelta)

	ctx, err := LoadContext(root, "modify-login", "")
	if err != nil {
		t.Fatal(err)
	}
	return root, ctx
}

func TestArchiveHappyPath(t *testing.T) {
	root, ctx := setupArchiveProject(t)
	if err := merge.CaptureFingerprints(ctx.ChangeDir, core.SpecsDir(root), false); err != nil {
		t.Fatal(err)
	}

	if err := CheckArchiveReady(ctx, ArchiveOptions{}); err != nil {
		t.Fatal(err)
	}
	result, err := Archive(ctx, ArchiveOptions{Now: fixedNow})
	if err != nil {
		t.Fatal(err)
	}
	if result.ArchivedAs != "2026-07-10-modify-login" {
		t.Fatalf("archivedAs = %q", result.ArchivedAs)
	}
	if result.Totals != (merge.Counts{Modified: 1}) {
		t.Fatalf("totals = %+v", result.Totals)
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("warnings = %v", result.Warnings)
	}

	updated, _ := os.ReadFile(filepath.Join(root, "openspec/specs/auth/spec.md"))
	if !strings.Contains(string(updated), "passkey") {
		t.Fatal("spec not updated")
	}
	if _, err := os.Stat(ctx.ChangeDir); !os.IsNotExist(err) {
		t.Fatal("change dir still present")
	}
	if _, err := os.Stat(filepath.Join(core.ArchiveDir(root), "2026-07-10-modify-login", "proposal.md")); err != nil {
		t.Fatal("archived content missing")
	}
}

func TestArchiveBlocksOnFingerprintConflict(t *testing.T) {
	root, ctx := setupArchiveProject(t)
	if err := merge.CaptureFingerprints(ctx.ChangeDir, core.SpecsDir(root), false); err != nil {
		t.Fatal(err)
	}

	edited := strings.Replace(archBaseSpec, "email and password", "SSO", 1)
	write(t, root, "openspec/specs/auth/spec.md", edited)

	_, err := Archive(ctx, ArchiveOptions{Now: fixedNow})
	if err == nil || !strings.Contains(err.Error(), "archive blocked") {
		t.Fatalf("err = %v", err)
	}

	current, _ := os.ReadFile(filepath.Join(root, "openspec/specs/auth/spec.md"))
	if string(current) != edited {
		t.Fatal("spec was modified despite the conflict")
	}
	if _, err := os.Stat(ctx.ChangeDir); err != nil {
		t.Fatal("change dir was moved despite the conflict")
	}

	newDelta := strings.Replace(archDelta, "email, password, and passkey", "SSO and passkey", 1)
	write(t, root, "openspec/changes/modify-login/specs/auth/spec.md", newDelta)
	if err := merge.CaptureFingerprints(ctx.ChangeDir, core.SpecsDir(root), true); err != nil {
		t.Fatal(err)
	}
	if _, err := Archive(ctx, ArchiveOptions{Now: fixedNow}); err != nil {
		t.Fatalf("archive after refresh failed: %v", err)
	}
}

func TestArchiveIncompleteTasks(t *testing.T) {
	_, ctx := setupArchiveProject(t)
	write(t, ctx.Root, "openspec/changes/modify-login/tasks.md", "- [ ] 1.1 Not done\n")
	err := CheckArchiveReady(ctx, ArchiveOptions{})
	var incomplete *IncompleteTasksError
	if err == nil {
		t.Fatal("expected incomplete-tasks error")
	}
	if !asIncomplete(err, &incomplete) || incomplete.Remaining != 1 {
		t.Fatalf("err = %v", err)
	}
}

func asIncomplete(err error, target **IncompleteTasksError) bool {
	e, ok := err.(*IncompleteTasksError)
	if ok {
		*target = e
	}
	return ok
}

func TestArchiveValidationFailureBlocks(t *testing.T) {
	_, ctx := setupArchiveProject(t)

	write(t, ctx.Root, "openspec/changes/modify-login/specs/auth/spec.md",
		"## MODIFIED Requirements\n\n### Requirement: User Login\nThe system SHALL log in.\n")
	err := CheckArchiveReady(ctx, ArchiveOptions{})
	if err == nil || !strings.Contains(err.Error(), "failed validation") {
		t.Fatalf("err = %v", err)
	}
}

func TestArchiveSkipSpecs(t *testing.T) {
	root, ctx := setupArchiveProject(t)
	result, err := Archive(ctx, ArchiveOptions{SkipSpecs: true, Now: fixedNow})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.SpecsUpdated) != 0 {
		t.Fatalf("specsUpdated = %v", result.SpecsUpdated)
	}
	original, _ := os.ReadFile(filepath.Join(root, "openspec/specs/auth/spec.md"))
	if string(original) != archBaseSpec {
		t.Fatal("spec should be untouched with --skip-specs")
	}
}

func TestArchiveNoFingerprintsWarns(t *testing.T) {
	_, ctx := setupArchiveProject(t)
	result, err := Archive(ctx, ArchiveOptions{Now: fixedNow})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "no fingerprints") {
		t.Fatalf("warnings = %v", result.Warnings)
	}
}

func TestArchiveNotBlockedByTraceabilityWarnings(t *testing.T) {

	root := scaffoldProject(t)
	if _, err := Create(root, "team-change", CreateOptions{Schema: "team-driven"}); err != nil {
		t.Fatal(err)
	}
	write(t, root, "openspec/changes/team-change/proposal.md", "## Why\nBecause.")
	write(t, root, "openspec/changes/team-change/tasks.md", "- [x] 1.1 Done without markers\n")
	write(t, root, "openspec/changes/team-change/specs/export/spec.md", `## ADDED Requirements

### Requirement: User can export data
The system SHALL allow users to export their data.

#### Scenario: Export
- **WHEN** the user exports
- **THEN** a file downloads
`)
	ctx, err := LoadContext(root, "team-change", "")
	if err != nil {
		t.Fatal(err)
	}
	result, err := Archive(ctx, ArchiveOptions{Now: fixedNow})
	if err != nil {
		t.Fatalf("archive blocked: %v", err)
	}
	if result == nil {
		t.Fatal("no archive result")
	}
}
