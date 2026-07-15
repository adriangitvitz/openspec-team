package parser

import (
	"regexp"
	"strings"
)

var (
	// `**ID**: ...` style metadata lines.
	metadataLineRe = regexp.MustCompile(`^\*\*[^*]+\*\*:`)
	headerLineRe   = regexp.MustCompile(`^#{1,6}\s`)
	// Parity: ANY #### header counts as a scenario, not only `#### Scenario:`.
	scenarioHeaderRe = regexp.MustCompile(`^####\s+`)
	shallOrMustRe    = regexp.MustCompile(`\b(SHALL|MUST)\b`)
)

// ContainsShallOrMust matches SHALL or MUST as whole words, case-sensitive.
func ContainsShallOrMust(text string) bool {
	return shallOrMustRe.MatchString(text)
}

// ExtractRequirementBody joins the body lines up to the first non-fenced
// header; metadata lines are dropped unless they are all the body has.
func ExtractRequirementBody(bodyLines []string) string {
	mask := BuildCodeFenceMask(bodyLines)
	var captured, metadata []string

	for i, line := range bodyLines {
		if mask[i] {
			continue
		}
		if headerLineRe.MatchString(line) {
			break
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if metadataLineRe.MatchString(trimmed) {
			metadata = append(metadata, trimmed)
			continue
		}
		captured = append(captured, trimmed)
	}

	if len(captured) > 0 {
		return strings.Join(captured, "\n")
	}
	return strings.Join(metadata, "\n")
}

// ExtractRequirementText returns the requirement body, falling back to the header title for display only.
func ExtractRequirementText(headerTitle string, bodyLines []string) string {
	if body := ExtractRequirementBody(bodyLines); body != "" {
		return body
	}
	return strings.TrimSpace(headerTitle)
}

// CountScenarios counts `#### ` headers on non-fenced lines.
func CountScenarios(bodyLines []string) int {
	mask := BuildCodeFenceMask(bodyLines)
	count := 0
	for i, line := range bodyLines {
		if mask[i] {
			continue
		}
		if scenarioHeaderRe.MatchString(line) {
			count++
		}
	}
	return count
}
