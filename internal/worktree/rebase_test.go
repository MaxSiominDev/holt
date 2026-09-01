package worktree

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/MaxSiominDev/holt/internal/git"
	"github.com/MaxSiominDev/holt/internal/testutil"
)

func TestCurrentBranchNamesTheOperationTheWorktreeIsStoppedIn(t *testing.T) {
	clone, origin := testutil.NewClonedRepo(t)
	feature := branchWorktree(t, clone, "feature", "shared.txt", "the branch's version\n")
	testutil.CommitTo(t, origin, "shared.txt", "the default branch's version\n")
	// The state "holt rebase --no-abort" leaves when it cannot finish, from a real
	// conflict, since git detaches HEAD and a faked marker would not.
	if err := Rebase(open(t, feature), false, nil, io.Discard); !errors.Is(err, ErrRebaseStopped) {
		t.Fatalf("the fixture produced %v, want a stopped rebase", err)
	}

	_, err := CurrentBranch(open(t, feature))

	if err == nil {
		t.Fatal("a worktree with HEAD parked on a commit answered with a branch")
	}
	// "holt ls" shows the branch and "holt rm" names the rebase, so "no branch
	// checked out" would send the user after what is right there.
	if !strings.Contains(err.Error(), "rebase") {
		t.Errorf("error %q says nothing about the rebase the worktree is stopped in", err)
	}
}

func TestRebaseOntoFetchedDefault(t *testing.T) {
	clone, origin := testutil.NewClonedRepo(t)
	feature := branchWorktree(t, clone, "feature", "work.txt", "my work\n")
	// The default branch moves on after the worktree was made.
	upstream := testutil.CommitTo(t, origin, "upstream.txt", "someone else's work\n")

	if err := Rebase(open(t, feature), true, nil, io.Discard); err != nil {
		t.Fatal(err)
	}

	parent := testutil.Git(t, feature, "rev-parse", "HEAD~1")
	if parent != upstream {
		t.Fatalf("the branch sits on %s, want it replanted onto %s", parent, upstream)
	}
	if _, err := os.Stat(filepath.Join(feature, "work.txt")); err != nil {
		t.Error("the branch's own commit was lost")
	}
}

