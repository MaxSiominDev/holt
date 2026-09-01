package git_test

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
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
	// Go's own words for this name a call the user never made.
	if strings.Contains(err.Error(), "lstat") {
		t.Errorf("error %q is Go's rather than holt's", err)
	}
}

func TestGitDirInMainCheckout(t *testing.T) {
	repo := testutil.NewRepo(t)

	// git answers ".git" from the top of the main checkout; from a subdirectory
	// or a linked worktree it answers an absolute path.
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

func TestOutputExitErrorWithoutStderr(t *testing.T) {
	opened := open(t, testutil.NewRepo(t))

	// --quiet, which is how holt asks this question everywhere it asks it: git
	// then says nothing at all and only the status carries the answer.
	_, err := opened.Output("rev-parse", "--quiet", "--verify", "refs/heads/missing")

	if err == nil {
		t.Fatal("a ref that is not there was reported as found")
	}
	// Without a fallback the message ends at the colon, and the user is left
	// with a sentence naming a command and no reason at all.
	if !strings.Contains(err.Error(), "exit status") {
		t.Errorf("error %q says nothing about what happened", err)
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

func TestOutputIgnoresInheritedGitDir(t *testing.T) {
	here := testutil.NewRepo(t)
	elsewhere := testutil.NewRepo(t)
	t.Setenv("GIT_DIR", filepath.Join(elsewhere, ".git"))

	common, err := open(t, here).CommonDir()
	if err != nil {
		t.Fatal(err)
	}

	// --show-toplevel answers "here" either way, the working tree falling back to
	// the current directory; it is the repository directory that moves.
	if want := filepath.Join(here, ".git"); common != want {
		t.Fatalf("git ran against %q, want %q", common, want)
	}
}

func TestOutputIgnoresInheritedIndexFile(t *testing.T) {
	here := testutil.NewRepo(t)
	worktree := testutil.AddWorktree(t, here, "feature")
	// git hands hooks a relative GIT_INDEX_FILE, resolved against whatever directory the
	// command runs in, so inherited it reads a linked worktree through a .git that is a
	// file, where the status fails on a path that is not an index at all.
	t.Setenv("GIT_INDEX_FILE", filepath.Join(".git", "index"))

	_, err := open(t, worktree).Output("--no-optional-locks", "status", "--porcelain")

	if err != nil {
		t.Fatalf("git ran against the caller's index: %v", err)
	}
}

func TestOutputRawKeepsTheTrailingNewline(t *testing.T) {
	dir := testutil.NewRepo(t)
	repo := open(t, dir)
	// The last newline of a file is the file's own, and Output takes it for git's.
	testutil.WriteFile(t, filepath.Join(dir, "notes.md"), "one\ntwo\n")
	testutil.Git(t, dir, "add", "notes.md")

	got, err := repo.OutputRaw("cat-file", "blob", ":notes.md")
	if err != nil {
		t.Fatal(err)
	}

	if want := "one\ntwo\n"; string(got) != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRunWithoutEditorOutranksTheOneInTheEnvironment(t *testing.T) {
	dir := testutil.NewRepo(t)
	repo := open(t, dir)
	// GIT_EDITOR outranks the core.editor setting, so a command holt runs on the
	// user's behalf would otherwise drop them into whatever they have set.
	t.Setenv("GIT_EDITOR", "false")

	if err := repo.RunWithoutEditor(io.Discard, "commit", "--amend"); err != nil {
		t.Fatal(err)
	}

	// An editor that leaves the file alone keeps the message it was given.
	if got := testutil.Git(t, dir, "log", "-1", "--format=%s"); got != "initial" {
		t.Fatalf("got %q, want the message the commit already had", got)
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

func TestOutputKeepsTrailingSpace(t *testing.T) {
	dir := testutil.NewRepo(t)
	repo := open(t, dir)
	// A path may end in a space, and so may anything else git prints. Trimming
	// space rather than the line ending would quietly shorten it.
	testutil.Git(t, dir, "config", "holt.test", "value with a trailing ")

	got, err := repo.Output("config", "--get", "holt.test")
	if err != nil {
		t.Fatal(err)
	}

	if want := "value with a trailing "; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
