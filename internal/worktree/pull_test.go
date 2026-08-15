package worktree

import (
	"io"
	"strings"
	"testing"

	"github.com/MaxSiominDev/holt/internal/testutil"
)

func TestPullFromOrigin(t *testing.T) {
	clone, origin := testutil.NewClonedRepo(t)
	newest := testutil.CommitTo(t, origin, "upstream.txt", "added after the clone\n")

	if err := Pull(open(t, clone), io.Discard); err != nil {
		t.Fatal(err)
	}

	if head := testutil.Git(t, clone, "rev-parse", "HEAD"); head != newest {
		t.Fatalf("the branch is at %s, want origin's %s", head, newest)
	}
}

func TestPullWithoutUpstream(t *testing.T) {
	clone, origin := testutil.NewClonedRepo(t)
	testutil.Git(t, origin, "switch", "--quiet", "--create", "feature")
	newest := testutil.CommitTo(t, origin, "theirs.txt", "their work\n")
	testutil.Git(t, origin, "switch", "--quiet", "main")
	// No upstream at all, which plain "git pull" refuses.
	testutil.Git(t, clone, "fetch", "--quiet", "origin", "feature")
	testutil.Git(t, clone, "switch", "--quiet", "--create", "feature", "--no-track", "origin/feature~1")

	if err := Pull(open(t, clone), io.Discard); err != nil {
		t.Fatal(err)
	}

	if head := testutil.Git(t, clone, "rev-parse", "HEAD"); head != newest {
		t.Fatalf("the branch is at %s, want origin's %s", head, newest)
	}
	if _, set, _ := open(t, clone).Config("branch.feature.merge"); set {
		t.Error("holt configured an upstream it was not asked to configure")
	}
}

func TestPullBranchNamedLikeFlag(t *testing.T) {
	clone, origin := testutil.NewClonedRepo(t)
	head := testutil.Git(t, clone, "rev-parse", "HEAD")
	// The porcelain refuses such a name, so it takes update-ref. Passed bare,
	// git would read it as a flag.
	testutil.Git(t, origin, "update-ref", "refs/heads/-f", head)
	testutil.Git(t, clone, "update-ref", "refs/heads/-f", head)
	testutil.Git(t, clone, "symbolic-ref", "HEAD", "refs/heads/-f")

	if err := Pull(open(t, clone), io.Discard); err != nil {
		t.Fatal(err)
	}
}

func TestPullDetachedHead(t *testing.T) {
	clone, _ := testutil.NewClonedRepo(t)
	testutil.Git(t, clone, "checkout", "--quiet", "--detach")

	err := Pull(open(t, clone), io.Discard)

	if err == nil {
		t.Fatal("a detached HEAD was pulled as if it were a branch")
	}
	if !strings.Contains(err.Error(), "no branch") {
		t.Errorf("error %q does not say what is wrong", err)
	}
}