func TestRebaseStaleOriginHead(t *testing.T) {
	clone, origin := testutil.NewClonedRepo(t)
	feature := branchWorktree(t, clone, "feature", "work.txt", "my work\n")
	// The default at clone time and not any more, and it still resolves here.
	testutil.Git(t, origin, "branch", "initial-pr")
	testutil.AddRemoteBranch(t, clone, "initial-pr")
	testutil.Git(t, clone, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/initial-pr")
	upstream := testutil.CommitTo(t, origin, "upstream.txt", "main has moved on\n")

	if err := Rebase(open(t, feature), true, nil, io.Discard); err != nil {
		t.Fatal(err)
	}

	// The abandoned branch is not what this work has to merge into.
	if parent := testutil.Git(t, feature, "rev-parse", "HEAD~1"); parent != upstream {
		t.Fatalf("the branch sits on %s, want the tip of origin's real default branch %s", parent, upstream)
	}
}

func TestRebaseOnDefaultBranch(t *testing.T) {
	clone, _ := testutil.NewClonedRepo(t)

	err := Rebase(open(t, clone), true, nil, io.Discard)

	if err == nil {
		t.Fatal("rebasing the default branch onto itself was allowed")
	}
	if !strings.Contains(err.Error(), "main") {
		t.Errorf("error %q does not name the branch", err)
	}
}

func TestRebaseRefusesUncommitted(t *testing.T) {
	clone, _ := testutil.NewClonedRepo(t)
	feature := branchWorktree(t, clone, "feature", "work.txt", "my work\n")
	testutil.WriteFile(t, filepath.Join(feature, "work.txt"), "edited and not committed\n")

	err := Rebase(open(t, feature), true, nil, io.Discard)

	if err == nil {
		t.Fatal("a rebase was started over uncommitted changes")
	}
	if !strings.Contains(err.Error(), "uncommitted") {
		t.Errorf("error %q does not say what is wrong", err)
	}
}

func TestRebaseAllowsUntrackedFiles(t *testing.T) {
	clone, origin := testutil.NewClonedRepo(t)
	feature := branchWorktree(t, clone, "feature", "work.txt", "my work\n")
	testutil.CommitTo(t, origin, "upstream.txt", "someone else's work\n")
	// A rebase does not touch untracked files.
	testutil.WriteFile(t, filepath.Join(feature, "scratch.md"), "notes\n")

	if err := Rebase(open(t, feature), true, nil, io.Discard); err != nil {
		t.Fatalf("an untracked file blocked the rebase: %v", err)
	}
}

func TestRebaseDuringBisect(t *testing.T) {
	clone, _ := testutil.NewClonedRepo(t)
	feature := branchWorktree(t, clone, "feature", "work.txt", "my work\n")
	// A bisect leaves a clean tree at every step, so nothing else stops the
	// rebase: this guard is what does.
	testutil.Git(t, feature, "bisect", "start")
	testutil.Git(t, feature, "bisect", "bad")

	err := Rebase(open(t, feature), true, nil, io.Discard)

	if err == nil {
		t.Fatal("a rebase was started in a bisecting worktree")
	}
	// A bisect has no --abort and no --continue, so the wording the other
	// operations get sends the user to commands that do not exist.
	if !strings.Contains(err.Error(), "git bisect reset") {
		t.Errorf("error %q does not name the command that ends a bisect", err)
	}
	if strings.Contains(err.Error(), "abort") {
		t.Errorf("error %q offers an abort that bisect does not have", err)
	}
}

func TestRebaseDuringRebase(t *testing.T) {
	clone, origin := testutil.NewClonedRepo(t)
	feature := branchWorktree(t, clone, "feature", "shared.txt", "the branch's version\n")
	testutil.CommitTo(t, origin, "shared.txt", "the default branch's version\n")
	// A real conflict, because git detaches HEAD and a faked marker would not.
	if err := Rebase(open(t, feature), false, nil, io.Discard); !errors.Is(err, ErrRebaseStopped) {
		t.Fatalf("the fixture produced %v, want a stopped rebase", err)
	}

	err := Rebase(open(t, feature), true, nil, io.Discard)

	if err == nil {
		t.Fatal("a second rebase was started on top of an unfinished one")
	}
	if !strings.Contains(err.Error(), "unfinished rebase") {
		t.Errorf("error %q does not name the unfinished operation", err)
	}
}

func TestRebaseDuringMerge(t *testing.T) {
	clone, _ := testutil.NewClonedRepo(t)
	feature := branchWorktree(t, clone, "feature", "shared.txt", "the branch's version\n")
	testutil.Git(t, feature, "switch", "--quiet", "--create", "other", "HEAD~1")
	testutil.CommitTo(t, feature, "shared.txt", "another version\n")
	testutil.Git(t, feature, "switch", "--quiet", "feature")
	if _, err := runGit(feature, "merge", "other"); err == nil {
		t.Fatal("the fixture did not produce a conflicted merge")
	}

	err := Rebase(open(t, feature), true, nil, io.Discard)

	if err == nil || !strings.Contains(err.Error(), "unfinished merge") {
		t.Fatalf("got %v, want the unfinished merge named", err)
	}
}

func TestRebaseRefusedByHook(t *testing.T) {
	clone, origin := testutil.NewClonedRepo(t)
	feature := branchWorktree(t, clone, "feature", "work.txt", "my work\n")
	testutil.CommitTo(t, origin, "upstream.txt", "someone else's work\n")
	// pre-rebase runs first, so a refusal leaves no rebase to continue or abort.
	hooks := filepath.Join(testutil.Git(t, feature, "rev-parse", "--git-common-dir"), "hooks")
	testutil.WriteFile(t, filepath.Join(hooks, "pre-rebase"), "#!/bin/sh\nexit 1\n")
	if err := os.Chmod(filepath.Join(hooks, "pre-rebase"), 0o755); err != nil {
		t.Fatal(err)
	}

	err := Rebase(open(t, feature), true, nil, io.Discard)

	if err == nil {
		t.Fatal("a refused rebase was reported as success")
	}
	if errors.Is(err, ErrRebaseStopped) {
		t.Errorf("got %v, want no advice to continue a rebase that never began", err)
	}
}

func TestCurrentBranchDetachedHead(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.Git(t, main, "checkout", "--quiet", "--detach")

	_, err := CurrentBranch(open(t, main))

	if err == nil {
		t.Fatal("a detached HEAD was reported as a branch")
	}
	if !strings.Contains(err.Error(), "no branch") {
		t.Errorf("error %q does not say what is wrong", err)
	}
}

// runGit returns the error instead of ending the test, as testutil.Git would.
func runGit(dir string, args ...string) (string, error) {
	repo, err := git.Open(dir)
	if err != nil {
		return "", err
	}
	return repo.Output(args...)
}

func TestRebaseConflictAborts(t *testing.T) {
	clone, origin := testutil.NewClonedRepo(t)
	feature := branchWorktree(t, clone, "feature", "shared.txt", "the branch's version\n")
	// The same file, changed differently on the default branch.
	testutil.CommitTo(t, origin, "shared.txt", "the default branch's version\n")
	before := testutil.Git(t, feature, "rev-parse", "HEAD")
	var progress bytes.Buffer

	err := Rebase(open(t, feature), true, nil, &progress)

	if !errors.Is(err, ErrRebaseStopped) {
		t.Fatalf("got %v, want ErrRebaseStopped", err)
	}
	if operation, _ := OperationInProgress(open(t, feature)); operation != "" {
		t.Errorf("the worktree is left in a %s, want the rebase undone", operation)
	}
	// git parks HEAD on a commit for the length of a rebase, so an attached branch
	// at the old tip is what says the worktree is usable again.
	if branch, err := CurrentBranch(open(t, feature)); err != nil || branch != "feature" {
		t.Errorf("got branch %q (%v), want the worktree back on feature", branch, err)
	}
	if head := testutil.Git(t, feature, "rev-parse", "HEAD"); head != before {
		t.Errorf("the branch sits on %s, want it back on %s", head, before)
	}
	// The abort takes the unmerged entries with it, and nothing afterwards says
	// which files the two branches disagreed on. git names them in its own output
	// too, so the line holt prints is what this looks for.
	if !strings.Contains(progress.String(), "holt: conflicts in shared.txt") {
		t.Errorf("output %q does not name the file that conflicted", progress.String())
	}
	// git's own advice offers to finish or abort a rebase that is over by the time
	// anyone reads it.
	if strings.Contains(progress.String(), "git rebase --continue") {
		t.Errorf("output %q still sends the user to the rebase", progress.String())
	}
}

func TestRebaseConflictNamesFilesFromASubdirectory(t *testing.T) {
	clone, origin := testutil.NewClonedRepo(t)
	feature := branchWorktree(t, clone, "feature", "shared.txt", "the branch's version\n")
	testutil.CommitTo(t, origin, "shared.txt", "the default branch's version\n")
	// Set where a user would have it, in the configuration testutil keeps out.
	testutil.Git(t, feature, "config", "diff.relative", "true")
	sub := filepath.Join(feature, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	var progress bytes.Buffer

	// Run from the subdirectory, which is what diff.relative measures against: it
	// drops every conflict outside it, and the conflict here is above it.
	err := Rebase(open(t, sub), true, nil, &progress)

	if !errors.Is(err, ErrRebaseStopped) {
		t.Fatalf("got %v, want ErrRebaseStopped", err)
	}
	if !strings.Contains(progress.String(), "holt: conflicts in shared.txt") {
		t.Errorf("output %q does not name the file that conflicted", progress.String())
	}
}

func TestRebaseConflictKept(t *testing.T) {
	clone, origin := testutil.NewClonedRepo(t)
	feature := branchWorktree(t, clone, "feature", "shared.txt", "the branch's version\n")
	testutil.CommitTo(t, origin, "shared.txt", "the default branch's version\n")

	var progress bytes.Buffer
	err := Rebase(open(t, feature), false, nil, &progress)

	if !errors.Is(err, ErrRebaseStopped) {
		t.Fatalf("got %v, want ErrRebaseStopped", err)
	}
	if !strings.Contains(err.Error(), "git rebase --abort") {
		t.Errorf("error %q does not say how to get out of it", err)
	}
	// This path announces what holt settled, and it settled nothing here.
	if strings.Contains(progress.String(), "holt: merged") {
		t.Errorf("output %q claims a merge over an empty list of them", &progress)
	}
	// And git's advice is kept, since the conflict it names really is the user's
	// to finish from here.
	if !strings.Contains(progress.String(), "git rebase --skip") {
		t.Errorf("output %q is missing git's advice, which is still true under --no-abort", &progress)
	}
	// What --no-abort is for: the conflict is the user's to resolve, so holt must
	// not clean up after git.
	if operation, _ := OperationInProgress(open(t, feature)); operation != "rebase" {
		t.Errorf("got operation %q, want the stopped rebase left in place", operation)
	}
}

func TestRebaseConflictAbortFails(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes a directory whatever its mode says")
	}
	clone, origin := testutil.NewClonedRepo(t)
	feature := branchWorktree(t, clone, "feature", "shared.txt", "the branch's version\n")
	testutil.CommitTo(t, origin, "shared.txt", "the default branch's version\n")
	if err := Rebase(open(t, feature), false, nil, io.Discard); !errors.Is(err, ErrRebaseStopped) {
		t.Fatalf("the fixture produced %v, want a stopped rebase", err)
	}
	// git restores a file by replacing it, which needs write permission on the
	// directory holding it. A linked worktree keeps git's own state elsewhere, so
	// this stops the abort and nothing else. The permission cannot be taken away
	// before the rebase, which writes the conflict markers into the same directory.
	if err := os.Chmod(feature, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(feature, 0o755) })

	var progress bytes.Buffer
	// The reason holt would carry in: the file it would not settle by itself.
	err := abortStoppedRebase(open(t, feature), errors.New("shared.txt is not one of the files holt merges itself"),
		[]string{"CHANGELOG.md"}, &progress)

	if !errors.Is(err, ErrRebaseStopped) {
		t.Fatalf("got %v, want ErrRebaseStopped", err)
	}
	// Saying the branch is back where it was would send the user off with a rebase
	// still open in the worktree.
	if !strings.Contains(err.Error(), "still in it") {
		t.Errorf("error %q does not say the rebase is still there", err)
	}
	// The user is left standing in the conflict, so the reason holt stopped is
	// the one thing that says what to do about it.
	if !strings.Contains(err.Error(), "shared.txt") {
		t.Errorf("error %q does not say what holt would not settle", err)
	}
	if operation, _ := OperationInProgress(open(t, feature)); operation != "rebase" {
		t.Errorf("got operation %q, want the rebase git could not undo", operation)
	}
	// Said before the abort is attempted, or a failed abort leaves the user in a
	// conflict holt named nothing of.
	if !strings.Contains(progress.String(), "holt: conflicts in shared.txt") {
		t.Errorf("output %q does not name the file left conflicting", &progress)
	}
	// The abort did not happen, so what holt merged is still in the worktree and
	// is the user's to keep or undo along with the rest.
	if !strings.Contains(progress.String(), "holt: merged CHANGELOG.md") {
		t.Errorf("output %q leaves holt's own merge unmentioned in a rebase that is still standing", &progress)
	}
}

