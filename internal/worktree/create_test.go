package worktree

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MaxSiominDev/holt/internal/testutil"
)

func TestWorktreePath(t *testing.T) {
	tests := []struct {
		name   string
		main   string
		branch string
		want   string
	}{
		{
			name: "beside the main checkout",
			main: filepath.Join("/code", "project"), branch: "feature",
			want: filepath.Join("/code", "project-worktrees", "feature"),
		},
		{
			name: "a branch name with a slash nests",
			main: filepath.Join("/code", "project"), branch: "PROJ-1/fix",
			want: filepath.Join("/code", "project-worktrees", "PROJ-1", "fix"),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := worktreePath(test.main, test.branch); got != test.want {
				t.Fatalf("got %q, want %q", got, test.want)
			}
		})
	}
}

func TestCreateFetchesDefault(t *testing.T) {
	clone, origin := testutil.NewClonedRepo(t)
	// A commit the clone has never seen.
	upstream := testutil.CommitTo(t, origin, "upstream.txt", "added after the clone\n")

	path, err := Create(open(t, clone), "feature", CreateOptions{}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}

	if want := filepath.Join(filepath.Dir(clone), filepath.Base(clone)+"-worktrees", "feature"); path != want {
		t.Fatalf("got %q, want %q", path, want)
	}
	// Branching off a stale origin/main would miss it.
	if head := testutil.Git(t, path, "rev-parse", "HEAD"); head != upstream {
		t.Fatalf("the new branch is at %s, want the freshly fetched %s", head, upstream)
	}
}

