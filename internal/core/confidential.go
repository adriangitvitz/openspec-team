package core

import (
	"fmt"
	"path"
	"strings"
)

// ExcludedScanDirs are directories never walked by persona tooling or validation scans.
var ExcludedScanDirs = []string{".git", "node_modules", "vendor", "dist"}

// MatchesConfidential reports whether relPath (slash-normalized, root-relative)
// matches any confidential pattern; a sibling ".md" extraction inherits its source's match.
func MatchesConfidential(patterns []string, relPath string) bool {
	if len(patterns) == 0 || relPath == "" {
		return false
	}
	for _, p := range patterns {

		if CheckConfidentialPattern(p) != nil {
			return true
		}
		if matchGlob(p, relPath) {
			return true
		}
		if stripped, ok := strings.CutSuffix(relPath, ".md"); ok && matchGlob(p, stripped) {
			return true
		}
	}
	return false
}

// CheckConfidentialPattern returns an error if any non-`**` segment is not a valid path.Match pattern.
func CheckConfidentialPattern(pattern string) error {
	for _, seg := range strings.Split(pattern, "/") {
		if seg == "**" {
			continue
		}
		if _, err := path.Match(seg, "probe"); err != nil {
			return fmt.Errorf("pattern %q: segment %q: %w", pattern, seg, err)
		}
	}
	return nil
}

// matchGlob matches `*` per segment and `**` across segments; path.Match alone silently fails open on `**`.
func matchGlob(pattern, relPath string) bool {
	return matchSegments(strings.Split(pattern, "/"), strings.Split(relPath, "/"))
}

func matchSegments(pat, parts []string) bool {
	if len(pat) == 0 {
		return len(parts) == 0
	}
	if pat[0] == "**" {
		if matchSegments(pat[1:], parts) {
			return true
		}
		if len(parts) > 0 {
			return matchSegments(pat, parts[1:])
		}
		return false
	}
	if len(parts) == 0 {
		return false
	}
	ok, err := path.Match(pat[0], parts[0])
	if err != nil {
		return true
	}
	if !ok {
		return false
	}
	return matchSegments(pat[1:], parts[1:])
}
