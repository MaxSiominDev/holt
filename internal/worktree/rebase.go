package worktree

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

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

	// The full name of the remote-tracking ref: a tag or local branch called
	// origin/main outranks it, and git would rebase onto that one and exit zero.
	if err := repo.Run(progress, "rebase", "refs/remotes/origin/"+defaultBranch); err != nil {
		// git also exits non-zero when it declines to start, a pre-rebase hook
		// refusing for one, leaving no rebase to continue or abort.
		if operation, checkErr := OperationInProgress(repo); checkErr != nil || operation != "rebase" {
			return err
		}
		return fmt.Errorf("%w: resolve the conflict and run %q, or %q to put the branch back. holt does not touch a rebase in progress",
			ErrRebaseStopped, "git rebase --continue", "git rebase --abort")
	}
	return nil
}

// Every state where a rebase would destroy something or stop halfway, as far as
// can be told without asking origin.
func checkWorktreeReady(repo *git.Repo) error {
	// git detaches HEAD during a rebase, so asking for the branch first would fail
	// with "HEAD is not a symbolic ref" and hide this.
	operation, err := OperationInProgress(repo)
	if err != nil {
		return err
	}
	if operation == "bisect" {
		return errors.New("this worktree has an unfinished bisect, end it with \"git bisect reset\" first")
	}
	if operation != "" {
		// "an unfinished %s" keeps the article right for "am" too.
		return fmt.Errorf("this worktree has an unfinished %s, finish or abort it first", operation)
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

// The full ref, not --short: shortening resolves tags first, so a same-named tag
// turns feature into heads/feature and push writes refs/heads/heads/feature.
func CurrentBranch(repo *git.Repo) (string, error) {
	ref, err := repo.Output("symbolic-ref", "HEAD")
	if err != nil {
		// git parks HEAD on a commit throughout these, and "no branch checked out"
		// would send the user looking for one "holt ls" still shows them.
		if operation, checkErr := OperationInProgress(repo); checkErr == nil && operation != "" {
			return "", fmt.Errorf("this worktree is stopped in a %s, so it is on no branch until that is finished or undone; \"git status\" there says which", operation)
		}
		return "", fmt.Errorf("this worktree has no branch checked out: %w", err)
	}
	return strings.TrimPrefix(ref, "refs/heads/"), nil
}

// OperationInProgress names the half-finished git operation by the marker git leaves,
// empty when there is none. git takes them all down with the worktree and refuses only
// over a dirty tree, so one stopped with a clean tree has to be found this way.
func OperationInProgress(repo *git.Repo) (string, error) {
	gitDir, err := repo.GitDir()
	if err != nil {
		return "", err
	}

	markers := []struct {
		file      string
		operation string
	}{
		{"rebase-merge", "rebase"},
		// "git am" and "git rebase --apply" share this directory and are told apart by a
		// file inside it: calling an am a rebase sends the user to a command that refuses.
		{filepath.Join("rebase-apply", "applying"), "am"},
		{"rebase-apply", "rebase"},
		{"MERGE_HEAD", "merge"},
		{"CHERRY_PICK_HEAD", "cherry-pick"},
		{"REVERT_HEAD", "revert"},
		// A bisect leaves a clean tree at every step, so git takes it down without a word.
		{"BISECT_LOG", "bisect"},
	}
	for _, marker := range markers {
		if _, err := os.Lstat(filepath.Join(gitDir, marker.file)); err == nil {
			return marker.operation, nil
		}
	}

	// The heads above last only while git is stopped on one commit, and a conflict
	// resolved by hand clears them while the sequencer keeps the rest.
	return pendingSequence(gitDir)
}

// The operation a sequencer holds commits for: a revert writes "revert" at the head
// of each todo line, so anything else here is a cherry-pick.
func pendingSequence(gitDir string) (string, error) {
	todo, err := os.ReadFile(filepath.Join(gitDir, "sequencer", "todo"))
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	first, _, _ := strings.Cut(string(todo), "\n")
	if word, _, _ := strings.Cut(first, " "); word == "revert" {
		return "revert", nil
	}
	if strings.TrimSpace(first) == "" {
		return "", nil
	}
	return "cherry-pick", nil
}
