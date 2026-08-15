package git_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/MaxSiominDev/holt/internal/git"
	"github.com/MaxSiominDev/holt/internal/testutil"
)

func TestOpenOutsideRepository(t *testing.T) {
	_, err := git.Open(t.TempDir())

	if !errors.Is(err, git.ErrNotARepository) {
		t.Fatalf("got %v, want ErrNotARepository", err)
	}
}

func TestOpenMissingDirectory(t *testing.T) {
	_, err := git.Open(filepath.Join(t.TempDir(), "absent"))

	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("got %v, want a not-exist error", err)
	}
	if errors.Is(err, git.ErrNotARepository) {
		t.Error("a missing directory was reported as a missing repository")
	}
}

func TestGitDirInMainCheckout(t *testing.T) {
	repo := testutil.NewRepo(t)

	// git answers ".git" here; only a linked worktree gets an absolute path.
	gitDir, err := open(t, repo).GitDir()
	if err != nil {
		t.Fatal(err)
	}

	if want := filepath.Join(repo, ".git"); gitDir != want {
		t.Fatalf("got %q, want %q", gitDir, want)
	}
}

func TestGitDirInLinkedWorktree(t *testing.T) {
	repo := testutil.NewRepo(t)
	linked := testutil.AddWorktree(t, repo, "feature")

	gitDir, err := open(t, linked).GitDir()
	if err != nil {
		t.Fatal(err)
	}

	// Per-worktree, unlike CommonDir: an interrupted rebase leaves its marker here.
	if want := filepath.Join(repo, ".git", "worktrees", "feature"); gitDir != want {
		t.Fatalf("got %q, want %q", gitDir, want)
	}
}

func TestCommonDirBareRepository(t *testing.T) {
	bare := testutil.NewBareRepo(t)

	// git answers "." here, so the relative form needs joining onto its own dir.
	common, err := open(t, bare).CommonDir()
	if err != nil {
		t.Fatal(err)
	}

	if common != bare {
		t.Fatalf("got %q, want %q", common, bare)
	}
}

func TestCommonDirWorktreeSubdir(t *testing.T) {
	repo := testutil.NewRepo(t)
	nested := filepath.Join(testutil.AddWorktree(t, repo, "feature"), "deep", "nested")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	common, err := open(t, nested).CommonDir()
	if err != nil {
		t.Fatal(err)
	}

	if want := filepath.Join(repo, ".git"); common != want {
		t.Fatalf("got %q, want %q", common, want)
	}
}

func TestCommonDirMainSubdir(t *testing.T) {
	repo := testutil.NewRepo(t)
	nested := filepath.Join(repo, "deep", "nested")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	opened, err := git.Open(nested)
	if err != nil {
		t.Fatal(err)
	}
	common, err := opened.CommonDir()
	if err != nil {
		t.Fatal(err)
	}

	// git answers "../../.git"; unresolved, holt's files would land two up.
	if want := filepath.Join(repo, ".git"); common != want {
		t.Fatalf("got %q, want %q", common, want)
	}
}

func TestCommonDirFromWorktree(t *testing.T) {
	repo := testutil.NewRepo(t)
	linked := testutil.AddWorktree(t, repo, "feature")

	opened, err := git.Open(linked)
	if err != nil {
		t.Fatal(err)
	}
	common, err := opened.CommonDir()
	if err != nil {
		t.Fatal(err)
	}

	if want := filepath.Join(repo, ".git"); common != want {
		t.Fatalf("got %q, want %q", common, want)
	}
}

func TestToplevelFromWorktree(t *testing.T) {
	repo := testutil.NewRepo(t)
	linked := testutil.AddWorktree(t, repo, "feature")

	opened, err := git.Open(linked)
	if err != nil {
		t.Fatal(err)
	}
	top, err := opened.Toplevel()
	if err != nil {
		t.Fatal(err)
	}

	if top != linked {
		t.Fatalf("got %q, want %q", top, linked)
	}
}

func TestConfigUnsetKey(t *testing.T) {
	opened := open(t, testutil.NewRepo(t))

	value, ok, err := opened.Config("holt.example")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatalf("got value %q, want the key reported as unset", value)
	}
}

func TestConfigSetKey(t *testing.T) {
	repo := testutil.NewRepo(t)
	testutil.Git(t, repo, "config", "holt.example", "trunk")

	value, ok, err := open(t, repo).Config("holt.example")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok || value != "trunk" {
		t.Fatalf("got (%q, %v), want (\"trunk\", true)", value, ok)
	}
}

func TestOutputExitError(t *testing.T) {
	opened := open(t, testutil.NewRepo(t))

	_, err := opened.Output("rev-parse", "--verify", "refs/heads/missing")

	var exit *git.ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("got %v, want an ExitError", err)
	}
	if exit.Code == 0 {
		t.Error("exit code was not captured")
	}
	if exit.Stderr == "" {
		t.Error("stderr was not captured, so git's own message is lost")
	}
}

func TestOutputForcesCLocale(t *testing.T) {
	main := testutil.NewRepo(t)
	// holt reads git's messages, which git translates unless LC_ALL is C. An
	// alias is the portable way to see what the child was handed.
	testutil.Git(t, main, "config", "alias.showlocale", "!printenv LC_ALL")
	t.Setenv("LC_ALL", "fr_FR.UTF-8")

	got, err := open(t, main).Output("showlocale")
	if err != nil {
		t.Fatal(err)
	}

	if got != "C" {
		t.Fatalf("git ran with LC_ALL=%q, want C", got)
	}
}

func open(t *testing.T, dir string) *git.Repo {
	t.Helper()
	repo, err := git.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	return repo
}
