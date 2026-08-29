package mirror

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"github.com/MaxSiominDev/holt/internal/git"
	"github.com/MaxSiominDev/holt/internal/worktree"
)

const sep = string(filepath.Separator)

type Result struct {
	Linked    []string // now pointing at the main checkout
	Blocked   []string // something holt did not create occupies the path
	Unmatched []string // the pattern matched nothing in the main checkout
	Cleared   []string // holt's own symlink, removed with its file gone from the main checkout
}

type WorktreeResult struct {
	Worktree worktree.Worktree
	Result   Result
	Gone     bool   // git still lists it, but its directory is not there
	Moved    string // gone from where git records it, and sitting here instead
	Err      error  // this worktree could not be mirrored into
}

var ErrWorktreeGone = errors.New("the worktree directory is not there")

// A path git still lists as a worktree that now holds a repository of its own,
// which reusing the directory of one deleted by hand leaves. Written into, holt
// puts its symlinks in somebody else's project, untracked and committable.
var ErrForeignWorktree = errors.New("the directory is not a worktree of this repository any more")

// A bare repository has no working tree, so git's listing names the repository
// directory instead. Mirrored from there a pattern reaches git's own objects and
// refs, and a wide enough glob writes "/*" into the shared info/exclude, after
// which nothing untracked shows up anywhere.
var ErrBareRepository = errors.New("a bare repository holds no files to mirror, only the repository itself")

// MainCheckout is the directory the mirrored files are read from.
func MainCheckout(repo *git.Repo) (worktree.Worktree, error) {
	main, err := worktree.Main(repo)
	if err != nil {
		return main, err
	}
	if main.Bare {
		return main, fmt.Errorf("%w: %s", ErrBareRepository, main.Path)
	}
	return main, nil
}

// Sync covers every worktree but the main checkout.
func Sync(repo *git.Repo, patterns []string) ([]WorktreeResult, error) {
	if _, err := MainCheckout(repo); err != nil {
		return nil, err
	}
	worktrees, err := worktree.List(repo)
	if err != nil {
		return nil, err
	}

	mainCheckout := worktrees[0].Path
	results := make([]WorktreeResult, 0, len(worktrees)-1)
	for _, w := range worktrees[1:] {
		// A directory that is gone is reported as gone below, with its prune.
		if _, err := os.Stat(w.Path); err == nil && !worktree.SameRepository(repo, w.Path) {
			results = append(results, WorktreeResult{Worktree: w, Err: fmt.Errorf("%w: %s", ErrForeignWorktree, w.Path)})
			continue
		}
		result, err := link(mainCheckout, w.Path, patterns)
		// One unreadable worktree must not cost the repair of the rest.
		switch {
		case errors.Is(err, ErrWorktreeGone):
			results = append(results, WorktreeResult{
				Worktree: w,
				Gone:     true,
				Moved:    worktree.RelocatedPath(repo, mainCheckout, w),
			})
		case err != nil:
			results = append(results, WorktreeResult{Worktree: w, Result: result, Err: err})
		default:
			results = append(results, WorktreeResult{Worktree: w, Result: result})
		}
	}
	return results, nil
}

// SyncOne covers a single worktree, which is all the post-checkout hook needs.
func SyncOne(repo *git.Repo, patterns []string, worktreePath string) (Result, error) {
	if _, err := MainCheckout(repo); err != nil {
		return Result{}, err
	}
	worktrees, err := worktree.List(repo)
	if err != nil {
		return Result{}, err
	}
	// By identity: git records the path resolved and absolute, so a relative or
	// trailing-slash spelling would be turned away as no worktree at all.
	given, err := os.Stat(worktreePath)
	if errors.Is(err, fs.ErrNotExist) {
		return Result{}, fmt.Errorf("%s is not a worktree of this repository", worktreePath)
	}
	if err != nil {
		return Result{}, fmt.Errorf("reading %s: %w", worktreePath, err)
	}
	known := slices.IndexFunc(worktrees, func(w worktree.Worktree) bool {
		listed, statErr := os.Stat(w.Path)
		return statErr == nil && os.SameFile(given, listed)
	})
	if known < 0 {
		return Result{}, fmt.Errorf("%s is not a worktree of this repository", worktreePath)
	}
	if !worktree.SameRepository(repo, worktrees[known].Path) {
		return Result{}, fmt.Errorf("%w: %s", ErrForeignWorktree, worktrees[known].Path)
	}
	// git's spelling, so links and reports below all use the one form.
	return link(worktrees[0].Path, worktrees[known].Path, patterns)
}

