package worktree

import (
	"io"
	"strings"
	"testing"

	"github.com/MaxSiominDev/holt/internal/testutil"
)

func TestPushToOrigin(t *testing.T) {
	clone, origin := testutil.NewPushableClone(t)
	testutil.Git(t, clone, "switch", "--quiet", "--create", "feature")
	head := testutil.CommitTo(t, clone, "work.txt", "my work\n")

	if err := Push(open(t, clone), false, io.Discard); err != nil {
		t.Fatal(err)
	}

	if remote := testutil.Git(t, origin, "rev-parse", "feature"); remote != head {
		t.Fatalf("origin is at %s, want %s", remote, head)
	}
}

func TestPushLeavesUpstream(t *testing.T) {
	clone, _ := testutil.NewPushableClone(t)
	testutil.Git(t, clone, "switch", "--quiet", "--create", "feature")
	testutil.CommitTo(t, clone, "work.txt", "my work\n")

	if err := Push(open(t, clone), false, io.Discard); err != nil {
		t.Fatal(err)
	}

	// No -u: holt pushes the branch and leaves what it tracks alone.
	_, set, err := open(t, clone).Config("branch.feature.merge")
	if err != nil {
		t.Fatal(err)
	}
	if set {
		t.Error("holt set an upstream it was told not to set")
	}
}

func TestPushWithoutForce(t *testing.T) {
	clone, origin := testutil.NewPushableClone(t)
	testutil.Git(t, clone, "switch", "--quiet", "--create", "feature")
	sent := testutil.CommitTo(t, clone, "work.txt", "my work\n")
	if err := Push(open(t, clone), false, io.Discard); err != nil {
		t.Fatal(err)
	}
	testutil.Git(t, clone, "commit", "--quiet", "--amend", "-m", "my work, reworded")

	err := Push(open(t, clone), false, io.Discard)

	if err == nil {
		t.Fatal("a rewritten branch was pushed without asking for force")
	}
	if remote := testutil.Git(t, origin, "rev-parse", "feature"); remote != sent {
		t.Fatalf("origin moved to %s, want it left at %s", remote, sent)
	}
}

func TestPushWithForce(t *testing.T) {
	clone, origin := testutil.NewPushableClone(t)
	testutil.Git(t, clone, "switch", "--quiet", "--create", "feature")
	testutil.CommitTo(t, clone, "work.txt", "my work\n")
	if err := Push(open(t, clone), false, io.Discard); err != nil {
		t.Fatal(err)
	}
	testutil.Git(t, clone, "commit", "--quiet", "--amend", "-m", "my work, reworded")
	rewritten := testutil.Git(t, clone, "rev-parse", "HEAD")

	if err := Push(open(t, clone), true, io.Discard); err != nil {
		t.Fatal(err)
	}

	if remote := testutil.Git(t, origin, "rev-parse", "feature"); remote != rewritten {
		t.Fatalf("origin is at %s, want the rewritten %s", remote, rewritten)
	}
}

func TestPushForceRemoteMoved(t *testing.T) {
	clone, origin := testutil.NewPushableClone(t)
	theirs := pushColleagueCommit(t, clone, origin)
	// Rewriting without having fetched, so the lease no longer matches.
	testutil.Git(t, clone, "commit", "--quiet", "--amend", "-m", "my work, reworded")

	err := Push(open(t, clone), true, io.Discard)

	if err == nil {
		t.Fatal("a force push overwrote a commit that had never been seen")
	}
	if remote := testutil.Git(t, origin, "rev-parse", "feature"); remote != theirs {
		t.Fatalf("origin is at %s, want the colleague's commit %s still there", remote, theirs)
	}
}

func TestPushForceUnintegratedFetch(t *testing.T) {
	clone, origin := testutil.NewPushableClone(t)
	theirs := pushColleagueCommit(t, clone, origin)
	testutil.Git(t, clone, "commit", "--quiet", "--amend", "-m", "my work, reworded")
	// A background fetch, from an IDE say, is enough to satisfy the lease alone.
	testutil.Git(t, clone, "fetch", "--quiet", "origin", "feature")

	err := Push(open(t, clone), true, io.Discard)

	if err == nil {
		t.Fatal("an unintegrated fetch was taken as consent to overwrite")
	}
	if remote := testutil.Git(t, origin, "rev-parse", "feature"); remote != theirs {
		t.Fatalf("origin is at %s, want the colleague's commit %s still there", remote, theirs)
	}
}

func TestPushBranchNamedLikeFlag(t *testing.T) {
	clone, origin := testutil.NewPushableClone(t)
	head := testutil.CommitTo(t, clone, "work.txt", "my work\n")
	// The porcelain refuses such a name, so it takes update-ref. Passed bare,
	// "git push origin -f" would read as "git push --force".
	testutil.Git(t, clone, "update-ref", "refs/heads/-f", head)
	testutil.Git(t, clone, "symbolic-ref", "HEAD", "refs/heads/-f")

	if err := Push(open(t, clone), false, io.Discard); err != nil {
		t.Fatal(err)
	}

	if remote := testutil.Git(t, origin, "rev-parse", "refs/heads/-f"); remote != head {
		t.Fatalf("origin has %s under the branch, want %s", remote, head)
	}
}