// branchWorktree adds a worktree on a new branch carrying one commit of its own.
func branchWorktree(t *testing.T, clone, branch, file, content string) string {
	t.Helper()
	path := testutil.AddWorktree(t, clone, branch)
	testutil.CommitTo(t, path, file, content)
	return path
}

func TestCurrentBranchWithSameNamedTag(t *testing.T) {
	repo := testutil.NewRepo(t)
	testutil.Git(t, repo, "switch", "--quiet", "--create", "feature")
	// Cutting a release tag named after its branch. git shortens a ref by
	// resolving tags first, so --short would answer heads/feature here.
	testutil.Git(t, repo, "tag", "feature")

	branch, err := CurrentBranch(open(t, repo))
	if err != nil {
		t.Fatal(err)
	}

	if branch != "feature" {
		t.Fatalf("got %q, want %q", branch, "feature")
	}
}

func TestRebaseOntoShadowedRemoteRef(t *testing.T) {
	clone, origin := testutil.NewClonedRepo(t)
	worktree := testutil.AddWorktree(t, clone, "feature")
	testutil.CommitTo(t, worktree, "work.txt", "my work\n")
	// A commit on the default branch that the rebase has to bring in.
	landed := testutil.CommitTo(t, origin, "theirs.txt", "landed on main\n")
	// A local branch of this name outranks the remote-tracking ref, and git rebases
	// onto it, warns among its progress and exits zero.
	testutil.Git(t, worktree, "branch", "origin/main", "HEAD")

	if err := Rebase(open(t, worktree), true, nil, io.Discard); err != nil {
		t.Fatal(err)
	}

	history := testutil.Git(t, worktree, "log", "--format=%H")
	if !strings.Contains(history, landed) {
		t.Fatal("the default branch's commit is not in the branch's history, so nothing was replanted")
	}
}

