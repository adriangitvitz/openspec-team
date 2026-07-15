package team

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/adriangitvitz/openspec-team/internal/fsutil"
)

// WriteArtifactOutput persists a persona run's output to the artifact path,
// refusing glob targets and empty output (which would mark the artifact complete).
func WriteArtifactOutput(changeDir, artifactID, generates, output string) (string, error) {
	if fsutil.IsGlobPattern(generates) {
		return "", fmt.Errorf("--write is not supported for multi-file artifact %q (%s); consume stdout and split the output", artifactID, generates)
	}
	if strings.TrimSpace(output) == "" {
		return "", fmt.Errorf("model returned empty output; not writing %s", generates)
	}
	path := filepath.Join(changeDir, generates)
	if err := fsutil.WriteFileAtomic(path, []byte(output), 0o644); err != nil {
		return "", err
	}
	return path, nil
}
