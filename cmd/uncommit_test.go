package cmd

import (
	"testing"

	"github.com/MaxSiominDev/holt/internal/testutil"
)

func TestUncommitKeepsStdoutClean(t *testing.T) {
	repo := testutil.NewRepo(t)
	before := testutil.Git(t, repo, "rev-parse", "HEAD")
	testutil.CommitTo(t, repo, "later.txt", "written later\n")
	t.Chdir(repo)

	stdout, stderr := runHolt(t, "uncommit")

	if head := testutil.Git(t, repo, "rev-parse", "HEAD"); head != before {
		t.Fatalf("the branch is at %s, want %s", head, before)
	}
	// stdout carries paths for the shell wrapper, so what a human reads goes to stderr.
	if stdout != "" {
		t.Errorf("stdout is %q, want the line on stderr instead", stdout)
	}
	if stderr == "" {
		t.Error("nothing said which commit was taken back")
	}
}

func TestUncommitTakesNoArguments(t *testing.T) {
	repo := testutil.NewRepo(t)
	testutil.CommitTo(t, repo, "later.txt", "written later\n")
	t.Chdir(repo)

	// A number here would read as how many commits to take back, which holt does
	// not do: it takes back the last one and no other.
	_ = runHoltExpectingFailure(t, "uncommit", "2")
}
