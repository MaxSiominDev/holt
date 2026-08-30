package cmd

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/MaxSiominDev/holt/internal/mirror"
	"github.com/MaxSiominDev/holt/internal/testutil"
)

func TestMirrorList(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.AddWorktree(t, main, "feature")
	t.Chdir(main)

	empty, _ := runHolt(t, "mirror", "ls")
	if !strings.Contains(empty, "nothing is mirrored") {
		t.Errorf("an empty list printed %q", empty)
	}

	testutil.WriteFile(t, filepath.Join(main, "CLAUDE.local.md"), "personal notes\n")
	runHolt(t, "mirror", "add", "CLAUDE.local.md")
	runHolt(t, "mirror", "add", "gone.local.md")

	// Hand-written, the way someone editing the file by hand leaves it.
	list := filepath.Join(main, ".git", "holt", "mirror.list")
	content, err := os.ReadFile(list)
	if err != nil {
		t.Fatal(err)
	}
	testutil.WriteFile(t, list, string(content)+"../outside.md\n")

	stdout, _ := runHolt(t, "mirror", "ls")

	if !strings.Contains(stdout, "does not keep that line") {
		t.Errorf("the listing %q says nothing about the line it cannot read", stdout)
	}
	for _, want := range []string{"CLAUDE.local.md", "present", "gone.local.md", "not found"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("the listing %q does not mention %q", stdout, want)
		}
	}
	// Bound to its own row, or the two statuses would pass swapped.
	if row := rowFor(t, stdout, "gone.local.md"); !strings.Contains(row, "not found") {
		t.Errorf("row %q reports a pattern that matches nothing as present", row)
	}
}

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

// The hook looks for holt on PATH, and the test binary would run as a test suite.
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
	root, out, errOut := holtCommand(t, args...)
	if err := root.Execute(); err != nil {
		t.Fatalf("holt %v: %v\n%s", args, err, errOut.String())
	}
	return out.String(), errOut.String()
}

func runHoltExpectingFailure(t *testing.T, args ...string) error {
	t.Helper()
	root, out, _ := holtCommand(t, args...)
	err := root.Execute()
	if err == nil {
		t.Fatalf("holt %v succeeded, want an error\n%s", args, out.String())
	}
	return err
}

func TestMirrorSyncWorktreeSaysNothingWhenListIsEmpty(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.WriteFile(t, filepath.Join(main, "CLAUDE.local.md"), "notes\n")
	feature := testutil.AddWorktree(t, main, "feature")
	t.Chdir(main)
	runHolt(t, "mirror", "add", "CLAUDE.local.md")
	runHolt(t, "mirror", "rm", "CLAUDE.local.md")

	// The hook calls this on every checkout, so an empty list has to stay silent.
	stdout, stderr := runHolt(t, "mirror", "sync", "--worktree", feature)

	if stdout != "" || stderr != "" {
		t.Fatalf("the hook path printed %q / %q, want nothing", stdout, stderr)
	}
}

func TestMirrorRemoveKeepsLinkASurvivingPatternOwns(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.WriteFile(t, filepath.Join(main, "CLAUDE.local.md"), "notes\n")
	feature := testutil.AddWorktree(t, main, "feature")
	t.Chdir(main)
	// Two patterns over one file, which share the single symlink.
	runHolt(t, "mirror", "add", "CLAUDE.local.md")
	runHolt(t, "mirror", "add", "CLAUDE.local.*")

	runHolt(t, "mirror", "rm", "CLAUDE.local.*")

	if _, err := os.Lstat(filepath.Join(feature, "CLAUDE.local.md")); err != nil {
		t.Fatalf("the link the surviving pattern owns is gone: %v", err)
	}
}

func TestMirrorAddNamesMissingPathWithoutWorktrees(t *testing.T) {
	main := testutil.NewRepo(t)
	t.Chdir(main)

	// Setting the list up, which is when there is no worktree to notice it.
	stdout, _ := runHolt(t, "mirror", "add", "CLAUDE.locl.md")

	// "added CLAUDE.locl.md" carries the name too, so the phrase has to match.
	if !strings.Contains(stdout, "not found in the main checkout: CLAUDE.locl.md") {
		t.Errorf("stdout %q never says the path is not there", stdout)
	}
}

