package mirror

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"github.com/MaxSiominDev/holt/internal/git"
	"github.com/MaxSiominDev/holt/internal/worktree"
)

type Result struct {
	Linked    []string // now pointing at the main checkout
	Blocked   []string // a real file or directory occupies the path
	Unmatched []string // the pattern matched nothing in the main checkout
}

type WorktreeResult struct {
	Worktree worktree.Worktree
	Result   Result
	Gone     bool  // git still lists it, but its directory is not there
	Err      error // this worktree could not be mirrored into
}

var ErrWorktreeGone = errors.New("the worktree directory is not there")

// Sync covers every worktree but the main checkout.
func Sync(repo *git.Repo, patterns []string) ([]WorktreeResult, error) {
	worktrees, err := worktree.List(repo)
	if err != nil {
		return nil, err
	}

	mainCheckout := worktrees[0].Path
	results := make([]WorktreeResult, 0, len(worktrees)-1)
	for _, w := range worktrees[1:] {
		result, err := link(mainCheckout, w.Path, patterns)
		// One worktree holt cannot read must not cost the repair of the rest,
		// which is what sync exists for. The caller reports it instead.
		switch {
		case errors.Is(err, ErrWorktreeGone):
			results = append(results, WorktreeResult{Worktree: w, Gone: true})
		case err != nil:
			results = append(results, WorktreeResult{Worktree: w, Err: err})
		default:
			results = append(results, WorktreeResult{Worktree: w, Result: result})
		}
	}
	return results, nil
}

// SyncOne is what the post-checkout hook calls.
func SyncOne(repo *git.Repo, patterns []string, worktreePath string) (Result, error) {
	worktrees, err := worktree.List(repo)
	if err != nil {
		return Result{}, err
	}
	known := slices.ContainsFunc(worktrees, func(w worktree.Worktree) bool {
		return w.Path == worktreePath
	})
	if !known {
		return Result{}, fmt.Errorf("%s is not a worktree of this repository", worktreePath)
	}
	return link(worktrees[0].Path, worktreePath, patterns)
}

// link never replaces anything but a symlink.
func link(mainCheckout, worktreePath string, patterns []string) (Result, error) {
	var result Result
	if worktreePath == mainCheckout {
		return result, nil
	}
	// Linking creates the parents it needs, so a deleted worktree would come
	// back holding nothing but symlinks. An unreadable one is not "gone": that
	// would send the user to prune a worktree still in place.
	if _, err := os.Stat(worktreePath); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return result, fmt.Errorf("%w: %s", ErrWorktreeGone, worktreePath)
		}
		return result, fmt.Errorf("reading %s: %w", worktreePath, err)
	}

	for _, pattern := range patterns {
		relatives, err := expand(mainCheckout, pattern)
		if err != nil {
			return result, err
		}
		if len(relatives) == 0 {
			result.Unmatched = append(result.Unmatched, pattern)
			continue
		}
		for _, rel := range relatives {
			target := filepath.Join(worktreePath, rel)
			if escapesWorktree(worktreePath, target) {
				result.Blocked = append(result.Blocked, rel)
				continue
			}
			linked, err := linkOne(filepath.Join(mainCheckout, rel), target)
			if err != nil {
				return result, err
			}
			if linked {
				result.Linked = append(result.Linked, rel)
			} else {
				result.Blocked = append(result.Blocked, rel)
			}
		}
	}
	return result, nil
}

// Unsync reports how many links went and how many worktrees held one.
func Unsync(repo *git.Repo, patterns []string) (links, worktrees int, err error) {
	all, err := worktree.List(repo)
	if err != nil {
		return 0, 0, err
	}

	mainCheckout := all[0].Path
	for _, w := range all[1:] {
		unlinked, err := unlink(mainCheckout, w.Path, patterns)
		if err != nil {
			return 0, 0, fmt.Errorf("unmirroring from %s: %w", w.Path, err)
		}
		links += len(unlinked)
		if len(unlinked) > 0 {
			worktrees++
		}
	}
	return links, worktrees, nil
}

