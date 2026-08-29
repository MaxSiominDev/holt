package cmd

import (
	"testing"

	"github.com/MaxSiominDev/holt/internal/testutil"
)

func TestPullKeepsStdoutClean(t *testing.T) {
	clone, origin := testutil.NewClonedRepo(t)
	testutil.Git(t, clone, "switch", "--quiet", "--create", "feature")
	testutil.Git(t, clone, "push", "--quiet", "origin", "feature")
	wanted := testutil.CommitTo(t, origin, "theirs.txt", "their work\n")
	testutil.Git(t, origin, "update-ref", "refs/heads/feature", wanted)
	t.Chdir(clone)

	stdout, stderr := runHolt(t, "pull")

	if got := testutil.Git(t, clone, "rev-parse", "HEAD"); got != wanted {
		t.Fatalf("the branch is at %s, want origin's %s", got, wanted)
	}
	// stdout carries paths for the shell wrapper, so git's progress goes to stderr.
	if stdout != "" {
		t.Errorf("stdout is %q, want git's progress on stderr instead", stdout)
	}
	if stderr == "" {
		t.Error("git's progress was swallowed")
	}
}

func TestPullTakesNoArguments(t *testing.T) {
	clone, _ := testutil.NewClonedRepo(t)
	t.Chdir(clone)

	// A branch name here would read as one to pull, which holt does not do: it
	// pulls the branch this worktree is on and no other.
	runHoltExpectingFailure(t, "pull", "feature")
}
