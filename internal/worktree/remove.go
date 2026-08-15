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

// BranchOutcome is what became of a branch after its worktree was removed.
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
	return repo.Run(progress, "worktree", "remove", path)
}

// git allows deleting the directory the caller stands in, leaving the shell
// somewhere that no longer exists.
func refuseIfInside(path string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	// git reports resolved paths, so the caller's own has to be resolved too.
	resolved, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		return err
	}

	relative, err := filepath.Rel(path, resolved)
	if err != nil || !filepath.IsLocal(relative) {
		return nil
	}
	return fmt.Errorf("you are inside %s; leave it first with %q", path, "holt home")
}

// Remove's safety net does not cover ignored files: git deletes them without a
// word, and a .env sits among the build output with no copy anywhere else.
func IgnoredFiles(repo *git.Repo, path string) ([]string, error) {
	// git goes on listing a worktree whose directory was deleted, and running
	// status in one fails outright. There is nothing left in it to lose.
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}

	// --ignored=matching collapses a wholly ignored directory into one entry.
	out, err := repo.At(path).Output("--no-optional-locks", "status", "--porcelain", "-z", "--ignored=matching")
	if err != nil {
		return nil, err
	}

	var ignored []string
	for entry := range strings.SplitSeq(out, "\x00") {
		if name, found := strings.CutPrefix(entry, "!! "); found {
			ignored = append(ignored, name)
		}
	}
	return ignored, nil
}

// The ancestry is checked here rather than by "git branch --delete", which
// compares against the upstream, or HEAD when there is none. A branch pushed
// with "git push -u" is its own upstream, so git would drop it the moment it
// was pushed, with the default branch never having seen those commits.
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

	// Safe: the ancestry check proved every commit survives on the default
	// branch, and plain --delete would re-ask the upstream.
	if _, err := repo.Output("branch", "--delete", "--force", branch); err != nil {
		return "", err
	}
	return BranchDeleted, nil
}