// link takes away only its own symlinks and directories holding no more than
// those, an empty one included; anything else is reported and left alone.
func link(mainCheckout, worktreePath string, patterns []string) (Result, error) {
	var result Result
	if worktreePath == mainCheckout {
		return result, nil
	}
	// Linking makes parents, so a deleted worktree would come back holding only
	// symlinks. An unreadable one is not gone: prune would be wrong advice.
	if _, err := os.Stat(worktreePath); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return result, fmt.Errorf("%w: %s", ErrWorktreeGone, worktreePath)
		}
		return result, fmt.Errorf("reading %s: %w", worktreePath, err)
	}

	// A pattern holt cannot get past costs only itself; failures go out at the end.
	var failed error
	var paths []string
	seen := make(map[string]bool)
	for _, pattern := range patterns {
		relatives, err := expand(mainCheckout, pattern)
		if err != nil {
			failed = errors.Join(failed, err)
			continue
		}
		if len(relatives) == 0 {
			result.Unmatched = append(result.Unmatched, pattern)
			continue
		}
		for _, rel := range relatives {
			// Two patterns reaching one file would print a line each, every checkout.
			if !seen[rel] {
				seen[rel] = true
				paths = append(paths, rel)
			}
		}
	}

	// Shallowest first, or a directory reached second takes over what the inner
	// path's link just made, undoing it on every checkout. This order leaves the
	// inner path covered by its parent, the one link the worktree keeps.
	slices.SortStableFunc(paths, func(a, b string) int {
		return strings.Count(a, sep) - strings.Count(b, sep)
	})

	for _, rel := range paths {
		target := filepath.Join(worktreePath, rel)
		if escapesWorktree(worktreePath, target) {
			if !coveredByMirroredParent(mainCheckout, rel, target) {
				result.Blocked = append(result.Blocked, rel)
			}
			continue
		}
		linked, err := linkOne(filepath.Join(mainCheckout, rel), target, rel)
		if err != nil {
			failed = errors.Join(failed, err)
			continue
		}
		if linked {
			result.Linked = append(result.Linked, rel)
		} else {
			result.Blocked = append(result.Blocked, rel)
		}
	}

	// With the file gone the loop above never reaches its link, which every worktree
	// keeps leading nowhere; the file coming back brings the link with it.
	stale, err := staleLinks(mainCheckout, worktreePath, patterns)
	failed = errors.Join(failed, err)
	for _, rel := range stale {
		target := filepath.Join(worktreePath, rel)
		if err := os.Remove(target); err != nil {
			failed = errors.Join(failed, err)
			continue
		}
		result.Cleared = append(result.Cleared, rel)
		pruneEmptyParents(worktreePath, target)
	}
	return result, failed
}