func TestMirrorAddNamesAMissingPathWithNoWorktreeToLookIn(t *testing.T) {
	main := testutil.NewRepo(t)
	feature := testutil.AddWorktree(t, main, "feature")
	t.Chdir(main)
	// The "not found" line comes from a worktree holt looked in, and with none left a
	// mistyped path would be added in silence until doctor is run.
	if err := os.RemoveAll(feature); err != nil {
		t.Fatal(err)
	}

	stdout, _ := runHolt(t, "mirror", "add", "typo.local.md")

	if !strings.Contains(stdout, "not found in the main checkout: typo.local.md") {
		t.Errorf("stdout %q says nothing about a path that is not there", stdout)
	}
}

func TestMirrorAddNamesAMissingPathWhenEveryWorktreeIsUnreadable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads a directory whatever its mode says")
	}
	main := testutil.NewRepo(t)
	testutil.WriteFile(t, filepath.Join(main, "CLAUDE.local.md"), "notes\n")
	feature := testutil.AddWorktree(t, main, "feature")
	t.Chdir(main)
	// A path that exists, so the unreadable directory becomes a failure, not a no-op.
	runHolt(t, "mirror", "add", "CLAUDE.local.md")
	// An unreadable worktree is the other way to a sync that looked in none, and it fails,
	// so the missing path has to be named before the failure is returned.
	if err := os.Chmod(feature, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(feature, 0o755) })

	root, out, _ := holtCommand(t, "mirror", "add", "typo.local.md")
	if err := root.Execute(); err == nil {
		t.Fatalf("a worktree holt cannot read was reported as mirrored:\n%s", out.String())
	}

	if !strings.Contains(out.String(), "not found in the main checkout: typo.local.md") {
		t.Errorf("stdout %q says nothing about a path that is not there", out.String())
	}
}

func TestMirrorSyncSeparatesALockedWorktreeFromAPrunableOne(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.WriteFile(t, filepath.Join(main, "CLAUDE.local.md"), "notes\n")
	prunable := testutil.AddWorktree(t, main, "prunable")
	locked := testutil.AddWorktree(t, main, "locked")
	t.Chdir(main)
	runHolt(t, "mirror", "add", "CLAUDE.local.md")
	// A locked entry outliving its directory is what lock is for, and prune spares
	// it, so naming prune is advice that never clears the line.
	testutil.Git(t, main, "worktree", "lock", locked)
	for _, path := range []string{prunable, locked} {
		if err := os.RemoveAll(path); err != nil {
			t.Fatal(err)
		}
	}

	stdout, _ := runHolt(t, "mirror", "sync")

	if !strings.Contains(stdout, "1 worktree git still lists but whose directory is gone (\"git worktree prune\")") {
		t.Errorf("stdout %q does not offer prune for the worktree prune would clear", stdout)
	}
	if !strings.Contains(stdout, "1 worktree locked with the directory not there") {
		t.Errorf("stdout %q does not name the locked worktree apart", stdout)
	}
	// Neither was linked into, so neither belongs in the count of worktrees reached.
	if !strings.Contains(stdout, "linked into 0 of 0 worktrees") {
		t.Errorf("stdout %q counts a worktree holt never reached", stdout)
	}
}

func TestMirrorAddRefusesInABareRepository(t *testing.T) {
	bare := testutil.NewBareRepo(t)
	t.Chdir(bare)

	err := runHoltExpectingFailure(t, "mirror", "add", "config")

	// git's listing names the repository directory in the main checkout's place, and
	// a pattern mirrored from there reaches git's own objects and refs.
	if !errors.Is(err, mirror.ErrBareRepository) {
		t.Fatalf("got %v, want the bare repository refused", err)
	}
	// git ships a template there, so holt's own block is what must be absent: a wide
	// enough pattern puts "/*" in it and nothing untracked shows up again.
	content, readErr := os.ReadFile(filepath.Join(bare, "info", "exclude"))
	if readErr == nil && strings.Contains(string(content), "holt mirror") {
		t.Errorf("holt wrote into the exclude file every worktree shares:\n%s", content)
	}
}

