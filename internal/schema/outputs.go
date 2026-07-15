package schema

import (
	"os"
	"path/filepath"

	"github.com/adriangitvitz/openspec-team/internal/fsutil"
)

// ResolveArtifactOutputs resolves a generates path or glob to existing files under changeDir.
func ResolveArtifactOutputs(changeDir, generates string) []string {
	if !fsutil.IsGlobPattern(generates) {
		full := filepath.Join(changeDir, generates)
		if info, err := os.Stat(full); err == nil && info.Mode().IsRegular() {
			return []string{full}
		}
		return nil
	}
	return fsutil.GlobFiles(changeDir, generates)
}

// ArtifactOutputExists reports whether an artifact has at least one output.
func ArtifactOutputExists(changeDir, generates string) bool {
	return len(ResolveArtifactOutputs(changeDir, generates)) > 0
}