// staleLinks returns the links holt wrote for a file the main checkout no longer
// has. Walked in the worktree, since the patterns expand where the file is not.
func staleLinks(mainCheckout, worktreePath string, patterns []string) ([]string, error) {
	var stale []string
	// An explicit path and a glob covering it name one link twice: the first
	// removal takes it, the second finds nothing, and the cleanup reads as failed.
	seen := make(map[string]bool)
	for _, pattern := range patterns {
		matches, err := globUnder(worktreePath, pattern)
		if err != nil {
			return stale, err
		}
		for _, match := range matches {
			rel, err := filepath.Rel(worktreePath, match)
			if err != nil {
				return stale, err
			}
			source := filepath.Join(mainCheckout, rel)
			if _, err := os.Lstat(source); !errors.Is(err, fs.ErrNotExist) {
				continue
			}
			// Only holt's own: a dangling link of the user's is theirs to keep.
			actual, err := os.Readlink(match)
			if err != nil || !sameFile(actual, source, filepath.Dir(match)) {
				continue
			}
			if seen[rel] {
				continue
			}
			seen[rel] = true
			stale = append(stale, rel)
		}
	}
	return stale, nil
}

// Unsync reports how many links went and from how many worktrees. One unreadable
// worktree must not cost the removal from the rest: the list is already saved, so a
// link skipped here is one no later "mirror rm" will offer to take away.
func Unsync(repo *git.Repo, patterns []string) (links, worktrees int, err error) {
	all, err := worktree.List(repo)
	if err != nil {
		return 0, 0, err
	}

	mainCheckout := all[0].Path
	var failed error
	for _, w := range all[1:] {
		unlinked, unlinkErr := unlink(mainCheckout, w.Path, patterns)
		// Counted first: those links are already gone and cannot be undone.
		links += len(unlinked)
		if len(unlinked) > 0 {
			worktrees++
		}
		if unlinkErr != nil {
			failed = errors.Join(failed, fmt.Errorf("unmirroring from %s: %w", w.Path, unlinkErr))
		}
	}
	return links, worktrees, failed
}

// Only links still pointing at the main checkout go, so hand-made files survive.
// What came away is returned even when something else did not.
func unlink(mainCheckout, worktreePath string, patterns []string) ([]string, error) {
	var removed []string
	// Read rather than stat: an unopenable directory stats fine, and Glob then reports
	// no matches and no error, while a directory that is gone took its links with it.
	if _, err := os.ReadDir(worktreePath); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return removed, err
	}
	var failed error
	for _, pattern := range patterns {
		// Expanded in the worktree, which finds links whose source is already gone.
		matches, err := globUnder(worktreePath, pattern)
		if err != nil {
			failed = errors.Join(failed, fmt.Errorf("pattern %s: %w", pattern, err))
			continue
		}
		for _, match := range matches {
			rel, err := filepath.Rel(worktreePath, match)
			if err != nil {
				failed = errors.Join(failed, err)
				continue
			}
			if escapesWorktree(worktreePath, match) {
				continue
			}
			// Inspect's own test, so what holt calls mirrored is what it unlinks. Not
			// linkOne's wider match: there it repoints a link, here it would delete one.
			actual, err := os.Readlink(match)
			if err != nil || !sameFile(actual, filepath.Join(mainCheckout, rel), filepath.Dir(match)) {
				continue
			}
			if err := os.Remove(match); err != nil {
				failed = errors.Join(failed, err)
				continue
			}
			removed = append(removed, rel)
			pruneEmptyParents(worktreePath, match)
		}
	}
	return removed, failed
}

// pruneEmptyParents takes back the directory linkOne made for a pattern inside one,
// which the link going leaves empty: nothing tracks or lists it, so unmirroring
// would otherwise leave a directory of holt's own in every worktree.
func pruneEmptyParents(worktreePath, target string) {
	for dir := filepath.Dir(target); dir != worktreePath; dir = filepath.Dir(dir) {
		rel, err := filepath.Rel(worktreePath, dir)
		if err != nil || !filepath.IsLocal(rel) {
			return
		}
		// Only a real directory: os.Remove takes a symlink whatever it points at.
		info, err := os.Lstat(dir)
		if err != nil || !info.IsDir() {
			return
		}
		// A directory holding something is not holt's, and its removal fails here.
		if os.Remove(dir) != nil {
			return
		}
	}
}