func TestMirrorSyncOneSpeaksAsHoltWhenItFails(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes a directory whatever its mode says")
	}
	main := testutil.NewRepo(t)
	testutil.WriteFile(t, filepath.Join(main, "sub", "note.local.md"), "notes\n")
	linked := testutil.AddWorktree(t, main, "feature")
	t.Chdir(main)
	runHolt(t, "mirror", "add", "sub/note.local.md")
	if err := os.Remove(filepath.Join(linked, "sub", "note.local.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(linked, "sub"), 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(linked, "sub"), 0o755) })

	// The hook's own call, inside a checkout nobody aimed at holt: returned to cobra
	// it prints as a bare Go error in the middle of "git checkout".
	stdout, _ := runHolt(t, "mirror", "sync", "--worktree", linked)

	if !strings.Contains(stdout, "holt: nothing was mirrored here") {
		t.Errorf("stdout %q does not say who is speaking", stdout)
	}
}

func TestMirrorSyncCountsOnlyTheWorktreesItLinkedInto(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.WriteFile(t, filepath.Join(main, "CLAUDE.local.md"), "notes\n")
	testutil.AddWorktree(t, main, "feature")
	t.Chdir(main)
	runHolt(t, "mirror", "add", "CLAUDE.local.md")
	// The file goes, so there is nothing left to link anywhere.
	if err := os.Remove(filepath.Join(main, "CLAUDE.local.md")); err != nil {
		t.Fatal(err)
	}

	stdout, _ := runHolt(t, "mirror", "sync")

	// Counting a worktree nothing was linked into reads as work done, in the same
	// breath as saying the file is not there.
	if !strings.Contains(stdout, "linked into 0 of 1 worktree") {
		t.Errorf("stdout %q counts a worktree nothing was linked into", stdout)
	}
}

func TestMirrorSyncSaysNothingAboutPruningForAMovedWorktree(t *testing.T) {
	main := testutil.CloneOf(t, testutil.NewRepo(t), "project")
	root := filepath.Dir(main)
	testutil.WriteFile(t, filepath.Join(main, "CLAUDE.local.md"), "notes\n")
	t.Chdir(main)
	runHolt(t, "mirror", "add", "CLAUDE.local.md")
	runHolt(t, "new", "feature")
	moved := filepath.Join(root, "moved")
	if err := os.Mkdir(moved, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"project", "project-worktrees"} {
		if err := os.Rename(filepath.Join(root, name), filepath.Join(moved, name)); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(filepath.Join(moved, "project"))

	stdout, _ := runHolt(t, "mirror", "sync")

	// A prune cures a worktree really gone and ruins one that only moved.
	if strings.Contains(stdout, "git worktree prune") {
		t.Errorf("stdout %q offers a prune for a worktree that only moved", stdout)
	}
}

func TestMirrorAddNamesGitAsTheOneInTheWay(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.CommitTo(t, main, "settings.json", "{}\n")
	testutil.AddWorktree(t, main, "feature")
	t.Chdir(main)

	stdout, _ := runHolt(t, "mirror", "add", "settings.json")

	// git writes a tracked file into every worktree itself, so this can never link,
	// and the report below would blame a stranger who is git.
	if !strings.Contains(stdout, "git tracks settings.json") {
		t.Errorf("stdout %q does not say why the path can never link", stdout)
	}
}

func TestMirrorAddQuotesThePathInTheAdviceItPrints(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.CommitTo(t, main, "my notes.md", "tracked\n")
	testutil.AddWorktree(t, main, "feature")
	t.Chdir(main)

	stdout, _ := runHolt(t, "mirror", "add", "my notes.md")

	// The line is there to be typed, and unquoted a path with a space in it runs
	// something else, or nothing.
	if !strings.Contains(stdout, `git rm --cached 'my notes.md'`) {
		t.Errorf("stdout %q hands back a command the shell would take apart", stdout)
	}
}

func TestMirrorAddSaysNothingAboutGitForAnUntrackedPath(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.WriteFile(t, filepath.Join(main, "CLAUDE.local.md"), "notes\n")
	testutil.AddWorktree(t, main, "feature")
	t.Chdir(main)

	stdout, _ := runHolt(t, "mirror", "add", "CLAUDE.local.md")

	if strings.Contains(stdout, "git tracks") {
		t.Errorf("stdout %q warns about a path git does not carry", stdout)
	}
}

func TestMirrorRemoveRelinksThePathsThatStayEvenWhenAWorktreeFails(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes a directory whatever its mode says")
	}
	main := testutil.NewRepo(t)
	testutil.WriteFile(t, filepath.Join(main, "d", "a.local.md"), "notes\n")
	reachable := testutil.AddWorktree(t, main, "reachable")
	blocked := testutil.AddWorktree(t, main, "blocked")
	t.Chdir(main)
	// Two patterns onto one file: removing either takes a link the other owns, which
	// the relink puts back.
	runHolt(t, "mirror", "add", "d/a.local.md")
	runHolt(t, "mirror", "add", "d/*")
	if err := os.Chmod(filepath.Join(blocked, "d"), 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(blocked, "d"), 0o755) })

	// One worktree failing must not cost the rest their repair, as everywhere else.
	_ = runHoltExpectingFailure(t, "mirror", "rm", "d/a.local.md")

	if _, err := os.Lstat(filepath.Join(reachable, "d", "a.local.md")); err != nil {
		t.Errorf("the link the surviving pattern owns was not put back: %v", err)
	}
}

