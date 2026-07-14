package carry

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// DefaultPatterns is the carry set used when no copse.carry configuration
// exists: classic dotenv files at any depth.
func DefaultPatterns() []string {
	return []string{".env", ".env.*"}
}

// Discover walks a worktree and returns the sorted, worktree-relative
// paths of carry candidates: regular files matching any pattern that git
// does not track. Tracked files travel with the branch already, so
// carrying them would only invite conflicts.
func Discover(root string, patterns []string, tracked map[string]bool) ([]string, error) {
	var found []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, relErr := filepath.Rel(root, p)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel == "." {
				return nil
			}
			if d.Name() == ".git" {
				return fs.SkipDir
			}
			// A nested .git entry means a submodule or another worktree:
			// its files belong to a different checkout, never carry them.
			if _, statErr := os.Lstat(filepath.Join(p, ".git")); statErr == nil {
				return fs.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil // symlinks, sockets, devices: never carried
		}
		if tracked[rel] {
			return nil
		}
		for _, pat := range patterns {
			if Match(pat, rel) {
				found = append(found, rel)
				break
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(found)
	return found, nil
}

// Action classifies what Sync did with one file.
type Action string

const (
	ActionCopy   Action = "copy"   // destination did not exist
	ActionUpdate Action = "update" // destination existed with different content
	ActionSame   Action = "same"   // destination already identical
)

// Result records the outcome for one carried file.
type Result struct {
	Rel    string
	Action Action
}

// Sync copies rels from srcRoot into dstRoot, creating parent directories
// and preserving permission bits. Identical files are left untouched so
// repeated syncs are cheap and timestamps stay meaningful.
func Sync(srcRoot, dstRoot string, rels []string) ([]Result, error) {
	results := make([]Result, 0, len(rels))
	for _, rel := range rels {
		src := filepath.Join(srcRoot, filepath.FromSlash(rel))
		dst := filepath.Join(dstRoot, filepath.FromSlash(rel))
		data, err := os.ReadFile(src)
		if err != nil {
			return results, err
		}
		info, err := os.Stat(src)
		if err != nil {
			return results, err
		}
		existing, readErr := os.ReadFile(dst)
		switch {
		case readErr == nil && bytes.Equal(existing, data):
			results = append(results, Result{rel, ActionSame})
			continue
		case readErr == nil:
			results = append(results, Result{rel, ActionUpdate})
		case errors.Is(readErr, fs.ErrNotExist):
			results = append(results, Result{rel, ActionCopy})
		default:
			return results, readErr
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return results, err
		}
		if err := os.WriteFile(dst, data, info.Mode().Perm()); err != nil {
			return results, err
		}
		// WriteFile only applies the mode on create; align updates too.
		if err := os.Chmod(dst, info.Mode().Perm()); err != nil {
			return results, err
		}
	}
	return results, nil
}

// Drift classifies one file in check mode.
type Drift string

const (
	DriftOK      Drift = "ok"      // task copy matches the source
	DriftStale   Drift = "stale"   // task copy differs from the source
	DriftMissing Drift = "missing" // file absent from the task worktree
)

// CheckResult records the drift status for one carried file.
type CheckResult struct {
	Rel   string
	Drift Drift
}

// Check compares carried files without writing anything, returning one
// result per file plus the number of files that drifted.
func Check(srcRoot, dstRoot string, rels []string) ([]CheckResult, int, error) {
	results := make([]CheckResult, 0, len(rels))
	drift := 0
	for _, rel := range rels {
		src := filepath.Join(srcRoot, filepath.FromSlash(rel))
		dst := filepath.Join(dstRoot, filepath.FromSlash(rel))
		want, err := os.ReadFile(src)
		if err != nil {
			return results, drift, err
		}
		got, err := os.ReadFile(dst)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			results = append(results, CheckResult{rel, DriftMissing})
			drift++
		case err != nil:
			return results, drift, err
		case bytes.Equal(want, got):
			results = append(results, CheckResult{rel, DriftOK})
		default:
			results = append(results, CheckResult{rel, DriftStale})
			drift++
		}
	}
	return results, drift, nil
}
