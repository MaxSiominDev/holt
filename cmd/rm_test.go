package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MaxSiominDev/holt/internal/testutil"
)

func TestRemoveRefusesCurrent(t *testing.T) {
	main := testutil.NewRepo(t)
	feature := testutil.AddWorktree(t, main, "feature")
	t.Chdir(feature)

	// git would delete it and leave the shell in a directory that is gone.
	err := runHoltExpectingFailure(t, "rm", "feature")

	if !strings.Contains(err.Error(), "holt home") {
		t.Errorf("error %q does not say how to get out first", err)
	}
	if _, statErr := os.Stat(feature); statErr != nil {
		t.Error("the worktree was removed anyway")
	}
}

func TestRemoveRefusesFromSubdirectory(t *testing.T) {
	main := testutil.NewRepo(t)
	feature := testutil.AddWorktree(t, main, "feature")
	nested := filepath.Join(feature, "deep", "nested")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(nested)

	runHoltExpectingFailure(t, "rm", "feature")

	if _, err := os.Stat(feature); err != nil {
		t.Error("the worktree the shell was standing in was removed")
	}
}

func TestRemoveDeletesSiblingPrefix(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.AddWorktree(t, main, "feature")
	sibling := testutil.AddWorktree(t, main, "feature-2")
	t.Chdir(sibling)

	// Comparing paths as plain strings would treat feature as inside feature-2.
	runHolt(t, "rm", "feature")

	if _, err := os.Stat(filepath.Join(filepath.Dir(sibling), "feature")); !os.IsNotExist(err) {
		t.Error("an unrelated worktree was left in place")
	}
}

func TestRemoveRefusesMainCheckout(t *testing.T) {
	main := testutil.NewRepo(t)
	t.Chdir(main)

	err := runHoltExpectingFailure(t, "rm", "main")

	if !strings.Contains(err.Error(), "main checkout") {
		t.Errorf("error %q does not say why", err)
	}
}

func TestRemoveNamesIgnoredFiles(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.WriteFile(t, filepath.Join(main, ".gitignore"), ".env\n")
	testutil.Git(t, main, "add", ".gitignore")
	testutil.Git(t, main, "commit", "-m", "ignore .env")
	feature := testutil.AddWorktree(t, main, "feature")
	// git takes this with the worktree, and nothing else has a copy of it.
	testutil.WriteFile(t, filepath.Join(feature, ".env"), "SECRET_TOKEN=hunter2\n")
	t.Chdir(main)

	stdout, _ := runHolt(t, "rm", "feature")

	if !strings.Contains(stdout, ".env") {
		t.Errorf("stdout %q never names the ignored file that went with the worktree", stdout)
	}
}

func TestRemoveDeletesMergedBranch(t *testing.T) {
	clone, _ := testutil.NewClonedRepo(t)
	feature := testutil.AddWorktree(t, clone, "feature")
	t.Chdir(clone)

	stdout, _ := runHolt(t, "rm", "feature")

	if _, err := os.Stat(feature); !os.IsNotExist(err) {
		t.Error("the directory is still there")
	}
	if !strings.Contains(stdout, "deleted branch feature") {
		t.Errorf("stdout %q does not report what happened to the branch", stdout)
	}
}

func TestRemoveKeepsUnmergedBranch(t *testing.T) {
	clone, _ := testutil.NewClonedRepo(t)
	feature := testutil.AddWorktree(t, clone, "feature")
	testutil.CommitTo(t, feature, "work.txt", "exists nowhere else\n")
	t.Chdir(clone)

	stdout, _ := runHolt(t, "rm", "feature")

	if !strings.Contains(stdout, "it holds commits the default branch does not") {
		t.Errorf("stdout %q does not report why the branch was kept", stdout)
	}
}

func TestRemoveKeepsUnverifiable(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.AddWorktree(t, main, "feature")
	t.Chdir(main)

	// No origin, so nothing justifies deleting a branch.
	stdout, _ := runHolt(t, "rm", "feature")

	if !strings.Contains(stdout, "no default branch here to check it against") {
		t.Errorf("stdout %q does not say why the branch was kept", stdout)
	}
}

func TestRemoveGoneWorktree(t *testing.T) {
	main := testutil.NewRepo(t)
	feature := testutil.AddWorktree(t, main, "feature")
	if err := os.RemoveAll(feature); err != nil {
		t.Fatal(err)
	}
	t.Chdir(main)

	// "holt ls" reports it as gone, so removing it has to work; plain
	// "git worktree remove" does.
	runHolt(t, "rm", "feature")

	if listed := testutil.Git(t, main, "worktree", "list"); strings.Contains(listed, "feature") {
		t.Fatalf("git still lists the worktree:\n%s", listed)
	}
}