func TestMirrorRemoveOfLastPathSaysNothingAboutLinking(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.WriteFile(t, filepath.Join(main, "CLAUDE.local.md"), "notes\n")
	testutil.AddWorktree(t, main, "feature")
	t.Chdir(main)
	runHolt(t, "mirror", "add", "CLAUDE.local.md")

	stdout, _ := runHolt(t, "mirror", "rm", "CLAUDE.local.md")

	// The whole line, not the absence of a phrase: removing the last path is where a
	// stray relink line would show up.
	if want := "removed CLAUDE.local.md, unlinked 1 symlink from 1 worktree\n"; stdout != want {
		t.Errorf("stdout %q, want %q", stdout, want)
	}
}

func TestMirrorSyncWorktreeSurvivesUnreadableList(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads a file whatever its mode says")
	}
	main := testutil.NewRepo(t)
	testutil.WriteFile(t, filepath.Join(main, "CLAUDE.local.md"), "notes\n")
	feature := testutil.AddWorktree(t, main, "feature")
	t.Chdir(main)
	runHolt(t, "mirror", "add", "CLAUDE.local.md")
	commonDir := filepath.Join(main, ".git")
	list := filepath.Join(commonDir, "holt", "mirror.list")
	if err := os.Chmod(list, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(list, 0o644) })

	// The hook runs this in every checkout, where a raw failure speaks Go, not holt.
	root, stdout, _ := holtCommand(t, "mirror", "sync", "--worktree", feature)
	if err := root.Execute(); err != nil {
		t.Fatalf("a file holt cannot read reached the checkout as a raw error: %v", err)
	}

	if !strings.Contains(stdout.String(), "holt doctor") {
		t.Errorf("stdout %q does not say where to look", stdout)
	}
}

func TestMirrorSyncRejectsEmptyWorktreeFlag(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.WriteFile(t, filepath.Join(main, "CLAUDE.local.md"), "notes\n")
	t.Chdir(main)
	runHolt(t, "mirror", "add", "CLAUDE.local.md")

	// Read as absent, this sweeps the repository from a hook meant for one worktree.
	err := runHoltExpectingFailure(t, "mirror", "sync", "--worktree", "")

	if !strings.Contains(err.Error(), "--worktree") {
		t.Errorf("error %q does not name the flag", err)
	}
}

func TestMirrorRemoveSaysNothingAboutLinking(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.WriteFile(t, filepath.Join(main, "CLAUDE.local.md"), "notes\n")
	testutil.AddWorktree(t, main, "feature")
	t.Chdir(main)
	// Two patterns over one file: removing either relinks what the other owns, which
	// is not what the user asked about.
	runHolt(t, "mirror", "add", "CLAUDE.local.md")
	runHolt(t, "mirror", "add", "CLAUDE.local.*")

	stdout, _ := runHolt(t, "mirror", "rm", "CLAUDE.local.*")

	if strings.Contains(stdout, "linked into") {
		t.Errorf("stdout %q reports linking on a command that unlinks", stdout)
	}
	if !strings.Contains(stdout, "removed CLAUDE.local.*") {
		t.Errorf("stdout %q does not report the removal", stdout)
	}
}
