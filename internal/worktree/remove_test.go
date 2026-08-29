package worktree

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
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

	if err == nil {
		t.Fatal("a worktree holding an uncommitted file was deleted")
	}
	if _, statErr := os.Stat(filepath.Join(path, "notes.md")); statErr != nil {
		t.Fatal("the uncommitted file is gone")
	}
}

func TestRemoveClearsSlashedParent(t *testing.T) {
	clone, _ := testutil.NewClonedRepo(t)
	nested, err := Create(open(t, clone), "feature/x", CreateOptions{}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	parent := filepath.Dir(nested)

	if err := Remove(open(t, clone), nested, io.Discard); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(parent); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the namespace directory %s survived the removal", parent)
	}
	// The worktrees root itself belongs to the repository, not to the branch.
	if _, err := os.Stat(filepath.Dir(parent)); err != nil {
		t.Errorf("the worktrees directory was taken with it: %v", err)
	}
}

func TestRemoveKeepsOccupiedParent(t *testing.T) {
	clone, _ := testutil.NewClonedRepo(t)
	first, err := Create(open(t, clone), "feature/x", CreateOptions{}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Create(open(t, clone), "feature/y", CreateOptions{}, io.Discard); err != nil {
		t.Fatal(err)
	}

	if err := Remove(open(t, clone), first, io.Discard); err != nil {
		t.Fatal(err)
	}

	sibling := filepath.Join(filepath.Dir(first), "y")
	if _, err := os.Stat(sibling); err != nil {
		t.Fatalf("a sibling worktree was taken with the parent: %v", err)
	}
}

func TestRemoveKeepsDirectoriesOutsideTheWorktreesRoot(t *testing.T) {
	clone, _ := testutil.NewClonedRepo(t)
	// One worktree of holt's own, so the root exists for the walk to start from.
	if _, err := Create(open(t, clone), "kept", CreateOptions{SkipFetch: true}, io.Discard); err != nil {
		t.Fatal(err)
	}
	// And one made by hand outside holt's layout, every directory above it the user's.
	outside := filepath.Join(filepath.Dir(clone), "outside", "deep", "wt")
	testutil.Git(t, clone, "worktree", "add", "--quiet", outside, "-b", "byhand")

	if err := Remove(open(t, clone), outside, io.Discard); err != nil {
		t.Fatal(err)
	}

	// The walk climbs through empty directories and stops at holt's root; started
	// outside it, nothing stops it taking the user's on the way up.
	for _, dir := range []string{filepath.Dir(outside), filepath.Dir(filepath.Dir(outside))} {
		if _, err := os.Stat(dir); err != nil {
			t.Errorf("holt removed %s, which it never made: %v", dir, err)
		}
	}
}

func TestRemoveKeepsSymlinkedParent(t *testing.T) {
	clone, _ := testutil.NewClonedRepo(t)
	nested, err := Create(open(t, clone), "feature/x", CreateOptions{}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	// The namespace directory replaced by the user's own symlink, which removal
	// takes whatever it points at.
	parent := filepath.Dir(nested)
	elsewhere := filepath.Join(t.TempDir(), "elsewhere")
	testutil.WriteFile(t, filepath.Join(elsewhere, "keep.txt"), "not holt's\n")
	if err := os.RemoveAll(parent); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, parent); err != nil {
		t.Fatal(err)
	}

	pruneEmptyParents(worktreesRoot(clone), nested)

	if _, err := os.Lstat(parent); err != nil {
		t.Fatalf("a symlink holt did not create was removed: %v", err)
	}
}

func TestRelocatedPathTellsAMovedWorktreeFromAGoneOne(t *testing.T) {
	clone, _ := testutil.NewClonedRepo(t)
	// Two branches ending in the same segment land in directories of one name, and
	// git files the second under a counter, so entry and path part company.
	for _, branch := range []string{"login", "hotfix/login"} {
		if _, err := Create(open(t, clone), branch, CreateOptions{SkipFetch: true}, io.Discard); err != nil {
			t.Fatal(err)
		}
	}
	listed, err := List(open(t, clone))
	if err != nil {
		t.Fatal(err)
	}
	// The project directory moved by a rename or a restore: git records the old paths
	// while the worktrees sit where holt keeps them, one repair, not a prune, away.
	moved := filepath.Join(filepath.Dir(clone), "moved")
	if err := os.Mkdir(moved, 0o755); err != nil {
		t.Fatal(err)
	}
	movedClone := filepath.Join(moved, filepath.Base(clone))
	for _, name := range []string{filepath.Base(clone), filepath.Base(worktreesRoot(clone))} {
		if err := os.Rename(filepath.Join(filepath.Dir(clone), name), filepath.Join(moved, name)); err != nil {
			t.Fatal(err)
		}
	}

	repo := open(t, movedClone)
	for _, w := range listed[1:] {
		want := worktreePath(movedClone, w.Branch)
		if got := RelocatedPath(repo, movedClone, w); got != want {
			t.Errorf("%s: got %q, want the worktree found at %q", w.Branch, got, want)
		}
	}

	// Nothing at the place holt would keep it: this one really is gone.
	gone := Worktree{Path: filepath.Join("/elsewhere", "wt", "wiped"), Branch: "wiped"}
	if got := RelocatedPath(repo, movedClone, gone); got != "" {
		t.Errorf("got %q, want a worktree that is gone reported as gone", got)
	}
	// Something of the user's own sitting there instead: taken for the worktree, holt
	// keeps naming a repair that cannot run and never offers the prune.
	stray := Worktree{Path: filepath.Join("/elsewhere", "wt", "scratch"), Branch: "scratch"}
	testutil.WriteFile(t, worktreePath(movedClone, "scratch"), "notes of my own\n")
	if got := RelocatedPath(repo, movedClone, stray); got != "" {
		t.Errorf("got %q, want a file of the user's own left out of it", got)
	}
	// And a directory registered as some other worktree.
	elsewhere := Worktree{Path: filepath.Join("/elsewhere", "wt", "other"), Branch: "other"}
	testutil.WriteFile(t, filepath.Join(worktreePath(movedClone, "other"), ".git"), "gitdir: /somewhere/.git/worktrees/login\n")
	if got := RelocatedPath(repo, movedClone, elsewhere); got != "" {
		t.Errorf("got %q, want a directory registered as something else left out", got)
	}
}

func TestRemovePrunesUnderASymlinkedRoot(t *testing.T) {
	clone, _ := testutil.NewClonedRepo(t)
	elsewhere := filepath.Join(t.TempDir(), "real")
	if err := os.Mkdir(elsewhere, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, worktreesRoot(clone)); err != nil {
		t.Fatal(err)
	}
	if _, err := Create(open(t, clone), "feature/x", CreateOptions{SkipFetch: true}, io.Discard); err != nil {
		t.Fatal(err)
	}
	// The path as the caller has it, git's listing rather than holt's own spelling,
	// which is where the two part company.
	found, err := Find(open(t, clone), "feature/x")
	if err != nil {
		t.Fatal(err)
	}

	if err := Remove(open(t, clone), found.Path, io.Discard); err != nil {
		t.Fatal(err)
	}

	// The namespace directory git will not take back blocks "holt new feature" for
	// good, and a symlinked root is where the two spellings differ.
	if _, err := os.Lstat(filepath.Join(elsewhere, "feature")); !os.IsNotExist(err) {
		t.Errorf("the emptied namespace directory is still there: %v", err)
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
	// What "git push -u" leaves: the branch is its own upstream, so "git branch -d"
	// would drop it the moment it was pushed.
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
	// A tag on the merged base carrying the branch name, which git resolves first,
	// so a bare name would make the branch look merged.
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
	// A second commit never pushed, then the merge: the default branch carries both,
	// while "git branch --delete" reads the stale upstream and refuses.
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

func TestIgnoredFilesNamesTheFilesInAnUnignoredDirectory(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.WriteFile(t, filepath.Join(main, ".gitignore"), "*.log\n")
	testutil.Git(t, main, "add", ".gitignore")
	testutil.Git(t, main, "commit", "--quiet", "-m", "ignore logs")
	path := testutil.AddWorktree(t, main, "feature")
	// A directory merely full of ignored files arrives under git's default as the
	// single entry "logs/", naming nothing that is about to be lost.
	testutil.WriteFile(t, filepath.Join(path, "logs", "a.log"), "one\n")
	testutil.WriteFile(t, filepath.Join(path, "logs", "b.log"), "two\n")

	ignored, err := IgnoredFiles(open(t, main), path)
	if err != nil {
		t.Fatal(err)
	}

	want := []string{filepath.Join("logs", "a.log"), filepath.Join("logs", "b.log")}
	if !reflect.DeepEqual(ignored, want) {
		t.Fatalf("got %v, want %v", ignored, want)
	}
}

func TestIgnoredFilesGoneWorktree(t *testing.T) {
	main := testutil.NewRepo(t)
	path := testutil.AddWorktree(t, main, "feature")
	if err := os.RemoveAll(path); err != nil {
		t.Fatal(err)
	}

	ignored, err := IgnoredFiles(open(t, main), path)

	// git still lists it, and status in a directory that is gone exits non-zero,
	// which would stop "holt rm" removing it.
	if err != nil {
		t.Fatalf("got %v, want a worktree whose directory is gone to hold nothing", err)
	}
	if len(ignored) != 0 {
		t.Fatalf("got %v, want nothing", ignored)
	}
}

func TestIgnoredFilesBrokenWorktree(t *testing.T) {
	main := testutil.NewRepo(t)
	feature := testutil.AddWorktree(t, main, "feature")
	// What "holt ls" shows as broken: the gitdir pointer leads nowhere.
	testutil.WriteFile(t, filepath.Join(feature, ".git"), "gitdir: /nowhere/at/all\n")

	_, err := IgnoredFiles(open(t, main), feature)

	if err == nil {
		t.Fatal("expected an error for a worktree holt cannot read")
	}
	if !strings.Contains(err.Error(), feature) {
		t.Errorf("error %q does not name the worktree it is about", err)
	}
	if !strings.Contains(err.Error(), "removing it would take") {
		t.Errorf("error %q reads as a status command the user never ran", err)
	}
}

func TestRemoveRefusesFromInsideSpelledInAnotherCase(t *testing.T) {
	clone, _ := testutil.NewClonedRepo(t)
	feature := testutil.AddWorktree(t, clone, "feature")
	// The same directory under a spelling the filesystem folds: compared as text the
	// caller looks elsewhere, and holt deletes the directory the shell stands in.
	folded := caseFoldedPath(t, feature)
	t.Chdir(folded)

	err := Remove(open(t, clone), feature, io.Discard)

	if err == nil {
		t.Fatal("the worktree the caller is standing in was removed")
	}
	if !strings.Contains(err.Error(), "leave it first") {
		t.Errorf("error %q is not the refusal that keeps the shell somewhere real", err)
	}
}

// The same directory under a spelling only a case-folding filesystem resolves, or a
// skip where it does not.
func caseFoldedPath(t *testing.T, path string) string {
	t.Helper()
	base := filepath.Base(path)
	folded := filepath.Join(filepath.Dir(path), strings.ToUpper(base))
	if folded == path {
		t.Skip("the directory name has no other case to try")
	}
	if _, err := os.Stat(folded); err != nil {
		t.Skip("this filesystem tells the two spellings apart")
	}
	return folded
}
