package worktree

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/MaxSiominDev/holt/internal/testutil"
)

func TestRemoveWorktree(t *testing.T) {
	main := testutil.NewRepo(t)
	path := testutil.AddWorktree(t, main, "feature")

	if err := Remove(open(t, main), path, io.Discard); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("the directory is still there")
	}
}

func TestRemoveDirtyWorktree(t *testing.T) {
	main := testutil.NewRepo(t)
	path := testutil.AddWorktree(t, main, "feature")
	testutil.WriteFile(t, filepath.Join(path, "notes.md"), "never committed\n")

	err := Remove(open(t, main), path, io.Discard)

	// holt never passes --force, so git's own refusal is the safety net.
	if err == nil {
		t.Fatal("a worktree holding an uncommitted file was deleted")
	}
	if _, statErr := os.Stat(filepath.Join(path, "notes.md")); statErr != nil {
		t.Fatal("the uncommitted file is gone")
	}
}

func TestDeleteMergedBranch(t *testing.T) {
	clone, _ := testutil.NewClonedRepo(t)
	testutil.Git(t, clone, "branch", "finished")

	outcome, err := DeleteMergedBranch(open(t, clone), "finished")
	if err != nil {
		t.Fatal(err)
	}

	if outcome != BranchDeleted {
		t.Fatalf("got %q, want the branch deleted", outcome)
	}
	if localBranchExists(open(t, clone), "finished") {
		t.Error("the branch is still there")
	}
}

func TestDeleteMergedBranchUnmerged(t *testing.T) {
	clone, _ := testutil.NewClonedRepo(t)
	testutil.Git(t, clone, "switch", "--quiet", "--create", "unfinished")
	testutil.CommitTo(t, clone, "work.txt", "exists nowhere else\n")
	testutil.Git(t, clone, "switch", "--quiet", "main")

	outcome, err := DeleteMergedBranch(open(t, clone), "unfinished")
	if err != nil {
		t.Fatal(err)
	}

	if outcome != BranchKeptUnmerged {
		t.Fatalf("got %q, want the branch kept", outcome)
	}
	if !localBranchExists(open(t, clone), "unfinished") {
		t.Error("the branch is gone")
	}
}

func TestDeleteMergedBranchPushed(t *testing.T) {
	clone, _ := testutil.NewClonedRepo(t)
	testutil.Git(t, clone, "switch", "--quiet", "--create", "mr-branch")
	head := testutil.CommitTo(t, clone, "work.txt", "waiting on a merge request\n")
	// What "git push -u" leaves: the branch is its own upstream, and
	// "git branch -d" would drop it the moment it was pushed.
	testutil.Git(t, clone, "update-ref", "refs/remotes/origin/mr-branch", head)
	testutil.Git(t, clone, "branch", "--set-upstream-to=origin/mr-branch", "mr-branch")
	testutil.Git(t, clone, "switch", "--quiet", "main")

	outcome, err := DeleteMergedBranch(open(t, clone), "mr-branch")
	if err != nil {
		t.Fatal(err)
	}

	if outcome != BranchKeptUnmerged {
		t.Fatalf("got %q, want a pushed but unmerged branch kept", outcome)
	}
	if !localBranchExists(open(t, clone), "mr-branch") {
		t.Fatal("work that only exists on this branch was deleted")
	}
}

func TestDeleteMergedBranchNoDefault(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.Git(t, main, "branch", "finished")

	// No origin, so there is nothing to check the branch against.
	outcome, err := DeleteMergedBranch(open(t, main), "finished")
	if err != nil {
		t.Fatal(err)
	}

	if outcome != BranchKeptUnverifiable {
		t.Fatalf("got %q, want the branch kept as unverifiable", outcome)
	}
	if !localBranchExists(open(t, main), "finished") {
		t.Error("a branch was deleted without anything to check it against")
	}
}

func TestDeleteMergedBranchTagShadow(t *testing.T) {
	clone, _ := testutil.NewClonedRepo(t)
	// A tag on the merged base carrying the branch name. git resolves a tag
	// first, so a bare name would answer for it and the branch would look merged.
	testutil.Git(t, clone, "tag", "release-1.0", "origin/main")
	testutil.Git(t, clone, "switch", "--quiet", "--create", "release-1.0")
	head := testutil.CommitTo(t, clone, "work.txt", "work nobody has seen\n")
	testutil.Git(t, clone, "switch", "--quiet", "main")

	outcome, err := DeleteMergedBranch(open(t, clone), "release-1.0")
	if err != nil {
		t.Fatal(err)
	}

	if outcome != BranchKeptUnmerged {
		t.Fatalf("got %q, want the branch kept", outcome)
	}
	if got := testutil.Git(t, clone, "rev-parse", "refs/heads/release-1.0"); got != head {
		t.Fatalf("the branch is at %s, want its own commit %s", got, head)
	}
}

func TestDeleteMergedBranchStaleUpstream(t *testing.T) {
	clone, _ := testutil.NewClonedRepo(t)
	testutil.Git(t, clone, "switch", "--quiet", "--create", "feature")
	pushed := testutil.CommitTo(t, clone, "work.txt", "first round\n")
	testutil.Git(t, clone, "update-ref", "refs/remotes/origin/feature", pushed)
	testutil.Git(t, clone, "branch", "--set-upstream-to=origin/feature", "feature")
	// A second commit never pushed, then the merge lands: the default branch
	// carries both, and "git branch --delete" reads the stale upstream and refuses.
	head := testutil.CommitTo(t, clone, "work.txt", "second round\n")
	testutil.Git(t, clone, "update-ref", "refs/remotes/origin/main", head)
	testutil.Git(t, clone, "switch", "--quiet", "main")

	outcome, err := DeleteMergedBranch(open(t, clone), "feature")
	if err != nil {
		t.Fatal(err)
	}

	if outcome != BranchDeleted {
		t.Fatalf("got %q, want the merged branch deleted", outcome)
	}
	if localBranchExists(open(t, clone), "feature") {
		t.Error("the branch survived, so the upstream was consulted instead of the ancestry")
	}
}

func TestIgnoredFilesGoneWorktree(t *testing.T) {
	main := testutil.NewRepo(t)
	path := testutil.AddWorktree(t, main, "feature")
	if err := os.RemoveAll(path); err != nil {
		t.Fatal(err)
	}

	ignored, err := IgnoredFiles(open(t, main), path)

	// git still lists it, and running status in a directory that is not there
	// fails outright, which would stop "holt rm" removing it.
	if err != nil {
		t.Fatalf("got %v, want a worktree whose directory is gone to hold nothing", err)
	}
	if len(ignored) != 0 {
		t.Fatalf("got %v, want nothing", ignored)
	}
}