type Check struct {
	Linked   []string // symlink present and pointing at the main checkout
	Absent   []string // nothing is there; never made, or deleted
	Blocked  []string // a real file or directory occupies the path
	Diverted []string // a symlink pointing somewhere other than the main checkout
	Stale    []string // holt's own symlink, to a file the main checkout no longer has
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
				if !coveredByMirroredParent(mainCheckout, rel, target) {
					check.Blocked = append(check.Blocked, rel)
				}
				continue
			}
			info, err := os.Lstat(target)
			switch {
			case errors.Is(err, os.ErrNotExist):
				check.Absent = append(check.Absent, rel)
			case errors.Is(err, syscall.ENOTDIR):
				check.Blocked = append(check.Blocked, rel)
			case err != nil:
				return check, err
			case info.Mode()&os.ModeSymlink == 0:
				// A directory of holt's own links reads as absent, which is what
				// the next sync makes of it; blocked would name a stranger.
				if info.IsDir() && onlyOurLinks(filepath.Join(mainCheckout, rel), target) {
					check.Absent = append(check.Absent, rel)
					continue
				}
				check.Blocked = append(check.Blocked, rel)
			default:
				actual, err := os.Readlink(target)
				if err != nil {
					return check, err
				}
				if sameFile(actual, filepath.Join(mainCheckout, rel), filepath.Dir(target)) {
					check.Linked = append(check.Linked, rel)
				} else {
					check.Diverted = append(check.Diverted, rel)
				}
			}
		}
	}

	// Links holt made for a file since deleted, reported rather than passed over, or
	// doctor calls a set of links complete while every one leads nowhere.
	stale, err := staleLinks(mainCheckout, worktreePath, patterns)
	if err != nil {
		return check, err
	}
	check.Stale = stale
	return check, nil
}

// TrackedMatches returns the paths a pattern reaches that git already tracks, and
// so writes into every worktree itself, leaving the link nowhere to go.
func TrackedMatches(repo *git.Repo, mainCheckout, pattern string) ([]string, error) {
	relatives, err := expand(mainCheckout, pattern)
	if err != nil {
		return nil, err
	}
	if len(relatives) == 0 {
		return nil, nil
	}
	// --literal-pathspecs: these names are resolved on disk already, and one holding
	// a star would match other files holt would then tell the user to untrack.
	args := append([]string{"--literal-pathspecs", "ls-files", "-z", "--"}, relatives...)
	out, err := repo.At(mainCheckout).Output(args...)
	if err != nil {
		return nil, err
	}

	var tracked []string
	for name := range strings.SplitSeq(strings.TrimSuffix(out, "\x00"), "\x00") {
		if name != "" {
			tracked = append(tracked, filepath.FromSlash(name))
		}
	}
	return tracked, nil
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

// onlyOurLinks reports whether the directory holds nothing beyond holt's own links
// and directories of the same. Anything of the user's, and it is not holt's to
// take; an empty one is, whoever made it, since there is nothing to lose.
func onlyOurLinks(source, target string) bool {
	entries, err := os.ReadDir(target)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		child := filepath.Join(target, entry.Name())
		want := filepath.Join(source, entry.Name())
		actual, err := os.Readlink(child)
		if err == nil {
			if !sameFile(actual, want, filepath.Dir(child)) {
				return false
			}
			continue
		}
		// IsDir is false for a symlink to one, which the branch above took.
		if !entry.IsDir() || !onlyOurLinks(want, child) {
			return false
		}
	}
	return true
}

// sameFile reports whether a symlink points at the file holt would have linked.
// Asked of the filesystem, not of the strings: the list carries the user's spelling
// and the link holt's own, which a folding filesystem resolves to one file.
//
// linkDir is the directory holding the link, which is what a relative target is
// measured from; holt's working directory would move the answer with the user.
func sameFile(target, source, linkDir string) bool {
	if target == source {
		return true
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(linkDir, target)
	}
	// Lstat, not Stat: the symlink is compared as itself, or a link the user aimed at
	// an alias of the mirrored file is taken for holt's own and unlinked.
	targetInfo, err := os.Lstat(target)
	if err != nil {
		return false
	}
	sourceInfo, err := os.Lstat(source)
	return err == nil && os.SameFile(targetInfo, sourceInfo)
}

