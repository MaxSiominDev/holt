package cmd

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/MaxSiominDev/holt/internal/testutil"
	"github.com/spf13/cobra"
)

func TestMirrorRemoveCounts(t *testing.T) {
	main := testutil.NewRepo(t)
	for _, skill := range []string{"cdc-style-local", "review-local"} {
		testutil.WriteFile(t, filepath.Join(main, ".claude", "skills", skill, "SKILL.md"), "skill\n")
	}
	testutil.AddWorktree(t, main, "first")
	testutil.AddWorktree(t, main, "second")
	t.Chdir(main)
	runHolt(t, "mirror", "add", ".claude/skills/*-local")

	out, _ := runHolt(t, "mirror", "rm", ".claude/skills/*-local")

	// Two skills into two worktrees.
	if want := "unlinked 4 symlinks from 2 worktrees"; !strings.Contains(out, want) {
		t.Fatalf("got %q, want it to contain %q", out, want)
	}
}

func TestMirrorRemoveNormalisesPath(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.WriteFile(t, filepath.Join(main, "CLAUDE.local.md"), "notes\n")
	worktreePath := testutil.AddWorktree(t, main, "feature")
	t.Chdir(main)
	runHolt(t, "mirror", "add", "CLAUDE.local.md")

	runHolt(t, "mirror", "rm", "./CLAUDE.local.md")

	if _, err := os.Lstat(filepath.Join(worktreePath, "CLAUDE.local.md")); !os.IsNotExist(err) {
		t.Fatal("the symlink survived: the unnormalised path did not match")
	}
}

func TestMirrorSyncRestoresExcludes(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.WriteFile(t, filepath.Join(main, "CLAUDE.local.md"), "notes\n")
	testutil.AddWorktree(t, main, "feature")
	t.Chdir(main)
	runHolt(t, "mirror", "add", "CLAUDE.local.md")

	// doctor sends the user here when the block is gone.
	exclude := filepath.Join(main, ".git", "info", "exclude")
	if err := os.WriteFile(exclude, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	runHolt(t, "mirror", "sync")

	content, err := os.ReadFile(exclude)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "/CLAUDE.local.md") {
		t.Fatalf("info/exclude holds %q, want holt's block restored", content)
	}
}

func TestMirrorSyncOneWorktree(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.WriteFile(t, filepath.Join(main, "CLAUDE.local.md"), "notes\n")
	first := testutil.AddWorktree(t, main, "first")
	second := testutil.AddWorktree(t, main, "second")
	t.Chdir(main)
	runHolt(t, "mirror", "add", "CLAUDE.local.md")
	for _, worktreePath := range []string{first, second} {
		if err := os.Remove(filepath.Join(worktreePath, "CLAUDE.local.md")); err != nil {
			t.Fatal(err)
		}
	}

	// What the post-checkout hook passes: only the worktree git just made.
	runHolt(t, "mirror", "sync", "--worktree", first)

	target, err := os.Readlink(filepath.Join(first, "CLAUDE.local.md"))
	if err != nil {
		t.Fatalf("the named worktree was not mirrored into: %v", err)
	}
	if want := filepath.Join(main, "CLAUDE.local.md"); target != want {
		t.Fatalf("the symlink points at %q, want %q", target, want)
	}
	if _, err := os.Lstat(filepath.Join(second, "CLAUDE.local.md")); !os.IsNotExist(err) {
		t.Error("a worktree that was not named got a symlink too")
	}
}

func TestHookLinksNewWorktree(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.WriteFile(t, filepath.Join(main, "CLAUDE.local.md"), "notes\n")
	holtOnPath(t)
	t.Chdir(main)
	// Installs the hook; nothing exists yet to mirror into.
	runHolt(t, "mirror", "add", "CLAUDE.local.md")

	feature := testutil.AddWorktree(t, main, "feature")

	link := filepath.Join(feature, "CLAUDE.local.md")
	target, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("the hook left the new worktree without the mirrored file: %v", err)
	}
	if want := filepath.Join(main, "CLAUDE.local.md"); target != want {
		t.Fatalf("the symlink points at %q, want %q", target, want)
	}
	content, err := os.ReadFile(link)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "notes\n" {
		t.Fatalf("reading through the symlink gives %q, want the main checkout's file", content)
	}
}

// The hook looks for holt on PATH. The test binary is no stand-in: the hook
// would run it as a test suite.
func holtOnPath(t *testing.T) {
	t.Helper()
	// go build has to run at the module root, which this file's own path gives.
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate this test's source file")
	}

	bin := t.TempDir()
	build := exec.Command("go", "build", "-o", filepath.Join(bin, "holt"), ".")
	build.Dir = filepath.Dir(filepath.Dir(file))
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building holt: %v\n%s", err, out)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// The streams stay apart: the shell function reads stdout as a path to enter.
func runHolt(t *testing.T, args ...string) (stdout, stderr string) {
	t.Helper()
	root, out, errOut := holtCommand(args)
	if err := root.Execute(); err != nil {
		t.Fatalf("holt %v: %v\n%s", args, err, errOut.String())
	}
	return out.String(), errOut.String()
}

func runHoltExpectingFailure(t *testing.T, args ...string) error {
	t.Helper()
	root, out, _ := holtCommand(args)
	err := root.Execute()
	if err == nil {
		t.Fatalf("holt %v succeeded, want an error\n%s", args, out.String())
	}
	return err
}

func holtCommand(args []string) (*cobra.Command, *bytes.Buffer, *bytes.Buffer) {
	root := newRootCommand("test")
	var out, errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs(args)
	return root, &out, &errOut
}

func TestMirrorSyncWorktreeSaysNothingWhenListIsEmpty(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.WriteFile(t, filepath.Join(main, "CLAUDE.local.md"), "notes\n")
	feature := testutil.AddWorktree(t, main, "feature")
	t.Chdir(main)
	runHolt(t, "mirror", "add", "CLAUDE.local.md")
	runHolt(t, "mirror", "rm", "CLAUDE.local.md")

	// The hook stays installed and calls this on every checkout, so an empty
	// list has to be silent rather than printing over every "git switch".
	stdout, stderr := runHolt(t, "mirror", "sync", "--worktree", feature)

	if stdout != "" || stderr != "" {
		t.Fatalf("the hook path printed %q / %q, want nothing", stdout, stderr)
	}
}
