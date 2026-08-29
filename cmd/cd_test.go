package cmd

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/MaxSiominDev/holt/internal/testutil"
	"github.com/spf13/cobra"
)

func TestCdUnreadableWorktreeIsNotCalledGone(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads a directory whatever its mode says")
	}
	main := testutil.NewRepo(t)
	feature := testutil.AddWorktree(t, main, "feature")
	// The directory is there and unreadable, which "holt ls" and doctor both say;
	// calling it gone sends the user after a directory that never left.
	parent := filepath.Dir(feature)
	if err := os.Chmod(parent, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(parent, 0o755) })
	t.Chdir(main)

	err := runHoltExpectingFailure(t, "cd", "feature")

	if strings.Contains(err.Error(), "gone") {
		t.Errorf("error %q calls a directory that is still there gone", err)
	}
}

func TestCdPrintsOnlyPath(t *testing.T) {
	main := testutil.NewRepo(t)
	feature := testutil.AddWorktree(t, main, "feature")
	t.Chdir(main)

	stdout, _ := runHolt(t, "cd", "feature")

	// A stray word on stdout would become part of the directory cd is handed.
	if stdout != feature+"\n" {
		t.Fatalf("stdout is %q, want exactly the path", stdout)
	}
}

func TestCdUnknownBranch(t *testing.T) {
	main := testutil.NewRepo(t)
	t.Chdir(main)

	err := runHoltExpectingFailure(t, "cd", "nowhere")

	if !strings.Contains(err.Error(), "nowhere") {
		t.Errorf("error %q does not name the branch", err)
	}
}

func TestCdWithoutBranch(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.AddWorktree(t, main, "feature")
	t.Chdir(main)

	err := runHoltExpectingFailure(t, "cd")

	if !strings.Contains(err.Error(), "feature") {
		t.Errorf("error %q does not list the worktrees that exist", err)
	}
}

func TestCdWithNoBranchAnywhere(t *testing.T) {
	main := testutil.NewRepo(t)
	// A worktree git made with --detach. holt never makes one, and reaches
	// worktrees by branch name, so there is nothing here for "holt cd" to take.
	testutil.Git(t, main, "worktree", "add", "--quiet", "--detach",
		filepath.Join(filepath.Dir(main), filepath.Base(main)+"-worktrees", "review"))
	t.Chdir(main)

	err := runHoltExpectingFailure(t, "cd")

	// "holt ls" one command earlier lists the worktree, so denying it exists
	// sends the user looking for a fault that is not there.
	if strings.Contains(err.Error(), "no linked worktrees yet") {
		t.Errorf("error %q denies a worktree that holt lists", err)
	}
}

func TestCdWithNoWorktreesAtAll(t *testing.T) {
	main := testutil.NewRepo(t)
	t.Chdir(main)

	err := runHoltExpectingFailure(t, "cd")

	// git lists the main checkout too, so a count forgetting it swaps this message
	// for the one about worktrees carrying no branch.
	if !strings.Contains(err.Error(), "no linked worktrees yet") {
		t.Errorf("error %q does not say the repository has none", err)
	}
}

func TestHomeFromWorktree(t *testing.T) {
	main := testutil.NewRepo(t)
	feature := testutil.AddWorktree(t, main, "feature")
	t.Chdir(feature)

	stdout, _ := runHolt(t, "home")

	if stdout != main+"\n" {
		t.Fatalf("stdout is %q, want exactly %q", stdout, main)
	}
}

func TestBranchCompletionSkipsMain(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.AddWorktree(t, main, "feature-one")
	t.Chdir(main)

	// The empty prefix: the main checkout is on "main" and must still be left out.
	completions, directive := completeWorktreeBranches(nil, nil, "")

	if slices.Contains(completions, "main") {
		t.Errorf("got %v, want the main checkout left out; it is reached with \"holt home\"", completions)
	}
	if !slices.Contains(completions, "feature-one") {
		t.Errorf("got %v, want the linked worktree offered", completions)
	}
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("got directive %v, want file completion suppressed", directive)
	}
}

func TestBranchCompletionPrefix(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.AddWorktree(t, main, "feature-one")
	testutil.AddWorktree(t, main, "feature-two")
	testutil.AddWorktree(t, main, "unrelated")
	t.Chdir(main)

	completions, _ := completeWorktreeBranches(nil, nil, "feature-")

	want := []string{"feature-one", "feature-two"}
	if !slices.Equal(completions, want) {
		t.Fatalf("got %v, want %v", completions, want)
	}
}

func TestCdMissingDirectory(t *testing.T) {
	main := testutil.NewRepo(t)
	feature := testutil.AddWorktree(t, main, "feature")
	if err := os.RemoveAll(feature); err != nil {
		t.Fatal(err)
	}
	t.Chdir(main)

	err := runHoltExpectingFailure(t, "cd", "feature")

	if !strings.Contains(err.Error(), "gone") {
		t.Errorf("error %q does not say the directory is missing", err)
	}
}
