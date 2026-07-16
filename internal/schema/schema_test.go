package schema

import (
	"reflect"
	"strings"
	"testing"
)

func TestResolveEmbeddedSpecDriven(t *testing.T) {
	s, err := Resolve("spec-driven", "")
	if err != nil {
		t.Fatal(err)
	}
	if s.Name != "spec-driven" || s.Version != 1 {
		t.Fatalf("schema = %s v%d", s.Name, s.Version)
	}
	if len(s.Artifacts) != 4 {
		t.Fatalf("artifacts = %d, want 4", len(s.Artifacts))
	}
	if s.Apply == nil || s.Apply.Tracks == nil || *s.Apply.Tracks != "tasks.md" {
		t.Fatalf("apply = %+v", s.Apply)
	}
	a, ok := s.TrackedTasksArtifact()
	if !ok || a.ID != "tasks" {
		t.Fatalf("tracked = %+v ok=%v", a, ok)
	}
	tmpl, err := s.TemplateContent(a)
	if err != nil || len(tmpl) == 0 {
		t.Fatalf("template err=%v len=%d", err, len(tmpl))
	}
}

func TestResolveNameNormalization(t *testing.T) {
	if _, err := Resolve("spec-driven.yaml", ""); err != nil {
		t.Fatal(err)
	}
}

func TestResolveUnknown(t *testing.T) {
	_, err := Resolve("nope", "")
	if err == nil || !strings.Contains(err.Error(), "spec-driven") {
		t.Fatalf("err = %v", err)
	}
}

