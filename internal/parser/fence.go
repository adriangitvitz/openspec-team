// Package parser implements the fence-aware markdown grammar for specs,
// change deltas, and task lists. Pure functions, exact port of the TS grammar.
package parser

import (
	"regexp"
	"strings"
)

var (
	fenceOpenRe  = regexp.MustCompile("^\\s*(`{3,}|~{3,})")
	fenceCloseRe = regexp.MustCompile("^\\s*(`{3,}|~{3,})\\s*$")
)

type fence struct {
	marker byte
	length int
}

func openingFence(line string) (fence, bool) {
	m := fenceOpenRe.FindStringSubmatch(line)
	if m == nil {
		return fence{}, false
	}
	return fence{marker: m[1][0], length: len(m[1])}, true
}

// Close: same marker char, length >= opener, whitespace-only tail.
func isClosingFence(line string, active fence) bool {
	m := fenceCloseRe.FindStringSubmatch(line)
	return m != nil && m[1][0] == active.marker && len(m[1]) >= active.length
}

// BuildCodeFenceMask marks lines inside fenced code blocks, fences included.
func BuildCodeFenceMask(lines []string) []bool {
	mask := make([]bool, len(lines))
	var active *fence

	for i, line := range lines {
		if active == nil {
			if f, ok := openingFence(line); ok {
				active = &f
				mask[i] = true
			}
			continue
		}
		mask[i] = true
		if isClosingFence(line, *active) {
			active = nil
		}
	}
	return mask
}

// StripFencedCodeBlocksPreservingLines blanks fenced lines so line numbers stay stable.
func StripFencedCodeBlocksPreservingLines(content string) string {
	lines := strings.Split(content, "\n")
	out := make([]string, 0, len(lines))
	var active *fence

	for _, line := range lines {
		if active == nil {
			if f, ok := openingFence(line); ok {
				active = &f
				out = append(out, "")
			} else {
				out = append(out, line)
			}
			continue
		}
		out = append(out, "")
		if isClosingFence(line, *active) {
			active = nil
		}
	}
	return strings.Join(out, "\n")
}

var lineEndings = strings.NewReplacer("\r\n", "\n", "\r", "\n")

// NormalizeLineEndings converts CRLF/CR to LF.
func NormalizeLineEndings(content string) string {
	return lineEndings.Replace(content)
}