func TestCreateStaleOriginHead(t *testing.T) {
	clone, origin := testutil.NewClonedRepo(t)
	current := testutil.Git(t, clone, "rev-parse", "origin/main")
	// origin/HEAD still names the default from clone time, and still resolves.
	testutil.AddRemoteBranch(t, clone, "initial-pr")
	testutil.Git(t, clone, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/initial-pr")
	testutil.Git(t, origin, "branch", "initial-pr")
	testutil.CommitTo(t, origin, "moved-on.txt", "main has moved on\n")

	path, err := Create(open(t, clone), "feature", CreateOptions{}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}

	base := testutil.Git(t, path, "rev-parse", "HEAD")
	if base == current {
		t.Fatal("the branch was built on the stale origin/HEAD")
	}
	if base != testutil.Git(t, origin, "rev-parse", "main") {
		t.Fatalf("the branch is at %s, want the tip of origin's real default branch", base)
	}
}

func TestCreateLeavesMainCheckout(t *testing.T) {
	clone, _ := testutil.NewClonedRepo(t)
	testutil.Git(t, clone, "switch", "--quiet", "--create", "work-in-progress")
	testutil.WriteFile(t, filepath.Join(clone, "README.md"), "half-finished edit\n")

	if _, err := Create(open(t, clone), "feature", CreateOptions{}, io.Discard); err != nil {
		t.Fatal(err)
	}

	// The shell script holt replaced checked the default branch out here first.
	if branch := testutil.Git(t, clone, "rev-parse", "--abbrev-ref", "HEAD"); branch != "work-in-progress" {
		t.Errorf("the main checkout moved to %q", branch)
	}
	content, err := os.ReadFile(filepath.Join(clone, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "half-finished edit\n" {
		t.Errorf("the uncommitted edit was changed, the file holds %q", content)
	}
}

func TestCreateExistingBranch(t *testing.T) {
	clone, _ := testutil.NewClonedRepo(t)
	testutil.Git(t, clone, "switch", "--quiet", "--create", "existing")
	kept := testutil.CommitTo(t, clone, "work.txt", "work done earlier\n")
	testutil.Git(t, clone, "switch", "--quiet", "main")

	path, err := Create(open(t, clone), "existing", CreateOptions{}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}

	if head := testutil.Git(t, path, "rev-parse", "HEAD"); head != kept {
		t.Fatalf("the worktree is at %s, want the branch's own commit %s", head, kept)
	}
}

func TestCreateBlockedParentLeavesNoBranch(t *testing.T) {
	clone, _ := testutil.NewClonedRepo(t)
	// A file where the worktrees directory belongs, so the occupied-path guard
	// does not fire and git gets as far as the branch before failing.
	testutil.WriteFile(t, filepath.Join(filepath.Dir(clone), filepath.Base(clone)+"-worktrees"), "in the way\n")

	_, err := Create(open(t, clone), "feature", CreateOptions{}, io.Discard)

	if err == nil {
		t.Fatal("a worktree was created under a file")
	}
	if localBranchExists(open(t, clone), "feature") {
		t.Error("a branch was left behind by the failed attempt")
	}
}

func TestCreateSingleBranchClone(t *testing.T) {
	_, origin := testutil.NewClonedRepo(t)
	testutil.Git(t, origin, "branch", "release")
	root := filepath.Dir(origin)
	single := filepath.Join(root, "single")
	// Covers only "release", so fetching main by name leaves no origin/main.
	testutil.Git(t, root, "clone", "--quiet", "--single-branch", "--branch", "release", origin, single)

	path, err := Create(open(t, single), "feature", CreateOptions{}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}

	want := testutil.Git(t, origin, "rev-parse", "main")
	if head := testutil.Git(t, path, "rev-parse", "HEAD"); head != want {
		t.Fatalf("the branch is at %s, want the tip of origin's default branch %s", head, want)
	}
}

func TestCreateTracksWithoutAutoSetup(t *testing.T) {
	clone, origin := testutil.NewClonedRepo(t)
	// autoSetupMerge would set the upstream itself and hide whether holt asked.
	testutil.Git(t, clone, "config", "branch.autoSetupMerge", "false")
	testutil.Git(t, origin, "switch", "--quiet", "--create", "colleague")
	testutil.CommitTo(t, origin, "theirs.txt", "someone else's work\n")
	testutil.Git(t, origin, "switch", "--quiet", "main")
	testutil.Git(t, clone, "fetch", "--quiet", "origin", "colleague")

	path, err := Create(open(t, clone), "colleague", CreateOptions{}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}

	upstream := testutil.Git(t, path, "rev-parse", "--abbrev-ref", "colleague@{upstream}")
	if upstream != "origin/colleague" {
		t.Fatalf("the branch tracks %q, want origin/colleague", upstream)
	}
}

func TestCreateTracksRemoteBranch(t *testing.T) {
	clone, origin := testutil.NewClonedRepo(t)
	// A colleague's branch, known here but with no local branch of its own.
	testutil.Git(t, origin, "switch", "--quiet", "--create", "colleague")
	theirWork := testutil.CommitTo(t, origin, "theirs.txt", "someone else's work\n")
	testutil.Git(t, origin, "switch", "--quiet", "main")
	testutil.Git(t, clone, "fetch", "--quiet", "origin", "colleague")

	path, err := Create(open(t, clone), "colleague", CreateOptions{}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}

	if head := testutil.Git(t, path, "rev-parse", "HEAD"); head != theirWork {
		t.Fatalf("the worktree is at %s, want the colleague's commit %s", head, theirWork)
	}
	upstream := testutil.Git(t, path, "rev-parse", "--abbrev-ref", "colleague@{upstream}")
	if upstream != "origin/colleague" {
		t.Fatalf("the branch tracks %q, want origin/colleague", upstream)
	}
}

func TestCreateFetchesRemoteBranch(t *testing.T) {
	clone, origin := testutil.NewClonedRepo(t)
	testutil.Git(t, origin, "switch", "--quiet", "--create", "colleague")
	testutil.CommitTo(t, origin, "theirs.txt", "their first commit\n")
	testutil.Git(t, clone, "fetch", "--quiet", "origin", "colleague")
	// They keep working after our last fetch.
	newest := testutil.CommitTo(t, origin, "theirs-again.txt", "their second commit\n")
	testutil.Git(t, origin, "switch", "--quiet", "main")

	path, err := Create(open(t, clone), "colleague", CreateOptions{}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}

	if head := testutil.Git(t, path, "rev-parse", "HEAD"); head != newest {
		t.Fatalf("the worktree is at %s, want the freshly fetched %s", head, newest)
	}
}

func TestCreateDroppedRemoteBranch(t *testing.T) {
	clone, origin := testutil.NewPushableClone(t)
	testutil.Git(t, clone, "switch", "--quiet", "--create", "done-branch")
	testutil.CommitTo(t, clone, "work.txt", "merged long ago\n")
	testutil.Git(t, clone, "push", "--quiet", "origin", "done-branch")
	testutil.Git(t, clone, "switch", "--quiet", "main")
	// What "holt rm" leaves after a merge, and normal without remote.origin.prune.
	testutil.Git(t, clone, "branch", "--delete", "--force", "done-branch")
	testutil.Git(t, origin, "update-ref", "-d", "refs/heads/done-branch")

	_, err := Create(open(t, clone), "done-branch", CreateOptions{}, io.Discard)

	if err == nil {
		t.Fatal("a worktree was created from a branch origin no longer has")
	}
	// git only says "couldn't find remote ref", with no way out.
	if !strings.Contains(err.Error(), "git fetch --prune") {
		t.Errorf("error %q does not say how to get out of it", err)
	}
}

func TestCreateOccupiedPath(t *testing.T) {
	clone, _ := testutil.NewClonedRepo(t)
	occupied := filepath.Join(filepath.Dir(clone), filepath.Base(clone)+"-worktrees", "feature")
	testutil.WriteFile(t, filepath.Join(occupied, "something.txt"), "in the way\n")

	_, err := Create(open(t, clone), "feature", CreateOptions{}, io.Discard)

	if err == nil {
		t.Fatal("a worktree was created over an occupied directory")
	}
	// git creates the branch first and notices the directory second.
	if localBranchExists(open(t, clone), "feature") {
		t.Error("a branch was left behind by the failed attempt")
	}
}

func TestCreateNoFetchWithoutOrigin(t *testing.T) {
	clone, origin := testutil.NewClonedRepo(t)
	// Removing the origin is the only proof holt did not reach for it.
	if err := os.RemoveAll(origin); err != nil {
		t.Fatal(err)
	}

	path, err := Create(open(t, clone), "feature", CreateOptions{SkipFetch: true}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

func TestCreateNoFetchLocalDefault(t *testing.T) {
	clone, origin := testutil.NewClonedRepo(t)
	local := testutil.Git(t, clone, "rev-parse", "origin/main")
	// --no-fetch must not pick this up.
	testutil.CommitTo(t, origin, "upstream.txt", "added after the clone\n")

	path, err := Create(open(t, clone), "feature", CreateOptions{SkipFetch: true}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}

	if head := testutil.Git(t, path, "rev-parse", "HEAD"); head != local {
		t.Fatalf("the branch is at %s, want the local origin/main at %s", head, local)
	}
}

func TestCreateNoFetchNoDefault(t *testing.T) {
	main := testutil.NewRepo(t)

	_, err := Create(open(t, main), "feature", CreateOptions{SkipFetch: true}, io.Discard)

	if err == nil {
		t.Fatal("a branch was created off a ref that does not exist")
	}
	// git echoes origin/main back too, so only holt's wording proves anything.
	if !strings.Contains(err.Error(), "no origin remote") {
		t.Errorf("error %q is git's, not holt's", err)
	}
}

func TestCreateExistingBranchOffline(t *testing.T) {
	clone, origin := testutil.NewClonedRepo(t)
	testutil.Git(t, clone, "switch", "--quiet", "--create", "existing")
	kept := testutil.CommitTo(t, clone, "work.txt", "work done earlier\n")
	testutil.Git(t, clone, "switch", "--quiet", "main")
	// The branch has its own history, so fetching buys nothing.
	if err := os.RemoveAll(origin); err != nil {
		t.Fatal(err)
	}

	path, err := Create(open(t, clone), "existing", CreateOptions{}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}

	if head := testutil.Git(t, path, "rev-parse", "HEAD"); head != kept {
		t.Fatalf("the worktree is at %s, want the branch's own commit %s", head, kept)
	}
}
