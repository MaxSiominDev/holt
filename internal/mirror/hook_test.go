package mirror

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MaxSiominDev/holt/internal/testutil"
)

func TestHookDirDefault(t *testing.T) {
	main := testutil.NewRepo(t)

	dir, insideWorkTree, err := HookDir(openRepo(t, main))
	if err != nil {
		t.Fatal(err)
	}

	if want := filepath.Join(main, ".git", "hooks"); dir != want {
		t.Fatalf("got %q, want %q", dir, want)
	}
	if insideWorkTree {
		t.Error("the default hook directory was reported as part of the working tree")
	}
}

func TestHookDirAbsolutePath(t *testing.T) {
	main := testutil.NewRepo(t)
	// The shape a hook manager leaves: the default location, spelled out.
	configured := filepath.Join(main, ".git", "hooks")
	testutil.Git(t, main, "config", "core.hooksPath", configured)

	dir, insideWorkTree, err := HookDir(openRepo(t, main))
	if err != nil {
		t.Fatal(err)
	}

	if dir != configured {
		t.Fatalf("got %q, want %q", dir, configured)
	}
	if insideWorkTree {
		t.Error("a directory inside .git was reported as part of the working tree")
	}
}

func TestHookDirTildePath(t *testing.T) {
	main := testutil.NewRepo(t)
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	// Read literally, ~ is joined onto the working tree and the hook lands nowhere.
	testutil.Git(t, main, "config", "core.hooksPath", "~/hooks")

	dir, _, err := HookDir(openRepo(t, main))
	if err != nil {
		t.Fatal(err)
	}

	if want := filepath.Join(home, "hooks"); dir != want {
		t.Fatalf("got %q, want %q", dir, want)
	}
}

func TestHookDirRelativePath(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.Git(t, main, "config", "core.hooksPath", ".husky")

	dir, insideWorkTree, err := HookDir(openRepo(t, main))
	if err != nil {
		t.Fatal(err)
	}

	if want := filepath.Join(main, ".husky"); dir != want {
		t.Fatalf("got %q, want %q", dir, want)
	}
	// A hook written here would be an untracked file in the project.
	if !insideWorkTree {
		t.Error("a hook directory inside the project was not recognised as such")
	}
}

func TestHookDirOutsideRepository(t *testing.T) {
	main := testutil.NewRepo(t)
	elsewhere := t.TempDir()
	testutil.Git(t, main, "config", "core.hooksPath", elsewhere)

	_, insideWorkTree, err := HookDir(openRepo(t, main))
	if err != nil {
		t.Fatal(err)
	}

	if insideWorkTree {
		t.Error("a directory outside the repository was reported as part of the working tree")
	}
}

func TestHookDirDoubleDotName(t *testing.T) {
	main := testutil.NewRepo(t)
	// An ordinary directory name, not a step out of the project.
	testutil.Git(t, main, "config", "core.hooksPath", "..hooks")

	_, insideWorkTree, err := HookDir(openRepo(t, main))
	if err != nil {
		t.Fatal(err)
	}

	if !insideWorkTree {
		t.Error("a name beginning with two dots was mistaken for a path outside the project")
	}
}

func TestHookDirSymlinkIntoProject(t *testing.T) {
	main := testutil.NewRepo(t)
	if err := os.Mkdir(filepath.Join(main, ".husky"), 0o755); err != nil {
		t.Fatal(err)
	}
	disguise := filepath.Join(t.TempDir(), "hooks")
	if err := os.Symlink(filepath.Join(main, ".husky"), disguise); err != nil {
		t.Fatal(err)
	}
	testutil.Git(t, main, "config", "core.hooksPath", disguise)

	_, insideWorkTree, err := HookDir(openRepo(t, main))
	if err != nil {
		t.Fatal(err)
	}

	if !insideWorkTree {
		t.Error("a path reaching the project through a symlink escaped the check")
	}
}

func TestHookDirBareRepository(t *testing.T) {
	bare := testutil.NewBareRepo(t)

	dir, insideWorkTree, err := HookDir(openRepo(t, bare))
	if err != nil {
		t.Fatal(err)
	}

	if want := filepath.Join(bare, "hooks"); dir != want {
		t.Fatalf("got %q, want %q", dir, want)
	}
	if insideWorkTree {
		t.Error("hooks in a bare repository were reported as part of a working tree")
	}
}

func TestHookDirFromWorktree(t *testing.T) {
	main := testutil.NewRepo(t)
	linked := testutil.AddWorktree(t, main, "feature")

	dir, _, err := HookDir(openRepo(t, linked))
	if err != nil {
		t.Fatal(err)
	}

	if want := filepath.Join(main, ".git", "hooks"); dir != want {
		t.Fatalf("got %q, want %q", dir, want)
	}
}

func TestInspectHookMentionsMarker(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.WriteFile(t, filepath.Join(main, ".git", "hooks", "post-checkout"),
		"#!/bin/sh\n# this is not the hook # installed by holt\necho someone else\n")

	_, state, err := InspectHook(openRepo(t, main))
	if err != nil {
		t.Fatal(err)
	}

	if state != HookForeign {
		t.Fatalf("got state %v, want the hook treated as someone else's", state)
	}
}

