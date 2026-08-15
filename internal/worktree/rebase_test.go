package worktree

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MaxSiominDev/holt/internal/git"
	"github.com/MaxSiominDev/holt/internal/testutil"
)

func TestRebaseOntoFetchedDefault(t *testing.T) {
	clone, origin := testutil.NewClonedRepo(t)
	feature := branchWorktree(t, clone, "feature", "work.txt", "my work\n")
	// The default branch moves on after the worktree was made.
	upstream := testutil.CommitTo(t, origin, "upstream.txt", "someone else's work\n")

	if err := Rebase(open(t, feature), io.Discard); err != nil {
		t.Fatal(err)
	}

	parent := testutil.Git(t, feature, "rev-parse", "HEAD~1")
	if parent != upstream {
		t.Fatalf("the branch sits on %s, want it replanted onto %s", parent, upstream)
	}
	if _, err := os.Stat(filepath.Join(feature, "work.txt")); err != nil {
		t.Error("the branch's own commit was lost")
	}
}

func TestRebaseStaleOriginHead(t *testing.T) {
	clone, origin := testutil.NewClonedRepo(t)
	feature := branchWorktree(t, clone, "feature", "work.txt", "my work\n")
	// The default at clone time and not any more, and it still resolves here.
	testutil.Git(t, origin, "branch", "initial-pr")
	testutil.AddRemoteBranch(t, clone, "initial-pr")
	testutil.Git(t, clone, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/initial-pr")
	upstream := testutil.CommitTo(t, origin, "upstream.txt", "main has moved on\n")

	if err := Rebase(open(t, feature), io.Discard); err != nil {
		t.Fatal(err)
	}

	// The abandoned branch is not what this work has to merge into.
	if parent := testutil.Git(t, feature, "rev-parse", "HEAD~1"); parent != upstream {
		t.Fatalf("the branch sits on %s, want the tip of origin's real default branch %s", parent, upstream)
	}
}

func TestRebaseOnDefaultBranch(t *testing.T) {
	clone, _ := testutil.NewClonedRepo(t)

	err := Rebase(open(t, clone), io.Discard)

	if err == nil {
		t.Fatal("rebasing the default branch onto itself was allowed")
	}
	if !strings.Contains(err.Error(), "main") {
		t.Errorf("error %q does not name the branch", err)
	}
}

func TestRebaseRefusesUncommitted(t *testing.T) {
	clone, _ := testutil.NewClonedRepo(t)
	feature := branchWorktree(t, clone, "feature", "work.txt", "my work\n")
	testutil.WriteFile(t, filepath.Join(feature, "work.txt"), "edited and not committed\n")

	err := Rebase(open(t, feature), io.Discard)

	if err == nil {
		t.Fatal("a rebase was started over uncommitted changes")
	}
	if !strings.Contains(err.Error(), "uncommitted") {
		t.Errorf("error %q does not say what is wrong", err)
	}
}

func TestRebaseAllowsUntrackedFiles(t *testing.T) {
	clone, origin := testutil.NewClonedRepo(t)
	feature := branchWorktree(t, clone, "feature", "work.txt", "my work\n")
	testutil.CommitTo(t, origin, "upstream.txt", "someone else's work\n")
	// A rebase does not touch untracked files.
	testutil.WriteFile(t, filepath.Join(feature, "scratch.md"), "notes\n")

	if err := Rebase(open(t, feature), io.Discard); err != nil {
		t.Fatalf("an untracked file blocked the rebase: %v", err)
	}
}

func TestRebaseDuringRebase(t *testing.T) {
	clone, origin := testutil.NewClonedRepo(t)
	feature := branchWorktree(t, clone, "feature", "shared.txt", "the branch's version\n")
	testutil.CommitTo(t, origin, "shared.txt", "the default branch's version\n")
	// A real conflict, because git detaches HEAD and a faked marker would not.
	if err := Rebase(open(t, feature), io.Discard); !errors.Is(err, ErrRebaseStopped) {
		t.Fatalf("the fixture produced %v, want a stopped rebase", err)
	}

	err := Rebase(open(t, feature), io.Discard)

	if err == nil {
		t.Fatal("a second rebase was started on top of an unfinished one")
	}
	if !strings.Contains(err.Error(), "already in progress") {
		t.Errorf("error %q does not name the unfinished operation", err)
	}
}

func TestRebaseDuringMerge(t *testing.T) {
	clone, _ := testutil.NewClonedRepo(t)
	feature := branchWorktree(t, clone, "feature", "shared.txt", "the branch's version\n")
	testutil.Git(t, feature, "switch", "--quiet", "--create", "other", "HEAD~1")
	testutil.CommitTo(t, feature, "shared.txt", "another version\n")
	testutil.Git(t, feature, "switch", "--quiet", "feature")
	if _, err := runGit(feature, "merge", "other"); err == nil {
		t.Fatal("the fixture did not produce a conflicted merge")
	}

	err := Rebase(open(t, feature), io.Discard)

	if err == nil || !strings.Contains(err.Error(), "merge is already in progress") {
		t.Fatalf("got %v, want the unfinished merge named", err)
	}
}

func TestRebaseRefusedByHook(t *testing.T) {
	clone, origin := testutil.NewClonedRepo(t)
	feature := branchWorktree(t, clone, "feature", "work.txt", "my work\n")
	testutil.CommitTo(t, origin, "upstream.txt", "someone else's work\n")
	// pre-rebase runs first, so a refusal leaves no rebase to continue or abort.
	hooks := filepath.Join(testutil.Git(t, feature, "rev-parse", "--git-common-dir"), "hooks")
	testutil.WriteFile(t, filepath.Join(hooks, "pre-rebase"), "#!/bin/sh\nexit 1\n")
	if err := os.Chmod(filepath.Join(hooks, "pre-rebase"), 0o755); err != nil {
		t.Fatal(err)
	}

	err := Rebase(open(t, feature), io.Discard)

	if err == nil {
		t.Fatal("a refused rebase was reported as success")
	}
	if errors.Is(err, ErrRebaseStopped) {
		t.Errorf("got %v, want no advice to continue a rebase that never began", err)
	}
}

func TestCurrentBranchDetachedHead(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.Git(t, main, "checkout", "--quiet", "--detach")

	_, err := CurrentBranch(open(t, main))

	if err == nil {
		t.Fatal("a detached HEAD was reported as a branch")
	}
	if !strings.Contains(err.Error(), "no branch") {
		t.Errorf("error %q does not say what is wrong", err)
	}
}

// runGit returns the error instead of ending the test, as testutil.Git would.
func runGit(dir string, args ...string) (string, error) {
	repo, err := git.Open(dir)
	if err != nil {
		return "", err
	}
	return repo.Output(args...)
}

func TestRebaseConflict(t *testing.T) {
	clone, origin := testutil.NewClonedRepo(t)
	feature := branchWorktree(t, clone, "feature", "shared.txt", "the branch's version\n")
	// The same file, changed differently on the default branch.
	testutil.CommitTo(t, origin, "shared.txt", "the default branch's version\n")

	err := Rebase(open(t, feature), io.Discard)

	if !errors.Is(err, ErrRebaseStopped) {
		t.Fatalf("got %v, want ErrRebaseStopped", err)
	}
	if !strings.Contains(err.Error(), "git rebase --abort") {
		t.Errorf("error %q does not say how to get out of it", err)
	}
	// The conflict is the user's to resolve, so holt must not clean up after git.
	if operation, _ := operationInProgress(open(t, feature)); operation != "rebase" {
		t.Errorf("got operation %q, want the stopped rebase left in place", operation)
	}
}

// branchWorktree adds a worktree on a new branch carrying one commit of its own.
func branchWorktree(t *testing.T, clone, branch, file, content string) string {
	t.Helper()
	path := testutil.AddWorktree(t, clone, branch)
	testutil.CommitTo(t, path, file, content)
	return path
}
