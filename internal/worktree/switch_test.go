package worktree

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MaxSiominDev/holt/internal/testutil"
)

func TestSwitchToDefault(t *testing.T) {
	clone, _ := testutil.NewClonedRepo(t)
	testutil.Git(t, clone, "switch", "--quiet", "--create", "feature")

	if err := SwitchToDefault(open(t, clone), io.Discard); err != nil {
		t.Fatal(err)
	}

	if branch := testutil.Git(t, clone, "rev-parse", "--abbrev-ref", "HEAD"); branch != "main" {
		t.Fatalf("the checkout is on %q, want main", branch)
	}
}

func TestSwitchToDefaultNamedTrunk(t *testing.T) {
	clone, _ := testutil.NewClonedRepo(t)
	// A default a hardcoded "main" would miss, and no local branch holds it yet.
	testutil.SetOriginHead(t, clone, "trunk")
	testutil.Git(t, clone, "switch", "--quiet", "--create", "feature")

	if err := SwitchToDefault(open(t, clone), io.Discard); err != nil {
		t.Fatal(err)
	}

	if branch := testutil.Git(t, clone, "rev-parse", "--abbrev-ref", "HEAD"); branch != "trunk" {
		t.Fatalf("the checkout is on %q, want trunk", branch)
	}
	if upstream := testutil.Git(t, clone, "rev-parse", "--abbrev-ref", "trunk@{upstream}"); upstream != "origin/trunk" {
		t.Errorf("the new branch tracks %q, want origin/trunk", upstream)
	}
}

func TestSwitchToDefaultInWorktree(t *testing.T) {
	clone, _ := testutil.NewClonedRepo(t)
	feature := testutil.AddWorktree(t, clone, "feature")

	err := SwitchToDefault(open(t, feature), io.Discard)

	if err == nil {
		t.Fatal("a linked worktree was moved off the branch it exists to hold")
	}
	if !strings.Contains(err.Error(), "holt home") {
		t.Errorf("error %q does not say how to reach the main checkout", err)
	}
	if branch := testutil.Git(t, feature, "rev-parse", "--abbrev-ref", "HEAD"); branch != "feature" {
		t.Errorf("the worktree moved to %q", branch)
	}
}

func TestSwitchToDefaultNoOrigin(t *testing.T) {
	main := testutil.NewRepo(t)

	err := SwitchToDefault(open(t, main), io.Discard)

	if err == nil {
		t.Fatal("a branch was checked out without knowing which one is the default")
	}
	if !errors.Is(err, ErrNoDefaultBranch) {
		t.Errorf("error %q is not ErrNoDefaultBranch", err)
	}
}

func TestSwitchToDefaultOffline(t *testing.T) {
	clone, origin := testutil.NewClonedRepo(t)
	testutil.Git(t, clone, "switch", "--quiet", "--create", "feature")
	// Removing the origin is the only proof holt did not reach for it.
	if err := os.RemoveAll(origin); err != nil {
		t.Fatal(err)
	}

	if err := SwitchToDefault(open(t, clone), io.Discard); err != nil {
		t.Fatal(err)
	}

	if branch := testutil.Git(t, clone, "rev-parse", "--abbrev-ref", "HEAD"); branch != "main" {
		t.Fatalf("the checkout is on %q, want main", branch)
	}
}

func TestSwitchToDefaultAlreadyThere(t *testing.T) {
	clone, _ := testutil.NewClonedRepo(t)

	if err := SwitchToDefault(open(t, clone), io.Discard); err != nil {
		t.Fatal(err)
	}

	if branch := testutil.Git(t, clone, "rev-parse", "--abbrev-ref", "HEAD"); branch != "main" {
		t.Fatalf("the checkout is on %q, want main", branch)
	}
}

func TestSwitchToDefaultDirtyConflict(t *testing.T) {
	clone, _ := testutil.NewClonedRepo(t)
	testutil.Git(t, clone, "switch", "--quiet", "--create", "feature")
	testutil.CommitTo(t, clone, "work.txt", "committed on feature\n")
	// main never had work.txt, so checking it out has to remove this edit.
	testutil.WriteFile(t, filepath.Join(clone, "work.txt"), "edited and not committed\n")

	err := SwitchToDefault(open(t, clone), io.Discard)

	if err == nil {
		t.Fatal("the checkout went through over uncommitted work")
	}
	content, readErr := os.ReadFile(filepath.Join(clone, "work.txt"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(content) != "edited and not committed\n" {
		t.Errorf("the edit was disturbed, the file now holds %q", content)
	}
}

func TestSwitchToDefaultInWorktreeFree(t *testing.T) {
	clone, _ := testutil.NewClonedRepo(t)
	feature := testutil.AddWorktree(t, clone, "feature")
	// Plain "git switch main" would succeed here, so the refusal is holt's own.
	testutil.Git(t, clone, "switch", "--quiet", "--create", "elsewhere")
	testutil.Git(t, clone, "branch", "--delete", "--force", "main")

	err := SwitchToDefault(open(t, feature), io.Discard)

	if err == nil {
		t.Fatal("a linked worktree was moved off the branch it exists to hold")
	}
	if branch := testutil.Git(t, feature, "rev-parse", "--abbrev-ref", "HEAD"); branch != "feature" {
		t.Errorf("the worktree moved to %q", branch)
	}
}

func TestSwitchToDefaultGuessOff(t *testing.T) {
	clone, _ := testutil.NewClonedRepo(t)
	// git's guess is what would otherwise build the missing local branch.
	testutil.Git(t, clone, "config", "checkout.guess", "false")
	testutil.Git(t, clone, "switch", "--quiet", "--create", "feature")
	testutil.Git(t, clone, "branch", "--delete", "--force", "main")

	if err := SwitchToDefault(open(t, clone), io.Discard); err != nil {
		t.Fatal(err)
	}

	if branch := testutil.Git(t, clone, "rev-parse", "--abbrev-ref", "HEAD"); branch != "main" {
		t.Fatalf("the checkout is on %q, want main", branch)
	}
}

func TestSwitchToDefaultSecondRemote(t *testing.T) {
	clone, origin := testutil.NewClonedRepo(t)
	// A fork carrying the same branch name, which git's guess gives up on.
	testutil.Git(t, clone, "remote", "add", "fork", origin)
	testutil.Git(t, clone, "fetch", "--quiet", "fork")
	testutil.Git(t, clone, "switch", "--quiet", "--create", "feature")
	testutil.Git(t, clone, "branch", "--delete", "--force", "main")

	if err := SwitchToDefault(open(t, clone), io.Discard); err != nil {
		t.Fatal(err)
	}

	if upstream := testutil.Git(t, clone, "rev-parse", "--abbrev-ref", "main@{upstream}"); upstream != "origin/main" {
		t.Fatalf("the branch tracks %q, want origin/main", upstream)
	}
}

func TestSwitchToDefaultCarriesWork(t *testing.T) {
	clone, _ := testutil.NewClonedRepo(t)
	testutil.Git(t, clone, "switch", "--quiet", "--create", "feature")
	// Nothing on main touches this file, so the edit rides along.
	testutil.WriteFile(t, filepath.Join(clone, "scratch.md"), "half-written\n")

	if err := SwitchToDefault(open(t, clone), io.Discard); err != nil {
		t.Fatal(err)
	}

	content, err := os.ReadFile(filepath.Join(clone, "scratch.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "half-written\n" {
		t.Errorf("the edit did not survive the switch, the file holds %q", content)
	}
}
