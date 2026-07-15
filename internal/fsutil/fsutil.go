// Package fsutil provides atomic writes, directory moves, and a minimal glob.
package fsutil

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// WriteFileAtomic writes via a same-directory temp file + fsync + rename.
func WriteFileAtomic(path string, content []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// MoveDir renames src to dst (copy+delete fallback for cross-device moves); fails if dst exists.
func MoveDir(src, dst string) error {
	if _, err := os.Stat(dst); err == nil {
		return fmt.Errorf("destination already exists: %s", dst)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	if err := copyDir(src, dst); err != nil {
		return err
	}
	return os.RemoveAll(src)
}

func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyFile(path, target)
	})
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// IsGlobPattern reports whether a path contains glob metacharacters.
func IsGlobPattern(pattern string) bool {
	return strings.ContainsAny(pattern, "*?[")
}

// GlobFiles returns sorted paths under root matching a slash-separated pattern; `**` spans directories.
func GlobFiles(root, pattern string) []string {
	segments := strings.Split(filepath.ToSlash(pattern), "/")
	var matches []string
	filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		if matchSegments(segments, strings.Split(filepath.ToSlash(rel), "/")) {
			matches = append(matches, path)
		}
		return nil
	})
	sort.Strings(matches)
	return matches
}

func matchSegments(pattern, parts []string) bool {
	if len(pattern) == 0 {
		return len(parts) == 0
	}
	if pattern[0] == "**" {
		for skip := 0; skip <= len(parts); skip++ {
			if matchSegments(pattern[1:], parts[skip:]) {
				return true
			}
		}
		return false
	}
	if len(parts) == 0 {
		return false
	}
	ok, err := filepath.Match(pattern[0], parts[0])
	if err != nil || !ok {
		return false
	}
	return matchSegments(pattern[1:], parts[1:])
}
