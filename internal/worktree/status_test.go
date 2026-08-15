package worktree

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/MaxSiominDev/holt/internal/testutil"
)

func TestStatusesDrift(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.SetOriginHead(t, main, "main")
	feature := testutil.AddWorktree(t, main, "feature")
	commit(t, feature, "first.txt")
	commit(t, feature, "second.txt")
	// The default branch moves on once, so feature is two ahead and one behind.
	commit(t, main, "upstream.txt")
	testutil.Git(t, main, "update-ref", "refs/remotes/origin/main", testutil.Git(t, main, "rev-parse", "HEAD"))

	statuses, err := Statuses(open(t, main))
	if err != nil {
		t.Fatal(err)
	}

	status := find(t, statuses, "feature")
	if !status.Compared {
		t.Fatal("the branch was not compared against the default branch")
	}
	if status.Ahead != 2 || status.Behind != 1 {
		t.Fatalf("got ahead %d behind %d, want ahead 2 behind 1", status.Ahead, status.Behind)
	}
}

func TestStatusesWithoutRemote(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.AddWorktree(t, main, "feature")

	statuses, err := Statuses(open(t, main))
	if err != nil {
		t.Fatal(err)
	}

	if len(statuses) != 2 {
		t.Fatalf("got %d worktrees, want the listing to survive a repository with no origin", len(statuses))
	}
	if find(t, statuses, "feature").Compared {
		t.Error("drift was reported with no default branch to compare against")
	}
}

func TestStatusesModifiedFile(t *testing.T) {
	main := testutil.NewRepo(t)
	feature := testutil.AddWorktree(t, main, "feature")
	testutil.WriteFile(t, filepath.Join(feature, "README.md"), "edited in the worktree\n")

	statuses, err := Statuses(open(t, main))
	if err != nil {
		t.Fatal(err)
	}

	if find(t, statuses, "feature").State != StateDirty {
		t.Error("a modified tracked file did not mark the worktree dirty")
	}
	if find(t, statuses, "main").State != StateClean {
		t.Error("the untouched main checkout was not reported clean")
	}
}

func TestStatusesUntrackedIsDirty(t *testing.T) {
	main := testutil.NewRepo(t)
	feature := testutil.AddWorktree(t, main, "feature")
	// "git worktree remove" refuses over this too.
	testutil.WriteFile(t, filepath.Join(feature, "notes.md"), "written but never committed\n")

	statuses, err := Statuses(open(t, main))
	if err != nil {
		t.Fatal(err)
	}

	if find(t, statuses, "feature").State != StateDirty {
		t.Error("an untracked file did not mark the worktree dirty")
	}
}

func TestStatusesIgnoredIsClean(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.WriteFile(t, filepath.Join(main, ".gitignore"), "target/\n")
	testutil.Git(t, main, "add", ".gitignore")
	testutil.Git(t, main, "commit", "-m", "ignore build output")
	feature := testutil.AddWorktree(t, main, "feature")
	// Build output sits in every worktree and would mark them all dirty for good.
	testutil.WriteFile(t, filepath.Join(feature, "target", "app.jar"), "output\n")

	statuses, err := Statuses(open(t, main))
	if err != nil {
		t.Fatal(err)
	}

	if got := find(t, statuses, "feature").State; got != StateClean {
		t.Errorf("got state %q, want ignored build output not to count", got)
	}
}

func TestStatusesGoneWorktree(t *testing.T) {
	main := testutil.NewRepo(t)
	feature := testutil.AddWorktree(t, main, "feature")
	// git keeps listing a worktree until it is pruned.
	if err := os.RemoveAll(feature); err != nil {
		t.Fatal(err)
	}

	statuses, err := Statuses(open(t, main))
	if err != nil {
		t.Fatal(err)
	}

	if got := find(t, statuses, "feature").State; got != StateGone {
		t.Errorf("got state %q, want %q", got, StateGone)
	}
}

func TestStatusesBrokenWorktree(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.AddWorktree(t, main, "healthy")
	damaged := testutil.AddWorktree(t, main, "damaged")
	// The directory survives but git can no longer tell what it is.
	if err := os.Remove(filepath.Join(damaged, ".git")); err != nil {
		t.Fatal(err)
	}

	statuses, err := Statuses(open(t, main))
	if err != nil {
		t.Fatalf("one damaged worktree failed the whole listing: %v", err)
	}

	if got := find(t, statuses, "damaged").State; got != StateBroken {
		t.Errorf("got state %q for the damaged worktree, want %q", got, StateBroken)
	}
	if got := find(t, statuses, "healthy").State; got != StateClean {
		t.Errorf("got state %q for the healthy worktree, want it reported normally", got)
	}
}

func TestSupportsDriftEmptyRepo(t *testing.T) {
	// No HEAD to compare against, and reporting an outdated git would mislead.
	empty := testutil.NewEmptyRepo(t)

	if !SupportsDrift(open(t, empty)) {
		t.Fatal("a repository with no commits was reported as an outdated git")
	}
}

func TestParseAheadBehind(t *testing.T) {
	tests := []struct {
		name string
		out  string
		want map[string]drift
	}{
		{
			name: "one line per branch",
			out:  "main\t0 0\nfeature\t2 1\n",
			want: map[string]drift{"main": {ahead: 0, behind: 0}, "feature": {ahead: 2, behind: 1}},
		},
		{
			name: "a branch with no counts is skipped",
			out:  "main\t0 0\northogonal\t\n",
			want: map[string]drift{"main": {ahead: 0, behind: 0}},
		},
		{
			name: "unparsable numbers are skipped",
			out:  "main\tmany few\nfeature\t3 4\n",
			want: map[string]drift{"feature": {ahead: 3, behind: 4}},
		},
		{
			name: "empty output",
			out:  "",
			want: map[string]drift{},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := parseAheadBehind(test.out)

			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("got %v, want %v", got, test.want)
			}
		})
	}
}

func TestStatusesBranchSharingTagName(t *testing.T) {
	clone, _ := testutil.NewClonedRepo(t)
	feature := testutil.AddWorktree(t, clone, "release-1.0")
	// With a tag of the same name, %(refname:short) answers "heads/release-1.0".
	testutil.Git(t, clone, "tag", "release-1.0", "main")
	testutil.CommitTo(t, feature, "work.txt", "one commit ahead\n")

	statuses, err := Statuses(open(t, clone))
	if err != nil {
		t.Fatal(err)
	}

	status := find(t, statuses, "release-1.0")
	if !status.Compared {
		t.Fatal("the drift columns went blank for a branch that shares a tag name")
	}
	if status.Ahead != 1 {
		t.Fatalf("got %d ahead, want 1", status.Ahead)
	}
}

func commit(t *testing.T, dir, name string) {
	t.Helper()
	testutil.WriteFile(t, filepath.Join(dir, name), name+"\n")
	testutil.Git(t, dir, "add", name)
	testutil.Git(t, dir, "commit", "-m", "add "+name)
}

func find(t *testing.T, statuses []Status, branch string) Status {
	t.Helper()
	for _, status := range statuses {
		if status.Branch == branch {
			return status
		}
	}
	t.Fatalf("no worktree on branch %q in %+v", branch, statuses)
	return Status{}
}
