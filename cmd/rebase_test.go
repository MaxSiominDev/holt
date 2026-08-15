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
