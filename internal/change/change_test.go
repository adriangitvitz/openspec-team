package change

import (
	"os"
	"path/filepath"
	"testing"
)

func scaffoldProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, dir := range []string{"openspec/specs", "openspec/changes/archive"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "openspec", "config.yaml"), []byte("schema: spec-driven\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func write(t *testing.T, root, rel, content string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCreateAndLoadContext(t *testing.T) {
	root := scaffoldProject(t)

	result, err := Create(root, "add-auth", CreateOptions{Goal: "secure login"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Schema != "spec-driven" {
		t.Fatalf("schema = %q", result.Schema)
	}
	if _, err := os.Stat(result.MetadataPath); err != nil {
		t.Fatal(err)
	}

	if _, err := Create(root, "add-auth", CreateOptions{}); err == nil {
		t.Fatal("expected duplicate error")
	}
	if _, err := Create(root, "Add_Auth", CreateOptions{}); err == nil {
		t.Fatal("expected name error")
	}

	ctx, err := LoadContext(root, "add-auth", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(ctx.Completed) != 0 {
		t.Fatalf("completed = %v", ctx.Completed)
	}
}

func TestBuildStatusProgression(t *testing.T) {
	root := scaffoldProject(t)
	if _, err := Create(root, "add-auth", CreateOptions{}); err != nil {
		t.Fatal(err)
	}

	ctx, _ := LoadContext(root, "add-auth", "")
	status := BuildStatus(ctx)
	if status.IsComplete {
		t.Fatal("fresh change should not be complete")
	}
	byID := map[string]string{}
	for _, a := range status.Artifacts {
		byID[a.ID] = a.Status
	}
	if byID["proposal"] != "ready" || byID["specs"] != "blocked" || byID["tasks"] != "blocked" {
		t.Fatalf("statuses = %v", byID)
	}

	write(t, root, "openspec/changes/add-auth/proposal.md", "## Why\nBecause.")
	ctx, _ = LoadContext(root, "add-auth", "")
	status = BuildStatus(ctx)
	byID = map[string]string{}
	for _, a := range status.Artifacts {
		byID[a.ID] = a.Status
	}
	if byID["proposal"] != "done" || byID["specs"] != "ready" || byID["design"] != "ready" {
		t.Fatalf("statuses = %v", byID)
	}
}

func TestBuildInstructions(t *testing.T) {
	root := scaffoldProject(t)
	Create(root, "add-auth", CreateOptions{})
	ctx, _ := LoadContext(root, "add-auth", "")

	in, err := BuildInstructions(ctx, "specs")
	if err != nil {
		t.Fatal(err)
	}
	if in.Template == "" || in.OutputPath != "specs/**/*.md" {
		t.Fatalf("instructions = %+v", in)
	}
	if len(in.Dependencies) != 1 || in.Dependencies[0].ID != "proposal" || in.Dependencies[0].Done {
		t.Fatalf("deps = %+v", in.Dependencies)
	}

	if _, err := BuildInstructions(ctx, "ghost"); err == nil {
		t.Fatal("expected unknown-artifact error")
	}
}

func TestBuildApplyInstructionsStates(t *testing.T) {
	root := scaffoldProject(t)
	Create(root, "add-auth", CreateOptions{})

	ctx, _ := LoadContext(root, "add-auth", "")
	in, _ := BuildApplyInstructions(ctx)
	if in.State != "blocked" || len(in.MissingArtifacts) == 0 {
		t.Fatalf("apply = %+v", in)
	}

	write(t, root, "openspec/changes/add-auth/tasks.md", "- [ ] 1.1 First\n- [x] 1.2 Second\n")
	ctx, _ = LoadContext(root, "add-auth", "")
	in, _ = BuildApplyInstructions(ctx)
	if in.State != "ready" || in.Progress.Total != 2 || in.Progress.Complete != 1 {
		t.Fatalf("apply = %+v", in)
	}

	write(t, root, "openspec/changes/add-auth/tasks.md", "- [x] 1.1 First\n- [x] 1.2 Second\n")
	ctx, _ = LoadContext(root, "add-auth", "")
	in, _ = BuildApplyInstructions(ctx)
	if in.State != "all_done" {
		t.Fatalf("apply = %+v", in)
	}
}

func TestListChangesAndSpecs(t *testing.T) {
	root := scaffoldProject(t)
	Create(root, "add-auth", CreateOptions{})
	write(t, root, "openspec/changes/add-auth/tasks.md", "- [x] 1.1 Done\n")
	write(t, root, "openspec/specs/auth/spec.md", "## Purpose\nAuth.\n\n## Requirements\n\n### Requirement: Login\nThe system SHALL log in.\n\n#### Scenario: X\n- WHEN a\n- THEN b\n")

	changes := ListChanges(root, "name")
	if len(changes) != 1 || changes[0].Status != "complete" {
		t.Fatalf("changes = %+v", changes)
	}
	specs := ListSpecs(root)
	if len(specs) != 1 || specs[0].RequirementCount != 1 {
		t.Fatalf("specs = %+v", specs)
	}
}

func TestLoadContextUnknownChange(t *testing.T) {
	root := scaffoldProject(t)
	if _, err := LoadContext(root, "ghost", ""); err == nil {
		t.Fatal("expected error")
	}
}

func TestInstructionsCarryKnowledge(t *testing.T) {
	root := scaffoldProject(t)
	write(t, root, "openspec/config.yaml", `schema: spec-driven
knowledge:
  - topic: ITK / material resolution
    paths: [docs/adr/adr-0001.md]
`)
	Create(root, "add-auth", CreateOptions{})
	ctx, _ := LoadContext(root, "add-auth", "")

	in, err := BuildInstructions(ctx, "proposal")
	if err != nil {
		t.Fatal(err)
	}
	if len(in.Knowledge) != 1 || in.Knowledge[0].Topic != "ITK / material resolution" {
		t.Fatalf("knowledge = %+v", in.Knowledge)
	}
	apply, err := BuildApplyInstructions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(apply.Knowledge) != 1 {
		t.Fatalf("apply knowledge = %+v", apply.Knowledge)
	}
}

func TestDeepSchemaBlocksProposalUntilResearch(t *testing.T) {
	root := scaffoldProject(t)
	write(t, root, "openspec/config.yaml", "schema: spec-driven-deep\n")
	Create(root, "add-auth", CreateOptions{})

	ctx, _ := LoadContext(root, "add-auth", "")
	status := BuildStatus(ctx)
	byID := map[string]ArtifactStatus{}
	for _, a := range status.Artifacts {
		byID[a.ID] = a
	}
	if byID["research"].Status != "ready" {
		t.Fatalf("research = %+v", byID["research"])
	}
	if byID["proposal"].Status != "blocked" || byID["proposal"].MissingDeps[0] != "research" {
		t.Fatalf("proposal = %+v", byID["proposal"])
	}

	write(t, root, "openspec/changes/add-auth/research.md", "# Research\nevidence")
	ctx, _ = LoadContext(root, "add-auth", "")
	status = BuildStatus(ctx)
	for _, a := range status.Artifacts {
		if a.ID == "proposal" && a.Status != "ready" {
			t.Fatalf("proposal after research = %+v", a)
		}
	}
}