func TestPushDetachedHead(t *testing.T) {
	clone, _ := testutil.NewPushableClone(t)
	testutil.Git(t, clone, "checkout", "--quiet", "--detach")

	err := Push(open(t, clone), false, io.Discard)

	if err == nil {
		t.Fatal("a detached HEAD was pushed as if it were a branch")
	}
	if !strings.Contains(err.Error(), "no branch") {
		t.Errorf("error %q does not say what is wrong", err)
	}
}

// A second clone adds a commit to the branch; the return is what origin holds.
func pushColleagueCommit(t *testing.T, clone, origin string) string {
	t.Helper()
	testutil.Git(t, clone, "switch", "--quiet", "--create", "feature")
	testutil.CommitTo(t, clone, "work.txt", "my work\n")
	if err := Push(open(t, clone), false, io.Discard); err != nil {
		t.Fatal(err)
	}

	colleague := testutil.CloneOf(t, origin, "colleague")
	testutil.Git(t, colleague, "switch", "--quiet", "--create", "feature", "origin/feature")
	theirs := testutil.CommitTo(t, colleague, "theirs.txt", "someone else's work\n")
	testutil.Git(t, colleague, "push", "--quiet", "origin", "feature")
	return theirs
}

func TestPushForceNarrowFetchRefspec(t *testing.T) {
	clone, origin := testutil.NewPushableClone(t)
	// What a clone made with --single-branch carries: one branch mapped.
	testutil.Git(t, clone, "config", "remote.origin.fetch", "+refs/heads/main:refs/remotes/origin/main")
	testutil.Git(t, clone, "switch", "--quiet", "--create", "feature")
	sent := testutil.CommitTo(t, clone, "work.txt", "my work\n")
	if err := Push(open(t, clone), false, io.Discard); err != nil {
		t.Fatal(err)
	}
	testutil.Git(t, clone, "commit", "--quiet", "--amend", "-m", "my work, reworded")

	err := Push(open(t, clone), true, io.Discard)

	if err == nil {
		t.Fatal("a lease was taken against a ref the refspec does not map")
	}
	// git only says "stale info", and no fetch gets the user out of it.
	if !strings.Contains(err.Error(), "remote.origin.fetch") {
		t.Errorf("error %q does not say how to get out of it", err)
	}
	if remote := testutil.Git(t, origin, "rev-parse", "feature"); remote != sent {
		t.Fatalf("origin moved to %s, want it left at %s", remote, sent)
	}
}

func TestPushForceAfterRefspecWidened(t *testing.T) {
	clone, origin := testutil.NewPushableClone(t)
	testutil.Git(t, clone, "config", "remote.origin.fetch", "+refs/heads/main:refs/remotes/origin/main")
	testutil.Git(t, clone, "switch", "--quiet", "--create", "feature")
	testutil.CommitTo(t, clone, "work.txt", "my work\n")
	if err := Push(open(t, clone), false, io.Discard); err != nil {
		t.Fatal(err)
	}
	testutil.Git(t, clone, "commit", "--quiet", "--amend", "-m", "my work, reworded")
	rewritten := testutil.Git(t, clone, "rev-parse", "HEAD")
	// The way out the refusal above names, carried out.
	testutil.Git(t, clone, "config", "--add", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*")
	testutil.Git(t, clone, "fetch", "--quiet", "origin")

	if err := Push(open(t, clone), true, io.Discard); err != nil {
		t.Fatalf("the advice holt gives does not work: %v", err)
	}

	if remote := testutil.Git(t, origin, "rev-parse", "feature"); remote != rewritten {
		t.Fatalf("origin is at %s, want the rewritten %s", remote, rewritten)
	}
}

func TestRefspecCovers(t *testing.T) {
	tests := []struct {
		name    string
		refspec string
		ref     string
		want    bool
	}{
		{name: "the wildcard a plain clone gets", refspec: "+refs/heads/*:refs/remotes/origin/*", ref: "refs/heads/feature", want: true},
		{name: "the branch named outright", refspec: "+refs/heads/feature:refs/remotes/origin/feature", ref: "refs/heads/feature", want: true},
		{name: "another branch named outright", refspec: "+refs/heads/main:refs/remotes/origin/main", ref: "refs/heads/feature"},
		{name: "a narrower wildcard", refspec: "+refs/heads/release/*:refs/remotes/origin/release/*", ref: "refs/heads/release/1.0", want: true},
		{name: "that wildcard missing the branch", refspec: "+refs/heads/release/*:refs/remotes/origin/release/*", ref: "refs/heads/feature"},
		{name: "no force marker", refspec: "refs/heads/*:refs/remotes/origin/*", ref: "refs/heads/feature", want: true},
		{name: "no destination", refspec: "+refs/heads/*", ref: "refs/heads/feature"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := refspecCovers(test.refspec, test.ref); got != test.want {
				t.Fatalf("got %v, want %v", got, test.want)
			}
		})
	}
}
