package parser

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Spec is a parsed main spec document.
type Spec struct {
	Name         string
	Purpose      string
	Requirements []Requirement
}

// Requirement is one `### Requirement:` block. Raw includes the header line
// and is the unit that is hashed and merged.
type Requirement struct {
	Name      string
	Text      string
	Raw       string
	Scenarios []Scenario
}

// Scenario is one `####` child of a requirement.
type Scenario struct {
	Name string
	Raw  string
}

var (
	ErrNoPurpose      = errors.New("spec must have a Purpose section")
	ErrNoRequirements = errors.New("spec must have a Requirements section")
)

var requirementTitleRe = regexp.MustCompile(`(?i)^Requirement:\s*`)

// ParseSpec parses a main spec document; Purpose and Requirements are required.
func ParseSpec(name, content string) (*Spec, error) {
	sections := ParseSections(content)

	purposeSection := FindSection(sections, "Purpose")
	if purposeSection == nil || strings.TrimSpace(purposeSection.Content) == "" {
		return nil, ErrNoPurpose
	}
	requirementsSection := FindSection(sections, "Requirements")
	if requirementsSection == nil {
		return nil, ErrNoRequirements
	}

	spec := &Spec{
		Name:    name,
		Purpose: strings.TrimSpace(purposeSection.Content),
	}
	for _, child := range requirementsSection.Children {
		bodyLines := strings.Split(child.Content, "\n")
		req := Requirement{
			Name: strings.TrimSpace(requirementTitleRe.ReplaceAllString(child.Title, "")),
			Text: ExtractRequirementText(child.Title, bodyLines),
			Raw:  rebuildBlock(child),
		}
		for _, sc := range child.Children {
			if strings.TrimSpace(sc.Content) == "" {
				continue
			}
			req.Scenarios = append(req.Scenarios, Scenario{
				Name: strings.TrimSpace(regexp.MustCompile(`(?i)^Scenario:\s*`).ReplaceAllString(sc.Title, "")),
				Raw:  sc.Content,
			})
		}
		spec.Requirements = append(spec.Requirements, req)
	}
	return spec, nil
}

func rebuildBlock(s *Section) string {
	header := strings.Repeat("#", s.Level) + " " + s.Title
	if strings.TrimSpace(s.Content) == "" {
		return header
	}
	return header + "\n" + s.Content
}

var (
	requirementsSectionHeaderRe = regexp.MustCompile(`(?i)^##\s+Requirements\s*$`)
	topLevelSectionHeaderRe     = regexp.MustCompile(`^##\s+`)
	deltaHeaderRe               = regexp.MustCompile(`(?i)^##\s+(ADDED|MODIFIED|REMOVED|RENAMED)\s+Requirements\s*$`)
	strictRequirementHeaderRe   = regexp.MustCompile(`(?i)^###\s+Requirement:\s*(.+)\s*$`)
)

// StructureIssueKind classifies a main-spec structure problem.
type StructureIssueKind string

const (
	IssueDeltaHeader                    StructureIssueKind = "delta-header"
	IssueRequirementOutsideRequirements StructureIssueKind = "requirement-outside-requirements"
)

// StructureIssue is a structural problem in a main spec, with a 1-based line.
type StructureIssue struct {
	Kind    StructureIssueKind
	Line    int
	Header  string
	Message string
}

// FindMainSpecStructureIssues reports delta headers and out-of-section requirement headers in a main spec.
func FindMainSpecStructureIssues(content string) []StructureIssue {
	stripped := StripFencedCodeBlocksPreservingLines(NormalizeLineEndings(content))
	lines := strings.Split(stripped, "\n")
	var issues []StructureIssue

	requirementsHeaderIndex := -1
	for i, line := range lines {
		if requirementsSectionHeaderRe.MatchString(line) {
			requirementsHeaderIndex = i
			break
		}
	}
	requirementsEndIndex := len(lines)
	if requirementsHeaderIndex != -1 {
		for i := requirementsHeaderIndex + 1; i < len(lines); i++ {
			if topLevelSectionHeaderRe.MatchString(lines[i]) {
				requirementsEndIndex = i
				break
			}
		}
	}

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if deltaHeaderRe.MatchString(line) {
			issues = append(issues, StructureIssue{
				Kind:   IssueDeltaHeader,
				Line:   i + 1,
				Header: trimmed,
				Message: fmt.Sprintf(
					"Main spec contains delta header %q. Delta headers are only valid inside openspec/changes/<name>/specs/<capability>/spec.md and truncate the parsed ## Requirements section.",
					trimmed),
			})
			continue
		}
		if !strictRequirementHeaderRe.MatchString(line) {
			continue
		}
		inside := requirementsHeaderIndex != -1 && i > requirementsHeaderIndex && i < requirementsEndIndex
		if !inside {
			issues = append(issues, StructureIssue{
				Kind:   IssueRequirementOutsideRequirements,
				Line:   i + 1,
				Header: trimmed,
				Message: fmt.Sprintf(
					"Requirement header %q appears outside the main ## Requirements section. Main specs only parse requirements inside that section, so this requirement is currently invisible to validate, list, and archive.",
					trimmed),
			})
		}
	}
	return issues
}

