package core

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const maxDiscoveryDepth = 5
const maxDiscoveredPaths = 25

var skipDirs = map[string]bool{
	".git": true, "node_modules": true, ".venv": true, "venv": true,
	"vendor": true, "dist": true, "build": true, "__pycache__": true,
	".next": true, "target": true, ".pytest_cache": true, ".idea": true,
	".vscode": true, "openspec": true, ".claude": true,
}

var hubFilenames = map[string]bool{
	"SUMMARY.md":      true,
	"ARCHITECTURE.md": true,
	"CLAUDE.md":       true,
	"AGENTS.md":       true,
}

var adrDirNames = map[string]bool{
	"adr": true, "adrs": true, "decisions": true,
}

func isDocsDirName(name string) bool {
	lower := strings.ToLower(name)
	return lower == "docs" || lower == "doc" || strings.HasPrefix(lower, "docs-")
}

// DiscoverDocHubs returns knowledge entries for ADR directories and doc hubs found under root.
func DiscoverDocHubs(root string) []KnowledgeEntry {
	var adrPaths, hubPaths []string

	filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil || rel == "." {
			return nil
		}
		depth := strings.Count(filepath.ToSlash(rel), "/")

		if d.IsDir() {
			if skipDirs[d.Name()] || depth >= maxDiscoveryDepth {
				return fs.SkipDir
			}
			if adrDirNames[strings.ToLower(d.Name())] && dirHasMarkdown(path) {
				adrPaths = append(adrPaths, filepath.ToSlash(rel))
				return fs.SkipDir
			}
			if isDocsDirName(d.Name()) && dirHasMarkdown(path) {

				hubPaths = append(hubPaths, filepath.ToSlash(rel))
			}
			return nil
		}

		if hubFilenames[d.Name()] {
			hubPaths = append(hubPaths, filepath.ToSlash(rel))
		}
		return nil
	})

	sort.Strings(adrPaths)
	sort.Strings(hubPaths)

	var entries []KnowledgeEntry
	if len(adrPaths) > 0 {
		entries = append(entries, KnowledgeEntry{
			Topic: "Architecture decision records",
			Note:  "Read the ADRs touching your change's area before proposing — they record decisions the code does not explain.",
			Paths: capPaths(adrPaths),
		})
	}
	if len(hubPaths) > 0 {
		entries = append(entries, KnowledgeEntry{
			Topic: "Documentation hubs (auto-discovered — curate into topical entries)",
			Note:  "Indexes and briefs found by openspec init. Split these into per-topic entries with the files that matter for each domain.",
			Paths: capPaths(hubPaths),
		})
	}
	return entries
}

func dirHasMarkdown(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			return true
		}
		if e.IsDir() && !skipDirs[e.Name()] {

			if sub, err := os.ReadDir(filepath.Join(dir, e.Name())); err == nil {
				for _, s := range sub {
					if !s.IsDir() && strings.HasSuffix(s.Name(), ".md") {
						return true
					}
				}
			}
		}
	}
	return false
}

func capPaths(paths []string) []string {
	if len(paths) > maxDiscoveredPaths {
		return paths[:maxDiscoveredPaths]
	}
	return paths
}
