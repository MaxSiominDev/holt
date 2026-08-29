package worktree

import (
	"io"
	"path/filepath"
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
	// The porcelain refuses such a name, so it takes update-ref; passed bare,
	// "git push origin -f" reads as "git push --force".
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
	// What --single-branch, and so --depth, carries: one branch mapped.
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
	// git only says "stale info", and no fetch helps while the refspec is this narrow.
	if !strings.Contains(err.Error(), "remote.origin.fetch") {
		t.Errorf("error %q does not say how to get out of it", err)
	}
	// The refspec is narrow, not hostile, and blaming an exclusion would send the
	// user looking for a line that is not in the config.
	if !strings.Contains(err.Error(), "does not cover") {
		t.Errorf("error %q does not blame the coverage the refspec actually lacks", err)
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

	// Somebody else builds on the branch, and the narrow refspec keeps their commit
	// out, so the branch here has never held it.
	colleague := testutil.CloneOf(t, origin, "colleague")
	testutil.Git(t, colleague, "switch", "--quiet", "feature")
	testutil.CommitTo(t, colleague, "theirs.txt", "their work\n")
	testutil.Git(t, colleague, "push", "--quiet", "origin", "feature")

	// The widening holt names gives the lease a ref to compare against, and git then
	// refuses over the colleague's commit in its own words, which is why the advice
	// stops at the fetch.
	testutil.Git(t, clone, "config", "--add", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*")
	testutil.Git(t, clone, "fetch", "--quiet", "origin")

	err := Push(open(t, clone), true, io.Discard)

	if err == nil {
		t.Fatal("a force push went through over a commit this clone never held")
	}
	if strings.Contains(err.Error(), "fetch refspec") {
		t.Fatalf("holt still refuses over the refspec after it was widened: %v", err)
	}
}

func TestPushForceAfterRefspecWidenedWithoutOriginCopy(t *testing.T) {
	clone, origin := testutil.NewPushableClone(t)
	testutil.Git(t, clone, "config", "remote.origin.fetch", "+refs/heads/main:refs/remotes/origin/main")
	testutil.Git(t, clone, "switch", "--quiet", "--create", "feature")
	sent := testutil.CommitTo(t, clone, "work.txt", "my work\n")

	err := Push(open(t, clone), true, io.Discard)
	if err == nil {
		t.Fatal("a lease was taken against a ref no refspec maps")
	}

	// Origin has no copy, so widening has to be enough on its own: a rebase onto
	// refs/remotes/origin/feature would die on an invalid upstream.
	testutil.Git(t, clone, "config", "--add", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*")
	testutil.Git(t, clone, "fetch", "--quiet", "origin")

	if err := Push(open(t, clone), true, io.Discard); err != nil {
		t.Fatalf("the way out holt names does not work without a copy on origin: %v", err)
	}
	if remote := testutil.Git(t, origin, "rev-parse", "feature"); remote != sent {
		t.Fatalf("origin is at %s, want %s", remote, sent)
	}
}

func TestTrackedAs(t *testing.T) {
	tests := []struct {
		name    string
		refspec string
		branch  string
		want    string
	}{
		{name: "the wildcard a plain clone gets", refspec: "+refs/heads/*:refs/remotes/origin/*", branch: "feature", want: "refs/remotes/origin/feature"},
		{name: "the branch named outright", refspec: "+refs/heads/feature:refs/remotes/origin/feature", branch: "feature", want: "refs/remotes/origin/feature"},
		{name: "a narrower wildcard", refspec: "+refs/heads/release/*:refs/remotes/origin/release/*", branch: "release/1.0", want: "refs/remotes/origin/release/1.0"},
		{name: "no force marker", refspec: "refs/heads/*:refs/remotes/origin/*", branch: "feature", want: "refs/remotes/origin/feature"},
		// git takes a single star anywhere in a side, not only at the end.
		{name: "a star before a suffix", refspec: "+refs/heads/*-x:refs/remotes/origin/*-x", branch: "feat-x", want: "refs/remotes/origin/feat-x"},
		{name: "a star standing for nothing at all", refspec: "+refs/heads/*-x:refs/remotes/origin/*-x", branch: "-x", want: "refs/remotes/origin/-x"},
		// The lease follows wherever the line writes, not always refs/remotes/origin.
		{name: "a destination of its own", refspec: "+refs/heads/*:refs/remotes/mirror/*", branch: "feature", want: "refs/remotes/mirror/feature"},
		// Lines that cover the branch and write nothing worth leasing against.
		{name: "an empty destination", refspec: "+refs/heads/feature:", branch: "feature"},
		{name: "a star on the source side only", refspec: "+refs/heads/*:refs/remotes/origin/pinned", branch: "feature"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := trackedAs(test.refspec, test.branch); got != test.want {
				t.Fatalf("got %q, want %q", got, test.want)
			}
		})
	}
}

func TestFirstCovering(t *testing.T) {
	tests := []struct {
		name     string
		refspecs []string
		branch   string
		want     string
	}{
		{name: "the wildcard a plain clone gets", refspecs: []string{"+refs/heads/*:refs/remotes/origin/*"}, branch: "feature", want: "+refs/heads/*:refs/remotes/origin/*"},
		{name: "another branch named outright", refspecs: []string{"+refs/heads/main:refs/remotes/origin/main"}, branch: "feature"},
		// A starless line covers that ref alone; read as a prefix it would lease
		// another branch's tracking ref, which is somebody else's work.
		{name: "a branch the named one is a prefix of", refspecs: []string{"+refs/heads/main:refs/remotes/origin/main"}, branch: "main-fix"},
		{name: "a wildcard missing the branch", refspecs: []string{"+refs/heads/release/*:refs/remotes/origin/release/*"}, branch: "feature"},
		{name: "the star's two sides overlapping", refspecs: []string{"+refs/heads/ab*ba:refs/remotes/origin/ab*ba"}, branch: "aba"},
		// No destination side at all is not a match for git either.
		{name: "no destination side", refspecs: []string{"+refs/heads/*"}, branch: "feature"},
		{name: "one of those before a wildcard", refspecs: []string{"refs/heads/feature", "+refs/heads/*:refs/remotes/origin/*"}, branch: "feature", want: "+refs/heads/*:refs/remotes/origin/*"},
		// git stops at the first covering line, so a wildcard behind one that
		// writes no ref does not rescue it.
		{name: "the first of two covering lines", refspecs: []string{"+refs/heads/*:refs/remotes/mirror/*", "+refs/heads/*:refs/remotes/origin/*"}, branch: "feature", want: "+refs/heads/*:refs/remotes/mirror/*"},
		{name: "an empty destination before a wildcard", refspecs: []string{"+refs/heads/feature:", "+refs/heads/*:refs/remotes/origin/*"}, branch: "feature", want: "+refs/heads/feature:"},
		// An exclusion has no destination, so it never covers; excludedBy reads those.
		{name: "an exclusion before a wildcard", refspecs: []string{"^refs/heads/feature", "+refs/heads/*:refs/remotes/origin/*"}, branch: "feature", want: "+refs/heads/*:refs/remotes/origin/*"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, covered := firstCovering(test.refspecs, test.branch)
			if covered != (test.want != "") {
				t.Fatalf("covered is %v, want %v", covered, test.want != "")
			}
			if got != test.want {
				t.Fatalf("got %q, want %q", got, test.want)
			}
		})
	}
}

func TestPushForceWithExcludedBranchStillCovered(t *testing.T) {
	clone, origin := testutil.NewPushableClone(t)
	testutil.Git(t, clone, "switch", "--quiet", "--create", "secret")
	sent := testutil.CommitTo(t, clone, "work.txt", "my work\n")
	if err := Push(open(t, clone), false, io.Discard); err != nil {
		t.Fatal(err)
	}
	testutil.Git(t, clone, "commit", "--quiet", "--amend", "-m", "my work, reworded")
	rewritten := testutil.Git(t, clone, "rev-parse", "HEAD")
	// A line telling git not to fetch this branch has no destination side, so holt
	// passes over it and takes the wildcard beside it.
	testutil.Git(t, clone, "config", "--add", "remote.origin.fetch", "^refs/heads/secret")

	if err := Push(open(t, clone), true, io.Discard); err != nil {
		t.Fatalf("holt turned away a push git performs: %v", err)
	}

	if remote := testutil.Git(t, origin, "rev-parse", "secret"); remote != rewritten {
		t.Fatalf("origin is at %s, want the rewritten %s (was %s)", remote, rewritten, sent)
	}
}

func TestPushForceExcludedWithoutRemoteCopy(t *testing.T) {
	clone, _ := testutil.NewPushableClone(t)
	testutil.Git(t, clone, "switch", "--quiet", "--create", "secret")
	testutil.CommitTo(t, clone, "work.txt", "my work\n")
	// Origin holds the branch, so the lease has a real value; only this clone's
	// copy of it is missing.
	if err := Push(open(t, clone), false, io.Discard); err != nil {
		t.Fatal(err)
	}
	// The exclusion sits beside a plain clone's wildcard, so the branch stays covered
	// while no plain fetch writes the ref again.
	testutil.Git(t, clone, "config", "--add", "remote.origin.fetch", "^refs/heads/secret")
	testutil.Git(t, clone, "update-ref", "-d", "refs/remotes/origin/secret")
	testutil.Git(t, clone, "commit", "--quiet", "--amend", "-m", "my work, reworded")

	err := Push(open(t, clone), true, io.Discard)

	if err == nil {
		t.Fatal("a lease was taken against a ref that is not here")
	}
	if !strings.Contains(err.Error(), "^refs/heads/secret") {
		t.Errorf("error %q does not name the line that excludes the branch", err)
	}
	// Widening, the neighbouring remedy, does nothing: the wildcard is already here
	// and the exclusion outranks it.
	if strings.Contains(err.Error(), "does not cover") {
		t.Errorf("error %q blames coverage for a branch the refspec covers", err)
	}
	// A fetch naming the ref goes around the exclusion, so no config edit is needed first.
	if want := `git fetch origin '+refs/heads/secret:refs/remotes/origin/secret'`; !strings.Contains(err.Error(), want) {
		t.Errorf("error %q does not name %q, the fetch that gets the ref back", err, want)
	}
}

func TestPushForceExcludedWithStaleRemoteCopy(t *testing.T) {
	clone, origin := testutil.NewPushableClone(t)
	testutil.Git(t, clone, "switch", "--quiet", "--create", "secret")
	testutil.CommitTo(t, clone, "work.txt", "my work\n")
	if err := Push(open(t, clone), false, io.Discard); err != nil {
		t.Fatal(err)
	}
	testutil.Git(t, clone, "config", "--add", "remote.origin.fetch", "^refs/heads/secret")
	// Somebody else moves origin: the ref is still here, so a presence test passes
	// this by and no plain fetch corrects it.
	colleague := testutil.CloneOf(t, origin, "colleague")
	testutil.Git(t, colleague, "switch", "--quiet", "secret")
	testutil.CommitTo(t, colleague, "theirs.txt", "their work\n")
	testutil.Git(t, colleague, "push", "--quiet", "origin", "secret")
	testutil.Git(t, clone, "commit", "--quiet", "--amend", "-m", "my work, reworded")

	err := Push(open(t, clone), true, io.Discard)

	if err == nil {
		t.Fatal("a lease was taken against a ref that no longer agrees with origin")
	}
	if !strings.Contains(err.Error(), "^refs/heads/secret") {
		t.Errorf("error %q does not name the line that excludes the branch", err)
	}
	// Spelled as a refspec, since a branch called -f reads as a flag in the command
	// the message names, and quoted, since a refname may hold shell characters.
	if want := `git fetch origin '+refs/heads/secret:refs/remotes/origin/secret'`; !strings.Contains(err.Error(), want) {
		t.Errorf("error %q does not name %q", err, want)
	}
}

func TestPushForceRefspecWritingNoRef(t *testing.T) {
	clone, _ := testutil.NewPushableClone(t)
	testutil.Git(t, clone, "switch", "--quiet", "--create", "feature")
	testutil.CommitTo(t, clone, "work.txt", "my work\n")
	if err := Push(open(t, clone), false, io.Discard); err != nil {
		t.Fatal(err)
	}
	// This refspec fetches into FETCH_HEAD alone: a current ref of that name is
	// still lying about, but the covering line names no destination to read it from.
	testutil.Git(t, clone, "config", "remote.origin.fetch", "+refs/heads/feature:")
	testutil.Git(t, clone, "commit", "--quiet", "--amend", "-m", "my work, reworded")

	err := Push(open(t, clone), true, io.Discard)

	if err == nil {
		t.Fatal("a lease was taken against a ref no refspec writes")
	}
	if !strings.Contains(err.Error(), "maps feature to no ref of its own") {
		t.Errorf("error %q does not say the line writes no ref", err)
	}
	// Widening cures a refspec that misses the branch and does nothing here, git
	// stopping at the covering line and never reaching another.
	if !strings.Contains(err.Error(), "adding another will not help") {
		t.Errorf("error %q lets the user think another line would help", err)
	}
}

func TestPushForceRefspecWritingNoRefBeforeAWildcard(t *testing.T) {
	clone, _ := testutil.NewPushableClone(t)
	testutil.Git(t, clone, "switch", "--quiet", "--create", "feature")
	testutil.CommitTo(t, clone, "work.txt", "my work\n")
	if err := Push(open(t, clone), false, io.Discard); err != nil {
		t.Fatal(err)
	}
	// The wildcard keeps refs/remotes/origin/feature current, but git stops at the
	// line above and the lease still has nothing.
	testutil.Git(t, clone, "config", "remote.origin.fetch", "+refs/heads/feature:")
	testutil.Git(t, clone, "config", "--add", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*")
	testutil.Git(t, clone, "fetch", "--quiet", "origin")
	testutil.Git(t, clone, "commit", "--quiet", "--amend", "-m", "my work, reworded")
	if testutil.Git(t, clone, "rev-parse", "--verify", "--quiet", "refs/remotes/origin/feature") == "" {
		t.Fatal("the fixture no longer reproduces the case, the wildcard wrote no ref")
	}

	err := Push(open(t, clone), true, io.Discard)

	if err == nil {
		t.Fatal("a ref a later line writes was taken for the one git leases against")
	}
	if !strings.Contains(err.Error(), "+refs/heads/feature:") {
		t.Errorf("error %q does not name the line git stops at", err)
	}
}

func TestPushForceExcludedNothingToPush(t *testing.T) {
	clone, _ := testutil.NewPushableClone(t)
	testutil.Git(t, clone, "switch", "--quiet", "--create", "secret")
	testutil.CommitTo(t, clone, "work.txt", "my work\n")
	if err := Push(open(t, clone), false, io.Discard); err != nil {
		t.Fatal(err)
	}
	// The branch is where origin has it, so git reports up to date and never takes
	// the lease; refusing would turn away a push git performs.
	testutil.Git(t, clone, "config", "--add", "remote.origin.fetch", "^refs/heads/secret")
	testutil.Git(t, clone, "update-ref", "-d", "refs/remotes/origin/secret")

	if err := Push(open(t, clone), true, io.Discard); err != nil {
		t.Fatalf("holt turned away a push git performs: %v", err)
	}
}

func TestPushForceExcludedOriginUnreachable(t *testing.T) {
	clone, _ := testutil.NewPushableClone(t)
	testutil.Git(t, clone, "switch", "--quiet", "--create", "secret")
	testutil.CommitTo(t, clone, "work.txt", "my work\n")
	if err := Push(open(t, clone), false, io.Discard); err != nil {
		t.Fatal(err)
	}
	testutil.Git(t, clone, "config", "--add", "remote.origin.fetch", "^refs/heads/secret")
	testutil.CommitTo(t, clone, "more.txt", "more work\n")
	// Origin cannot be asked, and silence read as an answer would have holt tell the
	// user to throw away the only value the lease measures against.
	testutil.Git(t, clone, "remote", "set-url", "origin", filepath.Join(clone, "..", "not-a-repository"))

	err := Push(open(t, clone), true, io.Discard)

	if err == nil {
		t.Fatal("a push to an origin that is not there was reported as done")
	}
	if strings.Contains(err.Error(), "no longer has that branch") {
		t.Errorf("error %q accuses origin on the strength of a question it never answered", err)
	}
	if strings.Contains(err.Error(), "update-ref -d") {
		t.Errorf("error %q tells the user to delete the ref the lease needs", err)
	}
}

func TestPushForceExcludedTrackingRefCheckedOutElsewhere(t *testing.T) {
	clone, _ := testutil.NewPushableClone(t)
	testutil.Git(t, clone, "switch", "--quiet", "--create", "secret")
	testutil.CommitTo(t, clone, "work.txt", "my work\n")
	if err := Push(open(t, clone), false, io.Discard); err != nil {
		t.Fatal(err)
	}
	// A refspec writing over local branches, with this branch's own open in a
	// worktree: git will not fetch into it, so the ordinary advice cannot be followed.
	testutil.Git(t, clone, "config", "remote.origin.fetch", "+refs/heads/*:refs/heads/x-*")
	testutil.Git(t, clone, "config", "--add", "remote.origin.fetch", "^refs/heads/secret")
	testutil.Git(t, clone, "branch", "x-secret", "main")
	held := filepath.Join(clone, "..", "sibling")
	testutil.Git(t, clone, "worktree", "add", "--quiet", held, "x-secret")
	testutil.CommitTo(t, clone, "more.txt", "more work\n")

	err := Push(open(t, clone), true, io.Discard)

	if err == nil {
		t.Fatal("a lease was taken against a ref that does not agree with origin")
	}
	if !strings.Contains(err.Error(), "checked out at "+held) {
		t.Errorf("error %q does not name the worktree standing in the way", err)
	}
	// Dropping the exclusion only lets the fetch reach the same ref and be refused
	// again; the checkout has to give way first.
	if !strings.Contains(err.Error(), "Check something else out there") {
		t.Errorf("error %q does not say to clear the checkout first", err)
	}
	if !strings.Contains(err.Error(), `git fetch origin '+refs/heads/secret:refs/heads/x-secret'`) {
		t.Errorf("error %q does not name the fetch that works once it is cleared", err)
	}
}

func TestPushForceExcludedBranchGoneFromOrigin(t *testing.T) {
	clone, origin := testutil.NewPushableClone(t)
	testutil.Git(t, clone, "switch", "--quiet", "--create", "secret")
	testutil.CommitTo(t, clone, "work.txt", "my work\n")
	if err := Push(open(t, clone), false, io.Discard); err != nil {
		t.Fatal(err)
	}
	testutil.Git(t, clone, "config", "--add", "remote.origin.fetch", "^refs/heads/secret")
	// Dropped on origin behind this clone's back, leaving the ref nothing to agree
	// with, and an excluded ref survives --prune too.
	testutil.Git(t, origin, "branch", "--delete", "--force", "secret")
	testutil.CommitTo(t, clone, "more.txt", "more work\n")

	err := Push(open(t, clone), true, io.Discard)

	if err == nil {
		t.Fatal("a lease was taken against a ref origin has nothing to match")
	}
	if want := `git update-ref -d 'refs/remotes/origin/secret'`; !strings.Contains(err.Error(), want) {
		t.Errorf("error %q does not name %q, the only way to clear the ref", err, want)
	}
	// It may say a fetch will not help; it must not send the user to run one.
	if strings.Contains(err.Error(), "git fetch origin") {
		t.Errorf("error %q sends the user to fetch a branch origin does not have", err)
	}
}

func TestPushForceExcludedBranchTrackedOutsideOrigin(t *testing.T) {
	clone, origin := testutil.NewPushableClone(t)
	// A refspec writing outside refs/remotes/origin: the lease follows it, and
	// looking under refs/remotes/origin would refuse a push git performs.
	testutil.Git(t, clone, "config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/mirror/*")
	testutil.Git(t, clone, "switch", "--quiet", "--create", "secret")
	testutil.CommitTo(t, clone, "work.txt", "my work\n")
	if err := Push(open(t, clone), false, io.Discard); err != nil {
		t.Fatal(err)
	}
	testutil.Git(t, clone, "fetch", "--quiet", "origin")
	testutil.Git(t, clone, "config", "--add", "remote.origin.fetch", "^refs/heads/secret")
	testutil.Git(t, clone, "commit", "--quiet", "--amend", "-m", "my work, reworded")
	rewritten := testutil.Git(t, clone, "rev-parse", "HEAD")

	if err := Push(open(t, clone), true, io.Discard); err != nil {
		t.Fatalf("holt turned away a push git performs: %v", err)
	}

	if remote := testutil.Git(t, origin, "rev-parse", "secret"); remote != rewritten {
		t.Fatalf("origin is at %s, want the rewritten %s", remote, rewritten)
	}
}

func TestPushForceExcludedBranchOriginDoesNotHave(t *testing.T) {
	clone, origin := testutil.NewPushableClone(t)
	// Excluded, and on neither side: the lease is empty, git creates the branch,
	// and holt has nothing to refuse over.
	testutil.Git(t, clone, "config", "--add", "remote.origin.fetch", "^refs/heads/fresh")
	testutil.Git(t, clone, "switch", "--quiet", "--create", "fresh")
	sent := testutil.CommitTo(t, clone, "work.txt", "my work\n")

	if err := Push(open(t, clone), true, io.Discard); err != nil {
		t.Fatalf("holt turned away a push git performs: %v", err)
	}

	if remote := testutil.Git(t, origin, "rev-parse", "fresh"); remote != sent {
		t.Fatalf("origin is at %s, want %s", remote, sent)
	}
}

func TestPushForceExcludedAndUncovered(t *testing.T) {
	clone, _ := testutil.NewPushableClone(t)
	testutil.Git(t, clone, "switch", "--quiet", "--create", "secret")
	testutil.CommitTo(t, clone, "work.txt", "my work\n")
	// Pushed while the refspec was whole, as "holt new" leaves a branch, so what is
	// left to report is the refspec, not a missing copy.
	if err := Push(open(t, clone), false, io.Discard); err != nil {
		t.Fatal(err)
	}
	testutil.Git(t, clone, "config", "remote.origin.fetch", "+refs/heads/main:refs/remotes/origin/main")
	testutil.Git(t, clone, "config", "--add", "remote.origin.fetch", "^refs/heads/secret")
	testutil.Git(t, clone, "commit", "--quiet", "--amend", "-m", "my work, reworded")

	err := Push(open(t, clone), true, io.Discard)

	if err == nil {
		t.Fatal("a lease was taken against a ref no refspec maps")
	}
	// Widening leaves an excluded branch as uncovered as before, dropping the user
	// back to git's bare "stale info", which this message exists to replace.
	if !strings.Contains(err.Error(), "^refs/heads/secret") {
		t.Errorf("error %q does not name the line that excludes the branch", err)
	}
	if !strings.Contains(err.Error(), "does not cover") {
		t.Errorf("error %q does not say the refspec stopped mapping the branch", err)
	}
	if !strings.Contains(err.Error(), "remote.origin.fetch") {
		t.Errorf("error %q does not say what to widen", err)
	}
}