// RequirementBlock is a raw `### Requirement:` block as the merge path sees it.
type RequirementBlock struct {
	HeaderLine string
	Name       string
	Raw        string
}

// RequirementsSection splits a spec document around its ## Requirements section.
type RequirementsSection struct {
	Before     string
	HeaderLine string
	Preamble   string
	Blocks     []RequirementBlock
	After      string
}

// \s* (not \s+): `###Requirement:` also matches, per the delta grammar.
var requirementHeaderRe = regexp.MustCompile(`(?i)^###\s*Requirement:\s*(.+)\s*$`)

// NormalizeRequirementName trims a requirement name.
func NormalizeRequirementName(name string) string {
	return strings.TrimSpace(name)
}

// ExtractRequirementsSection parses the Requirements section's blocks in
// order, synthesizing an empty section when missing.
func ExtractRequirementsSection(content string) RequirementsSection {
	lines := strings.Split(NormalizeLineEndings(content), "\n")

	reqHeaderIndex := -1
	for i, line := range lines {
		if requirementsSectionHeaderRe.MatchString(line) {
			reqHeaderIndex = i
			break
		}
	}
	if reqHeaderIndex == -1 {
		before := strings.TrimRight(content, " \t\n\r")
		if before != "" {
			before += "\n\n"
		}
		return RequirementsSection{
			Before:     before,
			HeaderLine: "## Requirements",
			After:      "\n",
		}
	}

	endIndex := len(lines)
	for i := reqHeaderIndex + 1; i < len(lines); i++ {
		if topLevelSectionHeaderRe.MatchString(lines[i]) {
			endIndex = i
			break
		}
	}

	before := strings.Join(lines[:reqHeaderIndex], "\n")
	headerLine := lines[reqHeaderIndex]
	sectionBody := lines[reqHeaderIndex+1 : endIndex]

	var blocks []RequirementBlock
	var preambleLines []string
	cursor := 0
	for cursor < len(sectionBody) && !requirementHeaderRe.MatchString(sectionBody[cursor]) {
		preambleLines = append(preambleLines, sectionBody[cursor])
		cursor++
	}
	for cursor < len(sectionBody) {
		headerCandidate := sectionBody[cursor]
		m := requirementHeaderRe.FindStringSubmatch(headerCandidate)
		if m == nil {
			cursor++
			continue
		}
		name := NormalizeRequirementName(m[1])
		cursor++
		bodyLines := []string{headerCandidate}
		for cursor < len(sectionBody) &&
			!requirementHeaderRe.MatchString(sectionBody[cursor]) &&
			!topLevelSectionHeaderRe.MatchString(sectionBody[cursor]) {
			bodyLines = append(bodyLines, sectionBody[cursor])
			cursor++
		}
		blocks = append(blocks, RequirementBlock{
			HeaderLine: headerCandidate,
			Name:       name,
			Raw:        strings.TrimRight(strings.Join(bodyLines, "\n"), " \t\n\r"),
		})
	}

	after := strings.Join(lines[endIndex:], "\n")
	if !strings.HasPrefix(after, "\n") {
		after = "\n" + after
	}
	if strings.TrimRight(before, " \t\n\r") != "" {
		before += "\n"
	}

	return RequirementsSection{
		Before:     before,
		HeaderLine: headerLine,
		Preamble:   strings.TrimRight(strings.Join(preambleLines, "\n"), " \t\n\r"),
		Blocks:     blocks,
		After:      after,
	}
}
