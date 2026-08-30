package cmd

import (
	"os"
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

func TestRebaseMergesTheListedFile(t *testing.T) {
	clone, origin := testutil.NewClonedRepo(t)
	testutil.CommitTo(t, origin, "CHANGELOG.md", "# Changelog\n\n- the entry that was there\n")
	testutil.Git(t, clone, "pull", "--quiet", "origin", "main")
	feature := testutil.AddWorktree(t, clone, "feature")
	testutil.CommitTo(t, feature, "CHANGELOG.md", "# Changelog\n\n- the entry that was there\n- what this branch did\n")
	testutil.CommitTo(t, origin, "CHANGELOG.md", "# Changelog\n\n- the entry that was there\n- what landed first\n")
	writeMergeList(t, "CHANGELOG.md\n")
	t.Chdir(feature)

	runHolt(t, "rebase")

	want := "# Changelog\n\n- the entry that was there\n- what this branch did\n- what landed first\n"
	got, err := os.ReadFile(filepath.Join(feature, "CHANGELOG.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Errorf("the merged file is\n%q\nwant\n%q", got, want)
	}
}

func TestRebaseReportsAnUnreadableMergeListLine(t *testing.T) {
	clone, origin := testutil.NewClonedRepo(t)
	feature := testutil.AddWorktree(t, clone, "feature")
	testutil.CommitTo(t, feature, "work.txt", "my work\n")
	testutil.CommitTo(t, origin, "upstream.txt", "someone else's work\n")
	writeMergeList(t, "CHANGELOG.md\nnotes.txt\n")
	t.Chdir(feature)

	_, errOut := runHolt(t, "rebase")

	// Nothing conflicted here, so this is the only chance the user has to hear it.
	if !strings.Contains(errOut, "notes.txt") {
		t.Errorf("the report %q says nothing about the line holt could not read", errOut)
	}
}

func writeMergeList(t *testing.T, content string) {
	t.Helper()
	testutil.WriteFile(t, filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "holt", "merge.list"), content)
}