// Only links still pointing at the main checkout go, so hand-made files survive.
func unlink(mainCheckout, worktreePath string, patterns []string) ([]string, error) {
	var removed []string
	for _, pattern := range patterns {
		// Expanded against the worktree, which also finds links whose source
		// file is already gone.
		matches, err := filepath.Glob(filepath.Join(worktreePath, pattern))
		if err != nil {
			return nil, fmt.Errorf("pattern %s: %w", pattern, err)
		}
		for _, match := range matches {
			rel, err := filepath.Rel(worktreePath, match)
			if err != nil {
				return nil, err
			}
			if escapesWorktree(worktreePath, match) {
				continue
			}
			if actual, err := os.Readlink(match); err != nil || actual != filepath.Join(mainCheckout, rel) {
				continue
			}
			if err := os.Remove(match); err != nil {
				return nil, err
			}
			removed = append(removed, rel)
		}
	}
	return removed, nil
}

type Check struct {
	Linked   []string // symlink present and pointing at the main checkout
	Absent   []string // nothing is there; never made, or deleted
	Blocked  []string // a real file or directory occupies the path
	Diverted []string // a symlink pointing somewhere other than the main checkout
}

func Inspect(mainCheckout, worktreePath string, patterns []string) (Check, error) {
	var check Check
	// Every path would otherwise count as absent.
	if _, err := os.Stat(worktreePath); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return check, fmt.Errorf("%w: %s", ErrWorktreeGone, worktreePath)
		}
		return check, fmt.Errorf("reading %s: %w", worktreePath, err)
	}

	for _, pattern := range patterns {
		relatives, err := expand(mainCheckout, pattern)
		if err != nil {
			return check, err
		}
		for _, rel := range relatives {
			target := filepath.Join(worktreePath, rel)
			if escapesWorktree(worktreePath, target) {
				check.Blocked = append(check.Blocked, rel)
				continue
			}
			info, err := os.Lstat(target)
			switch {
			case errors.Is(err, os.ErrNotExist):
				check.Absent = append(check.Absent, rel)
			case err != nil:
				return check, err
			case info.Mode()&os.ModeSymlink == 0:
				check.Blocked = append(check.Blocked, rel)
			default:
				actual, err := os.Readlink(target)
				if err != nil {
					return check, err
				}
				if actual == filepath.Join(mainCheckout, rel) {
					check.Linked = append(check.Linked, rel)
				} else {
					check.Diverted = append(check.Diverted, rel)
				}
			}
		}
	}
	return check, nil
}

// MissingPatterns returns the patterns matching nothing in the main checkout.
func MissingPatterns(mainCheckout string, patterns []string) ([]string, error) {
	var missing []string
	for _, pattern := range patterns {
		relatives, err := expand(mainCheckout, pattern)
		if err != nil {
			return nil, err
		}
		if len(relatives) == 0 {
			missing = append(missing, pattern)
		}
	}
	return missing, nil
}

func expand(mainCheckout, pattern string) ([]string, error) {
	matches, err := filepath.Glob(filepath.Join(mainCheckout, pattern))
	if err != nil {
		return nil, fmt.Errorf("pattern %s: %w", pattern, err)
	}

	relatives := make([]string, 0, len(matches))
	for _, match := range matches {
		rel, err := filepath.Rel(mainCheckout, match)
		if err != nil {
			return nil, err
		}
		relatives = append(relatives, rel)
	}
	return relatives, nil
}

// A mirrored directory is itself a symlink into the main checkout, so anything
// under one resolves there and holt would rewrite the original. A parent that
// is not there yet is fine: linkOne creates it inside the worktree.
func escapesWorktree(worktreePath, target string) bool {
	parent, err := filepath.EvalSymlinks(filepath.Dir(target))
	return err == nil && !under(parent, worktreePath)
}

// linkOne returns false when a real file or directory is in the way.
func linkOne(source, target string) (bool, error) {
	info, err := os.Lstat(target)
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return false, err
	case info.Mode()&os.ModeSymlink == 0:
		return false, nil
	default:
		// The hook runs on every checkout, so a correct symlink is left alone.
		if actual, err := os.Readlink(target); err == nil && actual == source {
			return true, nil
		}
		// Anything else is stale, most likely from before the main checkout moved.
		if err := os.Remove(target); err != nil {
			return false, err
		}
	}

	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return false, err
	}
	if err := os.Symlink(source, target); err != nil {
		return false, err
	}
	return true, nil
}
