package validate

import (
	"strings"
	"testing"

	"github.com/adriangitvitz/openspec-go/internal/core"
)

var itkEntries = []core.KnowledgeEntry{
	{
		Topic: "ITK codes / material resolution",
		Paths: []string{
			"docs/adr/adr-0001-resolucion-material.md",
			"src/material_xref.py",
		},
	},
	{
		Topic: "Payment gateway",
		Paths: []string{"docs/payments.md"},
	},
}

func TestKnowledgeCoverageWarnsOnUncitedTopic(t *testing.T) {
	dir := writeChange(t, map[string]string{
		"proposal.md": "## Why\nWe change how the material lookup handles drift.",
		"research.md": "# Research\nRead the middleware README only.",
	})
	issues := KnowledgeCoverage(dir, itkEntries)
	if len(issues) != 1 {
		t.Fatalf("issues = %+v", issues)
	}
	if issues[0].Level != Warning || !strings.Contains(issues[0].Message, "ITK codes / material resolution") {
		t.Fatalf("issue = %+v", issues[0])
	}

	if strings.Contains(issues[0].Message, "Payment gateway") {
		t.Fatalf("unrelated topic warned: %+v", issues[0])
	}
}

func TestKnowledgeCoverageSatisfiedByCitation(t *testing.T) {
	dir := writeChange(t, map[string]string{
		"proposal.md": "## Why\nWe change the material lookup.",
		"research.md": "# Research\n- docs/adr/adr-0001-resolucion-material.md — lookup, not formula.",
	})
	if issues := KnowledgeCoverage(dir, itkEntries); len(issues) != 0 {
		t.Fatalf("issues = %+v", issues)
	}
}

func TestKnowledgeCoverageBasenameCitationCounts(t *testing.T) {
	dir := writeChange(t, map[string]string{
		"proposal.md": "## Why\nMaterial handling changes.",
		"research.md": "# Research\nPer adr-0001-resolucion-material.md the codes are looked up.",
	})
	if issues := KnowledgeCoverage(dir, itkEntries); len(issues) != 0 {
		t.Fatalf("basename citation not accepted: %+v", issues)
	}
}

func TestKnowledgeCoverageWithoutResearchChecksAllArtifacts(t *testing.T) {
	dir := writeChange(t, map[string]string{
		"proposal.md": "## Why\nMaterial change. See src/material_xref.py for the current lookup.",
	})
	if issues := KnowledgeCoverage(dir, itkEntries); len(issues) != 0 {
		t.Fatalf("issues = %+v", issues)
	}
	dir2 := writeChange(t, map[string]string{
		"proposal.md": "## Why\nMaterial change with no citations.",
	})
	if issues := KnowledgeCoverage(dir2, itkEntries); len(issues) != 1 {
		t.Fatalf("issues = %+v", issues)
	}
}

func TestKnowledgeCoverageTermInResearchOnlyDoesNotImplicate(t *testing.T) {
	dir := writeChange(t, map[string]string{
		"proposal.md": "## Why\nUnrelated UI polish.",
		"research.md": "# Research\nConsidered material resolution; not in scope.",
	})
	if issues := KnowledgeCoverage(dir, itkEntries); len(issues) != 0 {
		t.Fatalf("issues = %+v", issues)
	}
}

func TestKnowledgeCoverageNoEntries(t *testing.T) {
	dir := writeChange(t, map[string]string{"proposal.md": "## Why\nMaterial change."})
	if issues := KnowledgeCoverage(dir, nil); issues != nil {
		t.Fatalf("issues = %+v", issues)
	}
}
