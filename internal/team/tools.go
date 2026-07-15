package team

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/adriangitvitz/openspec-team/internal/core"
)

const (
	toolReadCap   = 64 * 1024
	toolGrepLimit = 100
	toolGrepMaxSz = 1 << 20
	toolListLimit = 500
)

// skipDirs excludes VCS internals and dependency trees from every tool; derived from core.ExcludedScanDirs so the validation walk matches.
var skipDirs = func() map[string]bool {
	m := make(map[string]bool, len(core.ExcludedScanDirs))
	for _, d := range core.ExcludedScanDirs {
		m[d] = true
	}
	return m
}()

// rootRel relates a resolved path to the symlink-resolved root: matching against an unresolved (symlinked) root yields ../-paths that fail open.
func rootRel(root, resolved string) (string, bool) {
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", false
	}
	rel, err := filepath.Rel(resolvedRoot, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

// isConfidential reports whether a resolved path is withheld from external runners; an undeterminable root-relative path fails closed.
func isConfidential(confidential []string, root, resolved string) bool {
	if len(confidential) == 0 {
		return false
	}
	rel, ok := rootRel(root, resolved)
	if !ok {
		return true
	}
	return core.MatchesConfidential(confidential, rel)
}

func confidentialRefusal(rel string) string {
	return fmt.Sprintf("error: %s is confidential and withheld from external runners; do not guess its contents — if it is genuinely needed, ask the human at the gate for a curated release", rel)
}

// executeTool runs one root-scoped read-only tool; failures become error strings, never panics (confidential is nil for trusted runs).
func executeTool(root string, confidential []string, name, rawArgs string) string {
	var args struct {
		Path    string `json:"path"`
		Pattern string `json:"pattern"`
	}
	if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
		return fmt.Sprintf("error: invalid tool arguments: %v", err)
	}
	switch name {
	case "read_file":
		return toolReadFile(root, confidential, args.Path)
	case "grep":
		return toolGrep(root, confidential, args.Pattern)
	case "list_dir":
		return toolListDir(root, args.Path)
	default:
		return fmt.Sprintf("error: unknown tool %q", name)
	}
}

// resolveInRoot rejects absolute paths, root escapes (lexical and post-symlink), and skipDirs components.
func resolveInRoot(root, rel string) (string, error) {
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("absolute paths are not allowed: %s", rel)
	}
	clean := filepath.Clean(filepath.FromSlash(rel))
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes the project root: %s", rel)
	}
	for _, part := range strings.Split(clean, string(filepath.Separator)) {
		if skipDirs[part] {
			return "", fmt.Errorf("path is excluded from tool access: %s", rel)
		}
	}
	full := filepath.Join(root, clean)

	resolved, err := filepath.EvalSymlinks(full)
	if err != nil {
		return "", err
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	relToRoot, err := filepath.Rel(resolvedRoot, resolved)
	if err != nil || relToRoot == ".." || strings.HasPrefix(relToRoot, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes the project root: %s", rel)
	}
	return resolved, nil
}

func toolReadFile(root string, confidential []string, rel string) string {

	full, resolveErr := resolveInRoot(root, rel)
	if resolveErr == nil && isConfidential(confidential, root, full) {
		return confidentialRefusal(rel)
	}

	if binaryDocExts[strings.ToLower(filepath.Ext(rel))] {
		sibling := rel + ".md"
		if sfull, err := resolveInRoot(root, sibling); err == nil {
			if _, statErr := os.Stat(sfull); statErr == nil {
				return fmt.Sprintf("error: %s is a binary document and cannot be read raw; read its extraction %s instead", rel, sibling)
			}
		}
		return fmt.Sprintf("error: %s is a binary document and cannot be read raw; no extraction exists yet — call request_extraction to have the harness produce one", rel)
	}
	if resolveErr != nil {
		return "error: " + resolveErr.Error()
	}
	content, err := os.ReadFile(full)
	if err != nil {
		return "error: " + err.Error()
	}
	if len(content) > toolReadCap {
		return string(content[:toolReadCap]) + "\n[truncated]"
	}
	return string(content)
}

func toolGrep(root string, confidential []string, pattern string) string {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return "error: invalid pattern: " + err.Error()
	}
	var matches []string
	filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if len(matches) >= toolGrepLimit {
			return filepath.SkipAll
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}

		info, err := d.Info()
		if err != nil || !info.Mode().IsRegular() || info.Size() > toolGrepMaxSz {
			return nil
		}

		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)

		if core.MatchesConfidential(confidential, rel) {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer f.Close()
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		line := 0
		for scanner.Scan() {
			line++
			if re.MatchString(scanner.Text()) {
				matches = append(matches, fmt.Sprintf("%s:%d: %s", filepath.ToSlash(rel), line, scanner.Text()))
				if len(matches) >= toolGrepLimit {
					return filepath.SkipAll
				}
			}
		}
		return nil
	})
	if len(matches) == 0 {
		return "no matches"
	}
	out := strings.Join(matches, "\n")
	if len(matches) >= toolGrepLimit {
		out += fmt.Sprintf("\n[stopped at %d matches]", toolGrepLimit)
	}
	return out
}

func toolListDir(root, rel string) string {
	if rel == "" {
		rel = "."
	}
	full, err := resolveInRoot(root, rel)
	if err != nil {
		return "error: " + err.Error()
	}
	entries, err := os.ReadDir(full)
	if err != nil {
		return "error: " + err.Error()
	}
	var names []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			name += "/"
		}
		names = append(names, name)
		if len(names) >= toolListLimit {
			names = append(names, "[truncated]")
			break
		}
	}
	return strings.Join(names, "\n")
}