func TestInstallHookExecutable(t *testing.T) {
	main := testutil.NewRepo(t)
	repo := openRepo(t, main)

	if _, err := InstallHook(repo, HookOptions{}); err != nil {
		t.Fatal(err)
	}

	path, state, err := InspectHook(repo)
	if err != nil {
		t.Fatal(err)
	}
	if state != HookOurs {
		t.Fatalf("got state %v, want the hook recognised as holt's own", state)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("hook mode is %v, git will not run a file it cannot execute", info.Mode())
	}
}

func TestInstallHookForeign(t *testing.T) {
	main := testutil.NewRepo(t)
	repo := openRepo(t, main)
	foreign := filepath.Join(main, ".git", "hooks", "post-checkout")
	testutil.WriteFile(t, foreign, "#!/bin/sh\necho someone else\n")

	_, err := InstallHook(repo, HookOptions{})

	if !errors.Is(err, ErrForeignHook) {
		t.Fatalf("got %v, want ErrForeignHook", err)
	}
	content, readErr := os.ReadFile(foreign)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !strings.Contains(string(content), "someone else") {
		t.Fatal("the existing hook was modified despite the refusal")
	}
}

func TestInstallHookForce(t *testing.T) {
	main := testutil.NewRepo(t)
	repo := openRepo(t, main)
	testutil.WriteFile(t, filepath.Join(main, ".git", "hooks", "post-checkout"), "#!/bin/sh\necho someone else\n")

	if _, err := InstallHook(repo, HookOptions{Replace: true}); err != nil {
		t.Fatal(err)
	}

	_, state, err := InspectHook(repo)
	if err != nil {
		t.Fatal(err)
	}
	if state != HookOurs {
		t.Fatalf("got state %v, want holt's hook in place", state)
	}
}

func TestInstallHookInWorkTree(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.Git(t, main, "config", "core.hooksPath", ".husky")

	_, err := InstallHook(openRepo(t, main), HookOptions{})

	if !errors.Is(err, ErrHookDirInWorkTree) {
		t.Fatalf("got %v, want ErrHookDirInWorkTree", err)
	}
	if _, statErr := os.Stat(filepath.Join(main, ".husky")); !os.IsNotExist(statErr) {
		t.Error("holt created the directory it had just refused to write to")
	}
}

func TestInstallHookThroughSymlink(t *testing.T) {
	main := testutil.NewRepo(t)
	tracked := filepath.Join(main, "tracked-hook.sh")
	testutil.WriteFile(t, tracked, "#!/bin/sh\necho tracked\n")
	// A hook manager may leave the slot as a symlink into the project.
	if err := os.Symlink(tracked, filepath.Join(main, ".git", "hooks", "post-checkout")); err != nil {
		t.Fatal(err)
	}

	if _, err := InstallHook(openRepo(t, main), HookOptions{Replace: true}); err != nil {
		t.Fatal(err)
	}

	content, err := os.ReadFile(tracked)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "#!/bin/sh\necho tracked\n" {
		t.Fatalf("the file behind the symlink was overwritten, it now holds %q", content)
	}
}

func TestHookRunsOnNewWorktree(t *testing.T) {
	main := testutil.NewRepo(t)
	if _, err := InstallHook(openRepo(t, main), HookOptions{}); err != nil {
		t.Fatal(err)
	}

	log := stubHoltOnPath(t)

	worktreePath := testutil.AddWorktree(t, main, "feature")

	recorded, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("the hook did not run for the new worktree: %v", err)
	}
	want := "mirror sync --worktree " + worktreePath
	if strings.TrimSpace(string(recorded)) != want {
		t.Fatalf("hook called holt with %q, want %q", strings.TrimSpace(string(recorded)), want)
	}
}

func TestHookIgnoresFileCheckout(t *testing.T) {
	main := testutil.NewRepo(t)
	if _, err := InstallHook(openRepo(t, main), HookOptions{}); err != nil {
		t.Fatal(err)
	}
	log := stubHoltOnPath(t)

	// git passes 0 as the third argument here, not 1.
	testutil.Git(t, main, "checkout", "--", "README.md")

	if _, err := os.Stat(log); !os.IsNotExist(err) {
		recorded, _ := os.ReadFile(log)
		t.Fatalf("holt ran for a file checkout with %q", recorded)
	}
}

// Stands in for the holt binary and returns the file it records its calls in.
func stubHoltOnPath(t *testing.T) string {
	t.Helper()
	log := filepath.Join(t.TempDir(), "calls.log")
	bin := t.TempDir()
	testutil.WriteFile(t, filepath.Join(bin, "holt"), "#!/bin/sh\necho \"$@\" >> '"+log+"'\n")
	if err := os.Chmod(filepath.Join(bin, "holt"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return log
}

func TestShellQuoteApostrophe(t *testing.T) {
	got := shellQuote("/Users/max's tools/holt")

	if want := `'/Users/max'\''s tools/holt'`; got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}
