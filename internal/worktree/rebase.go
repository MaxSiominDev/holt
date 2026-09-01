package worktree

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/MaxSiominDev/holt/internal/config"
	"github.com/MaxSiominDev/holt/internal/git"
)

var ErrRebaseStopped = errors.New("the rebase stopped before it finished")

// Rebase never pushes: force-pushing the rewritten history is the user's call.
// The merge list names the files holt settles itself; without one every conflict
// is the user's.
func Rebase(repo *git.Repo, abortOnConflict bool, merge *config.MergeList, progress io.Writer) error {
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
	if err := repo.Run(progress, append(adviceOff(abortOnConflict), "rebase", "refs/remotes/origin/"+defaultBranch)...); err != nil {
		// git also exits non-zero when it declines to start, a pre-rebase hook
		// refusing for one, leaving no rebase to continue or abort.
		if operation, checkErr := OperationInProgress(repo); checkErr != nil || operation != "rebase" {
			return err
		}
		return finishStoppedRebase(repo, abortOnConflict, merge, progress)
	}
	return nil
}

// finishStoppedRebase carries the rebase as far as holt can: the files the merge
// list names are settled here and the rebase goes on, once per commit it stops
// at. The first conflict holt will not touch ends it. Only the rebase holt
// started in this run is ever continued, since one already in progress fails a
// precondition above.
func finishStoppedRebase(repo *git.Repo, abortOnConflict bool, merge *config.MergeList, progress io.Writer) error {
	var settled []string
	for {
		merged, refused := autoMerge(repo, merge)
		// A file the branch touches in several commits is settled at every stop
		// and worth naming once.
		for _, file := range merged {
			if !slices.Contains(settled, file) {
				settled = append(settled, file)
			}
		}
		if refused == nil && len(merged) == 0 {
			// Nothing unmerged means git stopped for something else: a commit that
			// came out empty, or a state a hand resolution left it in. holt has no
			// diagnosis of its own to add, so it sends the reader to git's, without
			// saying where it went: the caller owns that stream.
			refused = errors.New("nothing was left unmerged, so what stopped git was not a conflict at all, and git's own message says what it was")
		}
		if refused != nil {
			// An abort takes back every commit of this rebase, the ones holt
			// settled among them, so it says nothing about any of them.
			if abortOnConflict {
				return abortStoppedRebase(repo, refused, settled, progress)
			}
			announceMerged(progress, settled)
			return fmt.Errorf("%w: %w. Resolve it and run %q, or %q to put the branch back",
				ErrRebaseStopped, refused, "git rebase --continue", "git rebase --abort")
		}

		// git opens an editor on the message of the commit it just finished.
		err := repo.RunWithoutEditor(progress, append(adviceOff(abortOnConflict), "rebase", "--continue")...)
		if err == nil {
			announceMerged(progress, settled)
			return nil
		}
		// A rebase stops once per commit, so the next commit's conflict lands here.
		if operation, checkErr := OperationInProgress(repo); checkErr != nil || operation != "rebase" {
			return err
		}
	}
}

// git's conflict advice names a --continue, a --skip and an abort of the user's
// own. On the aborting kind of run none of the three is the user's to make: holt
// either settles the conflict and carries on, or puts the branch back. --no-abort
// keeps the advice, since a conflict holt does not settle is then left standing
// for the user to finish by hand.
func adviceOff(abortOnConflict bool) []string {
	if abortOnConflict {
		return []string{"-c", "advice.mergeConflict=false"}
	}
	return nil
}

func announceMerged(progress io.Writer, merged []string) {
	if len(merged) > 0 {
		fmt.Fprintf(progress, "holt: merged %s\n", strings.Join(merged, ", "))
	}
}

// git's hints about resolving the conflict or aborting it by hand are already on
// screen by now, so the message says outright that the abort has happened.
// The merged names are for the one case that keeps them: an abort that fails
// leaves holt's own writes standing with the rebase they were made in.
func abortStoppedRebase(repo *git.Repo, reason error, merged []string, progress io.Writer) error {
	// Read while the conflict is still there, since the abort takes the unmerged
	// entries with it. A listing that fails is dropped rather than kept from the
	// abort, which is the part that was asked for. diff.relative, which the user
	// may well have set, would cut the list down to the directory holt was run
	// from and say nothing about the rest.
	conflicted, _ := repo.Output("-c", "diff.relative=false", "--no-optional-locks", "diff", "--name-only", "--diff-filter=U")
	// Said before the abort rather than after it, since an abort that fails leaves
	// the user standing in the conflict this names.
	if conflicted != "" {
		fmt.Fprintf(progress, "holt: conflicts in %s\n", strings.ReplaceAll(conflicted, "\n", ", "))
	}

	if err := repo.Run(progress, "rebase", "--abort"); err != nil {
		announceMerged(progress, merged)
		return fmt.Errorf("%w: %w, and the abort failed as well, so the worktree is still in it: %w", ErrRebaseStopped, reason, err)
	}
	return fmt.Errorf("%w: %w. The branch is back where it was. Run %q to stop in the conflict and resolve it instead",
		ErrRebaseStopped, reason, "holt rebase --no-abort")
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
