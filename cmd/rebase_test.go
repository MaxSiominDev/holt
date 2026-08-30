package cmd

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/MaxSiominDev/holt/internal/testutil"
)

func TestRebaseOntoDefault(t *testing.T) {
	clone, origin := testutil.NewClonedRepo(t)
	feature := testutil.AddWorktree(t, clone, "feature")
	testutil.CommitTo(t, feature, "work.txt", "my work\n")
	upstream := testutil.CommitTo(t, origin, "upstream.txt", "someone else's work\n")
	t.Chdir(feature)

	runHolt(t, "rebase")

	if parent := testutil.Git(t, feature, "rev-parse", "HEAD~1"); parent != upstream {
		t.Fatalf("the branch sits on %s, want it replanted onto %s", parent, upstream)
	}
}

func TestRebaseUncommitted(t *testing.T) {
	clone, _ := testutil.NewClonedRepo(t)
	feature := testutil.AddWorktree(t, clone, "feature")
	testutil.WriteFile(t, filepath.Join(feature, "README.md"), "edited and not committed\n")
	t.Chdir(feature)

	err := runHoltExpectingFailure(t, "rebase")

	if !strings.Contains(err.Error(), "uncommitted") {
		t.Errorf("error %q does not say what is wrong", err)
	}
}

func TestRebaseConflictPutsTheBranchBack(t *testing.T) {
	clone, origin := testutil.NewClonedRepo(t)
	feature := testutil.AddWorktree(t, clone, "feature")
	testutil.CommitTo(t, feature, "shared.txt", "the branch's version\n")

	testutil.CommitTo(t, origin, "shared.txt", "the default branch's version\n")
	t.Chdir(feature)

	err := runHoltExpectingFailure(t, "rebase")

	if !strings.Contains(err.Error(), "back where it was") {
		t.Errorf("error %q does not say what became of the branch", err)
	}
	if status := testutil.Git(t, feature, "status", "--porcelain"); status != "" {
		t.Errorf("the worktree is left holding %q, want the conflict undone", status)
	}
}

func TestRebaseConflictNoAbort(t *testing.T) {
	clone, origin := testutil.NewClonedRepo(t)
	feature := testutil.AddWorktree(t, clone, "feature")
	testutil.CommitTo(t, feature, "shared.txt", "the branch's version\n")
	testutil.CommitTo(t, origin, "shared.txt", "the default branch's version\n")
	t.Chdir(feature)

	err := runHoltExpectingFailure(t, "rebase", "--no-abort")

	if !strings.Contains(err.Error(), "git rebase --continue") {
		t.Errorf("error %q does not say how to finish the rebase", err)
	}
	if status := testutil.Git(t, feature, "status", "--porcelain"); !strings.Contains(status, "shared.txt") {
		t.Errorf("got %q, want the conflict left standing", status)
	}
}