// reclaimable reports whether linkOne may replace the symlink at target: the same
// repo-relative path under a main checkout that is gone, which a move strands. One
// that resolves is somebody's own, and holt's own links are never relative.
func reclaimable(actual, rel, target string) bool {
	// Only a link leading nowhere: one that still reaches something says nothing.
	if _, err := os.Stat(target); !errors.Is(err, fs.ErrNotExist) {
		return false
	}
	return filepath.IsAbs(actual) && strings.HasSuffix(actual, sep+rel)
}

// globUnder escapes the directory before joining the pattern on, or a "[" in the
// repository's own path reads as syntax and matches nothing.
func globUnder(dir, pattern string) ([]string, error) {
	return filepath.Glob(filepath.Join(escapeGlob(dir), pattern))
}

func expand(mainCheckout, pattern string) ([]string, error) {
	matches, err := globUnder(mainCheckout, pattern)
	if err != nil {
		return nil, fmt.Errorf("pattern %s: %w", pattern, err)
	}

	relatives := make([]string, 0, len(matches))
	for _, match := range matches {
		rel, err := filepath.Rel(mainCheckout, match)
		if err != nil {
			return nil, err
		}
		// A glob wide enough to take in .git, which every worktree keeps its own
		// of, so the entry could only ever report itself blocked.
		if first, _, _ := strings.Cut(rel, sep); strings.EqualFold(first, ".git") {
			continue
		}
		relatives = append(relatives, rel)
	}
	return relatives, nil
}

// A mirrored directory is a symlink into the main checkout, so anything under it
// resolves there; a missing parent is one linkOne makes in the worktree.
func escapesWorktree(worktreePath, target string) bool {
	parent, err := filepath.EvalSymlinks(filepath.Dir(target))
	return err == nil && !under(parent, worktreePath)
}

// coveredByMirroredParent separates the one harmless escape: the parent already
// leads where this path would have been linked from, which is what a list holding
// a directory and a path inside it leaves.
func coveredByMirroredParent(mainCheckout, rel, target string) bool {
	parent, err := filepath.EvalSymlinks(filepath.Dir(target))
	if err != nil {
		return false
	}
	mirrored, err := filepath.EvalSymlinks(filepath.Join(mainCheckout, filepath.Dir(rel)))
	return err == nil && parent == mirrored
}

// linkOne returns false when something holt did not create is in the way: a
// real file or directory, or a symlink it may not reclaim.
func linkOne(source, target, rel string) (bool, error) {
	info, err := os.Lstat(target)
	switch {
	case errors.Is(err, os.ErrNotExist):
	case errors.Is(err, syscall.ENOTDIR):
		// A real file where the path needs a directory, which a branch tracking the
		// parent as a file leaves: something in the way, not a broken worktree.
		return false, nil
	case err != nil:
		return false, err
	case info.Mode()&os.ModeSymlink == 0:
		// A directory holt made to hold a link for a path inside it, now that the
		// list names the directory too: only holt's links are in it, and left
		// standing it is called a stranger's on every checkout.
		if !info.IsDir() || !onlyOurLinks(source, target) {
			return false, nil
		}
		if err := os.RemoveAll(target); err != nil {
			return false, err
		}
	default:
		actual, err := os.Readlink(target)
		if err != nil {
			return false, err
		}
		// The hook runs on every checkout, so a correct symlink is left alone.
		if sameFile(actual, source, filepath.Dir(target)) {
			return true, nil
		}
		if !reclaimable(actual, rel, target) {
			return false, nil
		}
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