func TestListNamesBranchOfStoppedRebase(t *testing.T) {
	clone, origin := testutil.NewClonedRepo(t)
	worktree := testutil.AddWorktree(t, clone, "feature")
	testutil.CommitTo(t, worktree, "shared.txt", "the branch's version\n")
	testutil.CommitTo(t, origin, "shared.txt", "the default branch's version\n")

	if err := Rebase(open(t, worktree), false, nil, io.Discard); !errors.Is(err, ErrRebaseStopped) {
		t.Fatalf("the fixture no longer stops the rebase: %v", err)
	}

	// git parks HEAD on a commit while a rebase is stopped, and without the branch
	// nothing reaches the worktree holt's own rebase left there.
	if _, err := Find(open(t, clone), "feature"); err != nil {
		t.Fatalf("the worktree holt stopped a rebase in is unreachable: %v", err)
	}
	branches, err := LinkedBranches(open(t, clone), "")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(branches, "feature") {
		t.Errorf("completion offers %v, want the branch still listed", branches)
	}
}

func TestListRebaseStartedFromDetachedHead(t *testing.T) {
	clone, origin := testutil.NewClonedRepo(t)
	worktree := testutil.AddWorktree(t, clone, "feature")
	testutil.CommitTo(t, worktree, "shared.txt", "the branch's version\n")
	testutil.CommitTo(t, origin, "shared.txt", "the default branch's version\n")
	testutil.Git(t, clone, "fetch", "--quiet", "origin")
	// Detached on purpose, the way anyone looking at an older commit is. holt
	// refuses to rebase from here, so this is git's own doing.
	testutil.Git(t, worktree, "checkout", "--quiet", "--detach")
	if _, err := runGit(worktree, "rebase", "refs/remotes/origin/main"); err == nil {
		t.Fatal("the fixture no longer stops the rebase")
	}

	worktrees, err := List(open(t, clone))
	if err != nil {
		t.Fatal(err)
	}
	stopped := worktrees[1]

	// git writes the words "detached HEAD" where a ref would go. Taken for a
	// name, it reaches --porcelain and completion as a branch git forbids.
	if stopped.Branch != "" {
		t.Errorf("holt calls the branch %q, and no branch may be named that", stopped.Branch)
	}
	// What "holt ls" would need to tell this from a plain detached HEAD; nothing
	// reads it yet, removal being decided by asking git for the operation.
	if !stopped.Rebasing {
		t.Error("the worktree is not marked as mid-rebase")
	}
}
