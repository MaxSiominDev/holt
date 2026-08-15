package mirror

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/MaxSiominDev/holt/internal/testutil"
)

func TestLinkCreatesSymlink(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.WriteFile(t, filepath.Join(main, "CLAUDE.local.md"), "personal notes\n")
	linked := testutil.AddWorktree(t, main, "feature")

	result, err := link(main, linked, []string{"CLAUDE.local.md"})
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(result.Linked, []string{"CLAUDE.local.md"}) {
		t.Fatalf("got %+v, want CLAUDE.local.md linked", result)
	}
	target, err := os.Readlink(filepath.Join(linked, "CLAUDE.local.md"))
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(main, "CLAUDE.local.md"); target != want {
		t.Fatalf("symlink points at %q, want %q", target, want)
	}
}

func TestLinkCreatesParentDirs(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.WriteFile(t, filepath.Join(main, ".claude", "settings.local.json"), "{}\n")
	linked := testutil.AddWorktree(t, main, "feature")

	if _, err := link(main, linked, []string{".claude/settings.local.json"}); err != nil {
		t.Fatal(err)
	}

	content, err := os.ReadFile(filepath.Join(linked, ".claude", "settings.local.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "{}\n" {
		t.Fatalf("got %q through the symlink", content)
	}
}

func TestLinkKeepsRealFile(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.WriteFile(t, filepath.Join(main, "CLAUDE.local.md"), "from the main checkout\n")
	linked := testutil.AddWorktree(t, main, "feature")
	testutil.WriteFile(t, filepath.Join(linked, "CLAUDE.local.md"), "written by hand\n")

	result, err := link(main, linked, []string{"CLAUDE.local.md"})
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(result.Blocked, []string{"CLAUDE.local.md"}) {
		t.Fatalf("got %+v, want the path reported as blocked", result)
	}
	content, err := os.ReadFile(filepath.Join(linked, "CLAUDE.local.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "written by hand\n" {
		t.Fatalf("the file was overwritten, it now holds %q", content)
	}
}

func TestLinkStopsAtMirroredDirectory(t *testing.T) {
	main := testutil.NewRepo(t)
	// Skills kept outside the repository, pointed at from a mirrored directory.
	outside := filepath.Join(filepath.Dir(main), "real-skills")
	testutil.WriteFile(t, filepath.Join(outside, "skill.md"), "written by hand\n")
	if err := os.MkdirAll(filepath.Join(main, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(main, ".claude", "skills")); err != nil {
		t.Fatal(err)
	}
	linked := testutil.AddWorktree(t, main, "feature")

	// The worktree's .claude is now a symlink into the main checkout, so the
	// second path resolves back there and would overwrite the user's own symlink.
	if _, err := link(main, linked, []string{".claude"}); err != nil {
		t.Fatal(err)
	}
	result, err := link(main, linked, []string{".claude/skills"})
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(result.Blocked, []string{".claude/skills"}) {
		t.Fatalf("got %+v, want the path left alone", result)
	}
	target, err := os.Readlink(filepath.Join(main, ".claude", "skills"))
	if err != nil {
		t.Fatal(err)
	}
	if target != outside {
		t.Fatalf("the main checkout's symlink now points at %q, want %q", target, outside)
	}
	content, err := os.ReadFile(filepath.Join(main, ".claude", "skills", "skill.md"))
	if err != nil {
		t.Fatalf("the user's file is no longer reachable: %v", err)
	}
	if string(content) != "written by hand\n" {
		t.Fatalf("the user's file was disturbed, it now holds %q", content)
	}
}

func TestLinkRepairsStaleSymlink(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.WriteFile(t, filepath.Join(main, "CLAUDE.local.md"), "personal notes\n")
	linked := testutil.AddWorktree(t, main, "feature")
	if err := os.Symlink(filepath.Join(main, "moved-away.md"), filepath.Join(linked, "CLAUDE.local.md")); err != nil {
		t.Fatal(err)
	}

	if _, err := link(main, linked, []string{"CLAUDE.local.md"}); err != nil {
		t.Fatal(err)
	}

	target, err := os.Readlink(filepath.Join(linked, "CLAUDE.local.md"))
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(main, "CLAUDE.local.md"); target != want {
		t.Fatalf("symlink still points at %q, want %q", target, want)
	}
}

func TestLinkExpandsGlob(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.WriteFile(t, filepath.Join(main, ".claude", "skills", "cdc-style-local", "SKILL.md"), "skill\n")
	testutil.WriteFile(t, filepath.Join(main, ".claude", "skills", "review-local", "SKILL.md"), "skill\n")
	testutil.WriteFile(t, filepath.Join(main, ".claude", "skills", "shared", "SKILL.md"), "skill\n")
	linked := testutil.AddWorktree(t, main, "feature")

	result, err := link(main, linked, []string{".claude/skills/*-local"})
	if err != nil {
		t.Fatal(err)
	}

	want := []string{
		filepath.Join(".claude", "skills", "cdc-style-local"),
		filepath.Join(".claude", "skills", "review-local"),
	}
	if !reflect.DeepEqual(result.Linked, want) {
		t.Fatalf("got %v, want only the local skills", result.Linked)
	}
	if _, err := os.Lstat(filepath.Join(linked, ".claude", "skills", "shared")); !os.IsNotExist(err) {
		t.Error("a directory that does not match the glob was linked")
	}
}

func TestLinkMissingSource(t *testing.T) {
	main := testutil.NewRepo(t)
	linked := testutil.AddWorktree(t, main, "feature")

	result, err := link(main, linked, []string{"CLAUDE.local.md"})
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(result.Unmatched, []string{"CLAUDE.local.md"}) {
		t.Fatalf("got %+v, want the pattern reported as missing", result)
	}
}

func TestLinkSkipsMainCheckout(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.WriteFile(t, filepath.Join(main, "CLAUDE.local.md"), "the real file\n")

	result, err := link(main, main, []string{"CLAUDE.local.md"})
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Linked)+len(result.Blocked)+len(result.Unmatched) != 0 {
		t.Fatalf("got %+v, want nothing done in the main checkout", result)
	}
	// A symlink to itself would destroy the real file.
	info, err := os.Lstat(filepath.Join(main, "CLAUDE.local.md"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatal("the real file in the main checkout became a symlink")
	}
}

func TestUnlinkOnlyOwnSymlinks(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.WriteFile(t, filepath.Join(main, "CLAUDE.local.md"), "personal notes\n")
	testutil.WriteFile(t, filepath.Join(main, "elsewhere.md"), "another file\n")
	ours := testutil.AddWorktree(t, main, "ours")
	theirs := testutil.AddWorktree(t, main, "theirs")

	if _, err := link(main, ours, []string{"CLAUDE.local.md"}); err != nil {
		t.Fatal(err)
	}
	// The same name, pointed somewhere of the user's own choosing.
	if err := os.Symlink(filepath.Join(main, "elsewhere.md"), filepath.Join(theirs, "CLAUDE.local.md")); err != nil {
		t.Fatal(err)
	}

	removedOurs, err := unlink(main, ours, []string{"CLAUDE.local.md"})
	if err != nil {
		t.Fatal(err)
	}
	removedTheirs, err := unlink(main, theirs, []string{"CLAUDE.local.md"})
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(removedOurs, []string{"CLAUDE.local.md"}) {
		t.Fatalf("got %v, want holt's own symlink removed", removedOurs)
	}
	if len(removedTheirs) != 0 {
		t.Fatalf("got %v, want a symlink holt did not create left in place", removedTheirs)
	}
	if _, err := os.Readlink(filepath.Join(theirs, "CLAUDE.local.md")); err != nil {
		t.Fatal("the user's own symlink was removed")
	}
}

func TestUnlinkDanglingSymlink(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.WriteFile(t, filepath.Join(main, "CLAUDE.local.md"), "personal notes\n")
	linked := testutil.AddWorktree(t, main, "feature")
	if _, err := link(main, linked, []string{"CLAUDE.local.md"}); err != nil {
		t.Fatal(err)
	}
	// The ordinary order: drop the file, then stop mirroring it.
	if err := os.Remove(filepath.Join(main, "CLAUDE.local.md")); err != nil {
		t.Fatal(err)
	}

	removed, err := unlink(main, linked, []string{"CLAUDE.local.md"})
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(removed, []string{"CLAUDE.local.md"}) {
		t.Fatalf("got %v, want the now-dangling symlink removed", removed)
	}
	if _, err := os.Lstat(filepath.Join(linked, "CLAUDE.local.md")); !os.IsNotExist(err) {
		t.Error("the worktree was left with a broken symlink")
	}
}

func TestInspectStates(t *testing.T) {
	main := testutil.NewRepo(t)
	for _, name := range []string{"linked.md", "absent.md", "blocked.md", "diverted.md", "other.md"} {
		testutil.WriteFile(t, filepath.Join(main, name), name+"\n")
	}
	worktreePath := testutil.AddWorktree(t, main, "feature")

	patterns := []string{"linked.md", "absent.md", "blocked.md", "diverted.md"}
	if err := os.Symlink(filepath.Join(main, "linked.md"), filepath.Join(worktreePath, "linked.md")); err != nil {
		t.Fatal(err)
	}
	testutil.WriteFile(t, filepath.Join(worktreePath, "blocked.md"), "a real file\n")
	if err := os.Symlink(filepath.Join(main, "other.md"), filepath.Join(worktreePath, "diverted.md")); err != nil {
		t.Fatal(err)
	}

	check, err := Inspect(main, worktreePath, patterns)
	if err != nil {
		t.Fatal(err)
	}

	for _, expectation := range []struct {
		name string
		got  []string
		want []string
	}{
		{"linked", check.Linked, []string{"linked.md"}},
		{"absent", check.Absent, []string{"absent.md"}},
		{"blocked", check.Blocked, []string{"blocked.md"}},
		{"diverted", check.Diverted, []string{"diverted.md"}},
	} {
		if !reflect.DeepEqual(expectation.got, expectation.want) {
			t.Errorf("%s: got %v, want %v", expectation.name, expectation.got, expectation.want)
		}
	}
}

func TestSyncAllWorktrees(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.WriteFile(t, filepath.Join(main, "CLAUDE.local.md"), "personal notes\n")
	first := testutil.AddWorktree(t, main, "first")
	second := testutil.AddWorktree(t, main, "second")

	results, err := Sync(openRepo(t, main), []string{"CLAUDE.local.md"})
	if err != nil {
		t.Fatal(err)
	}

	if len(results) != 2 {
		t.Fatalf("got %d results, want one per linked worktree", len(results))
	}
	for _, path := range []string{first, second} {
		if _, err := os.Readlink(filepath.Join(path, "CLAUDE.local.md")); err != nil {
			t.Errorf("%s was not mirrored: %v", path, err)
		}
	}
}

func TestSyncGoneWorktree(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.WriteFile(t, filepath.Join(main, "CLAUDE.local.md"), "personal notes\n")
	alive := testutil.AddWorktree(t, main, "alive")
	deleted := testutil.AddWorktree(t, main, "deleted")
	// Creating a symlink's parents would put the directory back, holding nothing.
	if err := os.RemoveAll(deleted); err != nil {
		t.Fatal(err)
	}

	results, err := Sync(openRepo(t, main), []string{"CLAUDE.local.md"})
	if err != nil {
		t.Fatal(err)
	}

	if _, statErr := os.Stat(deleted); !os.IsNotExist(statErr) {
		t.Error("the deleted worktree directory was rebuilt")
	}
	if _, statErr := os.Readlink(filepath.Join(alive, "CLAUDE.local.md")); statErr != nil {
		t.Error("the healthy worktree was not mirrored")
	}
	for _, result := range results {
		if result.Worktree.Path == deleted && !result.Gone {
			t.Error("the deleted worktree was not reported as gone")
		}
	}
}

func TestSyncOneUnknownPath(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.WriteFile(t, filepath.Join(main, "CLAUDE.local.md"), "personal notes\n")

	_, err := SyncOne(openRepo(t, main), []string{"CLAUDE.local.md"}, t.TempDir())

	if err == nil {
		t.Fatal("an unrelated directory was accepted as a worktree")
	}
}

func TestSyncUnreadableWorktree(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads a directory whatever its mode says")
	}
	main := testutil.NewRepo(t)
	testutil.WriteFile(t, filepath.Join(main, "CLAUDE.local.md"), "personal notes\n")
	unreadable := testutil.AddWorktree(t, main, "unreadable")
	healthy := testutil.AddWorktree(t, main, "healthy")
	if err := os.Chmod(unreadable, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(unreadable, 0o755) })

	results, err := Sync(openRepo(t, main), []string{"CLAUDE.local.md"})
	if err != nil {
		t.Fatalf("one unreadable worktree failed the whole repair: %v", err)
	}

	if _, err := os.Readlink(filepath.Join(healthy, "CLAUDE.local.md")); err != nil {
		t.Errorf("the healthy worktree was not mirrored: %v", err)
	}
	for _, result := range results {
		if result.Worktree.Path == unreadable && result.Err == nil {
			t.Error("the unreadable worktree was reported as mirrored")
		}
	}
}
