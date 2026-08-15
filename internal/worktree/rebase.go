package worktree

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/MaxSiominDev/holt/internal/git"
)

var ErrRebaseStopped = errors.New("the rebase stopped before it finished")

// Rebase never pushes: force-pushing the rewritten history is the user's call.
func Rebase(repo *git.Repo, progress io.Writer) error {
	// The offline guards come first, so a doomed rebase costs no round trip.
	if err := checkWorktreeReady(repo); err != nil {
		return err
	}

	// This command fetches anyway, so origin can be asked which branch is default.
	defaultBranch, err := FetchDefaultBranch(repo, progress)
	if err != nil {
		return err
	}
	branch, err := CurrentBranch(repo)
	if err != nil {
		return err
	}
	if branch == defaultBranch {
		return fmt.Errorf("this worktree is on %s, which is the branch a rebase would move onto", defaultBranch)
	}

	if err := repo.Run(progress, "rebase", "origin/"+defaultBranch); err != nil {
		// git also exits non-zero when it declines to start, a pre-rebase hook
		// refusing for one, and then there is no rebase to continue or abort.
		if operation, checkErr := operationInProgress(repo); checkErr != nil || operation != "rebase" {
			return err
		}
		return fmt.Errorf("%w: resolve the conflict and run %q, or %q to put the branch back. holt does not touch a rebase in progress",
			ErrRebaseStopped, "git rebase --continue", "git rebase --abort")
	}
	return nil
}

// Every state where a rebase would destroy something or stop halfway, as far as
// that can be told without asking origin.
func checkWorktreeReady(repo *git.Repo) error {
	// git detaches HEAD for the duration of a rebase, so asking for the current
	// branch first would fail with "HEAD is not a symbolic ref" and hide this.
	operation, err := operationInProgress(repo)
	if err != nil {
		return err
	}
	if operation != "" {
		return fmt.Errorf("a %s is already in progress here, finish or abort it first", operation)
	}

	// Untracked files are left out: a rebase does not touch them.
	dirty, err := repo.Output("--no-optional-locks", "status", "--porcelain", "--untracked-files=no")
	if err != nil {
		return err
	}
	if dirty != "" {
		return errors.New("this worktree has uncommitted changes, commit or stash them first")
	}
	return nil
}

func CurrentBranch(repo *git.Repo) (string, error) {
	branch, err := repo.Output("symbolic-ref", "--short", "HEAD")
	if err != nil {
		return "", fmt.Errorf("this worktree has no branch checked out: %w", err)
	}
	return branch, nil
}

// Names the half-finished git operation by its marker file.
func operationInProgress(repo *git.Repo) (string, error) {
	gitDir, err := repo.GitDir()
	if err != nil {
		return "", err
	}

	markers := []struct {
		file      string
		operation string
	}{
		{"rebase-merge", "rebase"},
		{"rebase-apply", "rebase"},
		{"MERGE_HEAD", "merge"},
		{"CHERRY_PICK_HEAD", "cherry-pick"},
		{"REVERT_HEAD", "revert"},
	}
	for _, marker := range markers {
		if _, err := os.Lstat(filepath.Join(gitDir, marker.file)); err == nil {
			return marker.operation, nil
		}
	}
	return "", nil
}
