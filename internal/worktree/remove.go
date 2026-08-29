package worktree

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/MaxSiominDev/holt/internal/git"
)

// BranchOutcome records what became of a branch once its worktree was removed.
type BranchOutcome string

const (
	BranchDeleted          BranchOutcome = "deleted"
	BranchKeptUnmerged     BranchOutcome = "unmerged"
	BranchKeptUnverifiable BranchOutcome = "unverifiable" // no default branch to compare against
)

// --force is never passed: git's refusal over uncommitted work is the safety net.
func Remove(repo *git.Repo, path string, progress io.Writer) error {
	if err := refuseIfInside(path); err != nil {
		return err
	}
	main, err := Main(repo)
	if err != nil {
		return err
	}
	if err := repo.Run(progress, "worktree", "remove", path); err != nil {
		return err
	}
	pruneEmptyParents(worktreesRoot(main.Path), path)
	return nil
}

// A branch named feature/x leaves a "feature" directory git does not take back,
// which blocks "holt new feature" for good with nothing listing or clearing it.
func pruneEmptyParents(root, path string) {
	// Both sides resolved before comparing: git records a worktree with its symlinks
	// taken out while holt builds the root from the path it was reached by.
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		return
	}
	for dir := filepath.Dir(path); ; dir = filepath.Dir(dir) {
		resolved, err := filepath.EvalSymlinks(dir)
		if err != nil || resolved == root {
			return
		}
		rel, err := filepath.Rel(root, resolved)
		if err != nil || !filepath.IsLocal(rel) {
			return
		}
		// Only a real directory of holt's own layout: os.Remove takes a symlink whatever
		// it points at, and holt does not delete what it did not create.
		info, err := os.Lstat(dir)
		if err != nil || !info.IsDir() {
			return
		}
		// Removing a directory that still holds something fails, which ends it.
		if os.Remove(dir) != nil {
			return
		}
	}
}

// git will delete the directory the caller stands in, stranding their shell.
func refuseIfInside(path string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	resolved, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		return err
	}

	if !inside(resolved, path) {
		return nil
	}
	return fmt.Errorf("you are inside %s; leave it first with %q", path, "holt home")
}

// Whether directory is ancestor or sits under it, asked of the filesystem rather
// than the strings, which on a case-folding one differ for the very same place.
func inside(directory, ancestor string) bool {
	want, err := os.Stat(ancestor)
	if err != nil {
		return false
	}
	for {
		here, err := os.Stat(directory)
		if err == nil && os.SameFile(here, want) {
			return true
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			return false
		}
		directory = parent
	}
}

// Remove's safety net misses ignored files: git deletes them without a word, and
// a .env sits among the build output with no copy anywhere else.
func IgnoredFiles(repo *git.Repo, path string) ([]string, error) {
	// git still lists a worktree whose directory was deleted, where status exits
	// non-zero and there is nothing left to lose.
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}

	// --ignored=matching rather than git's default, or a directory merely full of ignored
	// files vanishes into one entry. One matching a pattern itself still arrives whole,
	// and what is inside it is the caller's to judge.
	out, err := repo.At(path).Output("--no-optional-locks", "status", "--porcelain", "-z", "--ignored=matching")
	if err != nil {
		// A worktree whose gitdir leads nowhere, which "holt ls" shows as broken; git's
		// own words would be about a status command the user never ran.
		return nil, fmt.Errorf("cannot read %s to see what removing it would take: %w", path, err)
	}

	var ignored []string
	for entry := range strings.SplitSeq(out, "\x00") {
		if name, found := strings.CutPrefix(entry, "!! "); found {
			ignored = append(ignored, name)
		}
	}
	return ignored, nil
}

// Ancestry is checked here, not by "git branch --delete", which compares against the
// upstream: a branch pushed with -u is its own upstream, so git would drop it then.
func DeleteMergedBranch(repo *git.Repo, branch string) (BranchOutcome, error) {
	defaultBranch, err := DefaultBranch(repo)
	if errors.Is(err, ErrNoDefaultBranch) {
		return BranchKeptUnverifiable, nil
	}
	if err != nil {
		return "", err
	}

	// refs/heads/ rather than the bare name, which git resolves as a tag first.
	if _, err := repo.Output("merge-base", "--is-ancestor",
		"refs/heads/"+branch, "refs/remotes/origin/"+defaultBranch); err != nil {
		var exit *git.ExitError
		if errors.As(err, &exit) && exit.Code == 1 {
			return BranchKeptUnmerged, nil
		}
		return "", err
	}

	// Safe: ancestry proved the commits survive, and --delete would re-ask the upstream.
	if _, err := repo.Output("branch", "--delete", "--force", branch); err != nil {
		return "", err
	}
	return BranchDeleted, nil
}
