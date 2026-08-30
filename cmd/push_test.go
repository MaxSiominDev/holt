package cmd

import (
	"strings"
	"testing"

	"github.com/MaxSiominDev/holt/internal/testutil"
)

func TestPushKeepsStdoutClean(t *testing.T) {
	clone, origin := testutil.NewPushableClone(t)
	testutil.Git(t, clone, "switch", "--quiet", "--create", "feature")
	head := testutil.CommitTo(t, clone, "work.txt", "my work\n")
	t.Chdir(clone)

	stdout, stderr := runHolt(t, "push")

	if remote := testutil.Git(t, origin, "rev-parse", "feature"); remote != head {
		t.Fatalf("origin is at %s, want %s", remote, head)
	}
	// stdout carries paths for the shell wrapper, so git's progress goes to stderr.
	if stdout != "" {
		t.Errorf("stdout is %q, want git's progress on stderr instead", stdout)
	}
	if stderr == "" {
		t.Error("git's progress was swallowed")
	}
}

func TestPushDefaultsToNoForce(t *testing.T) {
	clone, _ := testutil.NewPushableClone(t)
	testutil.Git(t, clone, "switch", "--quiet", "--create", "feature")
	testutil.CommitTo(t, clone, "work.txt", "my work\n")
	t.Chdir(clone)
	runHolt(t, "push")
	testutil.Git(t, clone, "commit", "--quiet", "--amend", "-m", "reworded")

	_ = runHoltExpectingFailure(t, "push")
}

func TestPushForceShorthand(t *testing.T) {
	clone, origin := testutil.NewPushableClone(t)
	testutil.Git(t, clone, "switch", "--quiet", "--create", "feature")
	testutil.CommitTo(t, clone, "work.txt", "my work\n")
	t.Chdir(clone)
	runHolt(t, "push")
	testutil.Git(t, clone, "commit", "--quiet", "--amend", "-m", "reworded")
	rewritten := testutil.Git(t, clone, "rev-parse", "HEAD")

	// -f, not --force: the shorthand is what gets typed.
	runHolt(t, "push", "-f")

	if remote := testutil.Git(t, origin, "rev-parse", "feature"); remote != rewritten {
		t.Fatalf("origin is at %s, want the rewritten %s", remote, rewritten)
	}
}

func TestPushTakesNoArguments(t *testing.T) {
	clone, _ := testutil.NewPushableClone(t)
	t.Chdir(clone)

	err := runHoltExpectingFailure(t, "push", "origin", "feature")

	if !strings.Contains(err.Error(), "unknown command") {
		t.Errorf("error %q does not reject the extra arguments", err)
	}
}