func TestBuildOrderSpecDriven(t *testing.T) {
	s, err := Resolve("spec-driven", "")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"proposal", "design", "specs", "tasks"}
	if got := s.BuildOrder(); !reflect.DeepEqual(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

func TestGraphQueries(t *testing.T) {
	s, _ := Resolve("spec-driven", "")

	completed := map[string]bool{"proposal": true}
	if got := s.NextArtifacts(completed); !reflect.DeepEqual(got, []string{"design", "specs"}) {
		t.Fatalf("next = %v", got)
	}
	blocked := s.Blocked(completed)
	if !reflect.DeepEqual(blocked["tasks"], []string{"design", "specs"}) {
		t.Fatalf("blocked = %v", blocked)
	}
	if s.IsComplete(completed) {
		t.Fatal("should not be complete")
	}
	all := map[string]bool{"proposal": true, "specs": true, "design": true, "tasks": true}
	if !s.IsComplete(all) {
		t.Fatal("should be complete")
	}
}

func TestParseErrors(t *testing.T) {
	cases := map[string]string{
		"missing name": "version: 1\nartifacts:\n  - id: a\n    generates: a.md\n    description: d\n    template: a.md\n",
		"duplicate id": "name: x\nversion: 1\nartifacts:\n  - id: a\n    generates: a.md\n    description: d\n    template: a.md\n  - id: a\n    generates: b.md\n    description: d\n    template: b.md\n",
		"bad requires": "name: x\nversion: 1\nartifacts:\n  - id: a\n    generates: a.md\n    description: d\n    template: a.md\n    requires: [ghost]\n",
		"cycle":        "name: x\nversion: 1\nartifacts:\n  - id: a\n    generates: a.md\n    description: d\n    template: a.md\n    requires: [b]\n  - id: b\n    generates: b.md\n    description: d\n    template: b.md\n    requires: [a]\n",
	}
	for name, yaml := range cases {
		if _, err := Parse([]byte(yaml)); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestResolveArtifactOutputs(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "proposal.md", "x")
	writeTestFile(t, dir, "specs/auth/spec.md", "x")
	writeTestFile(t, dir, "specs/nested/area/spec.md", "x")

	if got := ResolveArtifactOutputs(dir, "proposal.md"); len(got) != 1 {
		t.Fatalf("plain = %v", got)
	}
	if got := ResolveArtifactOutputs(dir, "specs/**/*.md"); len(got) != 2 {
		t.Fatalf("glob = %v", got)
	}
	if got := ResolveArtifactOutputs(dir, "design.md"); got != nil {
		t.Fatalf("missing = %v", got)
	}
}

func TestResolveSpecDrivenDeep(t *testing.T) {
	s, err := Resolve("spec-driven-deep", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Artifacts) != 5 {
		t.Fatalf("artifacts = %d, want 5", len(s.Artifacts))
	}
	research, ok := s.Artifact("research")
	if !ok || research.Generates != "research.md" {
		t.Fatalf("research = %+v ok=%v", research, ok)
	}
	proposal, _ := s.Artifact("proposal")
	if !reflect.DeepEqual(proposal.Requires, []string{"research"}) {
		t.Fatalf("proposal.requires = %v", proposal.Requires)
	}
	if order := s.BuildOrder(); order[0] != "research" || order[1] != "proposal" {
		t.Fatalf("order = %v", order)
	}
	if tmpl, err := s.TemplateContent(research); err != nil || len(tmpl) == 0 {
		t.Fatalf("template err=%v", err)
	}
	if got := List(""); !reflect.DeepEqual(got, []string{"spec-driven", "spec-driven-deep", "team-driven", "team-driven-ux"}) {
		t.Fatalf("list = %v", got)
	}
}

func TestResolveTeamDriven(t *testing.T) {
	s, err := Resolve("team-driven", "")
	if err != nil {
		t.Fatal(err)
	}
	wantRequires := map[string][]string{
		"research":    {},
		"proposal":    {"research"},
		"specs":       {"proposal"},
		"design":      {"proposal"},
		"test-matrix": {"specs"},
		"tasks":       {"design", "test-matrix"},
	}
	if len(s.Artifacts) != len(wantRequires) {
		t.Fatalf("artifacts = %d, want %d", len(s.Artifacts), len(wantRequires))
	}
	for id, want := range wantRequires {
		a, ok := s.Artifact(id)
		if !ok {
			t.Fatalf("missing artifact %s", id)
		}
		got := a.Requires
		if got == nil {
			got = []string{}
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s.requires = %v, want %v", id, got, want)
		}
		if tmpl, err := s.TemplateContent(a); err != nil || len(tmpl) == 0 {
			t.Fatalf("%s template err=%v", id, err)
		}
	}
	if s.Apply == nil || !s.Apply.Traceability {
		t.Fatal("team-driven must opt into apply.traceability")
	}

	wantPersonas := map[string][]string{
		"research":    {"product-owner"},
		"proposal":    {"product-owner", "senior-staff"},
		"specs":       {"senior-staff", "senior-engineer", "qa"},
		"design":      {"senior-engineer", "senior-staff"},
		"test-matrix": {"qa"},
		"tasks":       {"backend-dev", "frontend-dev", "senior-engineer"},
	}
	for id, personas := range wantPersonas {
		a, _ := s.Artifact(id)
		for _, p := range personas {
			if !strings.Contains(a.Instruction, p) {
				t.Errorf("%s instruction does not name persona %s", id, p)
			}
		}
	}
	tasks, _ := s.Artifact("tasks")
	for _, needle := range []string{"(req: ", "typosquat", "CVEs"} {
		if !strings.Contains(tasks.Instruction, needle) {
			t.Errorf("tasks instruction missing %q", needle)
		}
	}
	tm, _ := s.Artifact("test-matrix")
	for _, needle := range []string{"teamTestMatrix", "priority", "untestable"} {
		if !strings.Contains(tm.Instruction, needle) {
			t.Errorf("test-matrix instruction missing %q", needle)
		}
	}
	research, _ := s.Artifact("research")
	for _, needle := range []string{"analysis or readiness question", "grounding from scratch"} {
		if !strings.Contains(research.Instruction, needle) {
			t.Errorf("research instruction missing %q", needle)
		}
	}
	for _, a := range s.Artifacts {
		if strings.Contains(a.Instruction, "ui-ux") {
			t.Errorf("team-driven %s instruction names ui-ux", a.ID)
		}
	}
}

func TestResolveTeamDrivenUx(t *testing.T) {
	s, err := Resolve("team-driven-ux", "")
	if err != nil {
		t.Fatal(err)
	}
	wantRequires := map[string][]string{
		"research":    {},
		"proposal":    {"research"},
		"specs":       {"proposal"},
		"design":      {"proposal"},
		"test-matrix": {"specs"},
		"ux-review":   {"specs", "design"},
		"tasks":       {"design", "test-matrix", "ux-review"},
	}
	if len(s.Artifacts) != len(wantRequires) {
		t.Fatalf("artifacts = %d, want %d", len(s.Artifacts), len(wantRequires))
	}
	for id, want := range wantRequires {
		a, ok := s.Artifact(id)
		if !ok {
			t.Fatalf("missing artifact %s", id)
		}
		got := a.Requires
		if got == nil {
			got = []string{}
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s.requires = %v, want %v", id, got, want)
		}
		if tmpl, err := s.TemplateContent(a); err != nil || len(tmpl) == 0 {
			t.Fatalf("%s template err=%v", id, err)
		}
	}
	if s.Apply == nil || !s.Apply.Traceability {
		t.Fatal("team-driven-ux must opt into apply.traceability")
	}

	ux, _ := s.Artifact("ux-review")
	for _, needle := range []string{
		"ui-ux", "Design system", "Engagement mechanics", "Accessibility",
		"Friction audit", "Findings against specs", "reflective-endorsement",
		"WCAG 2.2 AA", "colorsenv", "foreground/background",
	} {
		if !strings.Contains(ux.Instruction, needle) {
			t.Errorf("ux-review instruction missing %q", needle)
		}
	}
	specs, _ := s.Artifact("specs")
	if !strings.Contains(specs.Instruction, "ui-ux") || !strings.Contains(specs.Instruction, "friction") {
		t.Error("specs instruction lacks the ui-ux reviewer seat")
	}
	design, _ := s.Artifact("design")
	if !strings.Contains(design.Instruction, "ui-ux") || !strings.Contains(design.Instruction, "design-system fit") {
		t.Error("design instruction lacks the ui-ux reviewer seat")
	}
	tasks, _ := s.Artifact("tasks")
	for _, needle := range []string{"(req: ", "typosquat", "CVEs", "ux-review"} {
		if !strings.Contains(tasks.Instruction, needle) {
			t.Errorf("tasks instruction missing %q", needle)
		}
	}
	for _, id := range []string{"proposal", "specs", "design"} {
		a, _ := s.Artifact(id)
		if !strings.Contains(a.Instruction, "two review rounds") {
			t.Errorf("%s instruction lacks the bounded review protocol", id)
		}
	}
}
