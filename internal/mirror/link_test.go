package mirror

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
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

func TestLinkTakesOverTheDirectoryItMadeForAnInnerPath(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.WriteFile(t, filepath.Join(main, ".claude", "settings.local.json"), "{}\n")
	testutil.WriteFile(t, filepath.Join(main, ".claude", "notes.md"), "notes\n")
	linked := testutil.AddWorktree(t, main, "feature")
	// The inner path mirrored first makes the directory, and the list naming the
	// directory comes later, which no ordering inside one run can help with.
	if _, err := link(main, linked, []string{".claude/settings.local.json"}); err != nil {
		t.Fatal(err)
	}

	result, err := link(main, linked, []string{".claude", ".claude/settings.local.json"})
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Blocked) != 0 {
		t.Errorf("holt calls %v blocked, want the directory of its own links taken over", result.Blocked)
	}
	// The whole directory, or every other file in it stays out for good.
	if _, err := os.Stat(filepath.Join(linked, ".claude", "notes.md")); err != nil {
		t.Errorf("the rest of the mirrored directory never arrived: %v", err)
	}
}

func TestLinkKeepsADirectoryHoldingSomethingOfTheUsers(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.WriteFile(t, filepath.Join(main, ".claude", "settings.local.json"), "{}\n")
	linked := testutil.AddWorktree(t, main, "feature")
	if _, err := link(main, linked, []string{".claude/settings.local.json"}); err != nil {
		t.Fatal(err)
	}
	// One file of the user's own is enough: the directory is no longer holt's.
	testutil.WriteFile(t, filepath.Join(linked, ".claude", "scratch.txt"), "mine\n")

	result, err := link(main, linked, []string{".claude"})
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(result.Blocked, []string{".claude"}) {
		t.Fatalf("got %+v, want the directory left alone", result)
	}
	if _, err := os.Stat(filepath.Join(linked, ".claude", "scratch.txt")); err != nil {
		t.Errorf("a file holt did not put there was removed: %v", err)
	}
}

func TestLinkTakesAwayItsOwnLinkToADeletedFile(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.WriteFile(t, filepath.Join(main, ".claude", "one.local"), "1\n")
	testutil.WriteFile(t, filepath.Join(main, ".claude", "two.local"), "2\n")
	linked := testutil.AddWorktree(t, main, "feature")
	if _, err := link(main, linked, []string{".claude/*.local"}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(main, ".claude", "one.local")); err != nil {
		t.Fatal(err)
	}

	result, err := link(main, linked, []string{".claude/*.local"})
	if err != nil {
		t.Fatal(err)
	}

	// The glob still matches the other file, so "holt mirror rm" of the pattern would
	// take the healthy link with it, and nothing narrower exists to run.
	if !reflect.DeepEqual(result.Cleared, []string{filepath.Join(".claude", "one.local")}) {
		t.Fatalf("got %+v, want the dead link taken away", result)
	}
	if _, err := os.Lstat(filepath.Join(linked, ".claude", "two.local")); err != nil {
		t.Errorf("the link whose file is still there went too: %v", err)
	}

	// And with the last of them gone, the directory holt made to hold them.
	if err := os.Remove(filepath.Join(main, ".claude", "two.local")); err != nil {
		t.Fatal(err)
	}
	if _, err := link(main, linked, []string{".claude/*.local"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(linked, ".claude")); !os.IsNotExist(err) {
		t.Errorf("the emptied directory holt made is still there: %v", err)
	}
}

func TestLinkTakesADeadLinkAwayOnceForOverlappingPatterns(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.WriteFile(t, filepath.Join(main, ".claude", "settings.local.json"), "{}\n")
	linked := testutil.AddWorktree(t, main, "feature")
	// A named path and a glob covering it, an ordinary way to write the list.
	patterns := []string{".claude/*.json", ".claude/settings.local.json"}
	if _, err := link(main, linked, patterns); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(main, ".claude", "settings.local.json")); err != nil {
		t.Fatal(err)
	}

	result, err := link(main, linked, patterns)

	// Named once per pattern, the link goes on the first pass and is missing on the
	// second, so a clean removal comes back as a failure that swallows its own report.
	if err != nil {
		t.Fatalf("a link removed as intended was reported as a failure: %v", err)
	}
	if !reflect.DeepEqual(result.Cleared, []string{filepath.Join(".claude", "settings.local.json")}) {
		t.Fatalf("got %+v, want the link named once", result)
	}
}

func TestLinkLeavesTheUsersOwnDeadLinkAlone(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.WriteFile(t, filepath.Join(main, "CLAUDE.local.md"), "notes\n")
	linked := testutil.AddWorktree(t, main, "feature")
	own := filepath.Join(t.TempDir(), "gone.md")
	if err := os.Symlink(own, filepath.Join(linked, "CLAUDE.local.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(main, "CLAUDE.local.md")); err != nil {
		t.Fatal(err)
	}

	result, err := link(main, linked, []string{"CLAUDE.local.md"})
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Cleared) != 0 {
		t.Fatalf("got %+v, want a link holt did not write left alone", result)
	}
	if _, err := os.Lstat(filepath.Join(linked, "CLAUDE.local.md")); err != nil {
		t.Errorf("a link holt did not write was removed: %v", err)
	}
}

func TestTrackedMatchesReadsTheNamesLiterally(t *testing.T) {
	main := testutil.NewRepo(t)
	repo := open(t, main)
	testutil.CommitTo(t, main, "abc.md", "part of the project\n")
	// A personal file whose name holds a star, which holt tells the user to escape.
	testutil.WriteFile(t, filepath.Join(main, "a*.md"), "mine\n")

	tracked, err := TrackedMatches(repo, main, `a\*.md`)
	if err != nil {
		t.Fatal(err)
	}

	// Handed to git as a pathspec the name globs onto abc.md, and holt would tell the
	// user to untrack a file that has nothing to do with the mirrored one.
	if len(tracked) != 0 {
		t.Fatalf("got %v, want nothing: git carries no file of that name", tracked)
	}
}

func TestInspectFindsLinksLeftByADeletedSource(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.WriteFile(t, filepath.Join(main, "CLAUDE.local.md"), "notes\n")
	linked := testutil.AddWorktree(t, main, "feature")
	if _, err := link(main, linked, []string{"CLAUDE.local.md"}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(main, "CLAUDE.local.md")); err != nil {
		t.Fatal(err)
	}

	check, err := Inspect(main, linked, []string{"CLAUDE.local.md"})
	if err != nil {
		t.Fatal(err)
	}

	// The pattern is expanded in the main checkout, so with the file gone every
	// worktree keeps a link leading nowhere while holt calls the set complete.
	if !reflect.DeepEqual(check.Stale, []string{"CLAUDE.local.md"}) {
		t.Fatalf("got %+v, want the link holt left behind", check)
	}
}

func TestInspectLeavesTheUsersOwnDeadLinkAlone(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.WriteFile(t, filepath.Join(main, "CLAUDE.local.md"), "notes\n")
	linked := testutil.AddWorktree(t, main, "feature")
	// A link of the user's own, which no holt command could put right anyway.
	if err := os.Symlink(filepath.Join(t.TempDir(), "gone.md"), filepath.Join(linked, "CLAUDE.local.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(main, "CLAUDE.local.md")); err != nil {
		t.Fatal(err)
	}

	check, err := Inspect(main, linked, []string{"CLAUDE.local.md"})
	if err != nil {
		t.Fatal(err)
	}

	if len(check.Stale) != 0 {
		t.Fatalf("got %+v, want a link holt did not write left out", check)
	}
}

func TestInspectReadsHoltsOwnDirectoryAsAbsent(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.WriteFile(t, filepath.Join(main, ".claude", "settings.local.json"), "{}\n")
	linked := testutil.AddWorktree(t, main, "feature")
	if _, err := link(main, linked, []string{".claude/settings.local.json"}); err != nil {
		t.Fatal(err)
	}

	check, err := Inspect(main, linked, []string{".claude"})
	if err != nil {
		t.Fatal(err)
	}

	// Called blocked, doctor names a stranger in the way of what sync repairs itself.
	if !reflect.DeepEqual(check.Absent, []string{".claude"}) || len(check.Blocked) != 0 {
		t.Fatalf("got %+v, want the directory reported as not linked yet", check)
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

	// The worktree's .claude is a symlink into the main checkout, so the second path
	// resolves back onto the user's own symlink.
	if _, err := link(main, linked, []string{".claude"}); err != nil {
		t.Fatal(err)
	}
	result, err := link(main, linked, []string{".claude/skills"})
	if err != nil {
		t.Fatal(err)
	}

	// Passed over without a word: the mirrored .claude already reaches the skills.
	if len(result.Blocked) != 0 || len(result.Linked) != 0 {
		t.Fatalf("got %+v, want the path passed over in silence", result)
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

func TestExpandBracketedCheckoutPath(t *testing.T) {
	// Directory names git has no opinion about, the checkout sitting where it likes.
	for _, name := range []string{"pro[ject", "pro[j]ect", "star*dir", `back\slash`} {
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), name)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			testutil.WriteFile(t, filepath.Join(dir, "CLAUDE.local.md"), "personal notes\n")

			matches, err := expand(dir, "CLAUDE.local.md")
			if err != nil {
				t.Fatal(err)
			}

			if !reflect.DeepEqual(matches, []string{"CLAUDE.local.md"}) {
				t.Fatalf("got %v, want the file found", matches)
			}
		})
	}
}

func TestLinkRepairsStaleSymlink(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.WriteFile(t, filepath.Join(main, "CLAUDE.local.md"), "personal notes\n")
	linked := testutil.AddWorktree(t, main, "feature")
	// What a moved main checkout strands: the same repo-relative path under a
	// directory that is no longer there.
	stale := filepath.Join(filepath.Dir(main), "where-it-used-to-be", "CLAUDE.local.md")
	if err := os.Symlink(stale, filepath.Join(linked, "CLAUDE.local.md")); err != nil {
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

func TestLinkKeepsSymlinkAimedElsewhere(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.WriteFile(t, filepath.Join(main, "CLAUDE.local.md"), "from the main checkout\n")
	linked := testutil.AddWorktree(t, main, "feature")
	// A link of the user's own, which the hook would replace on every checkout.
	own := filepath.Join(t.TempDir(), "notes-of-my-own.md")
	testutil.WriteFile(t, own, "written by hand\n")
	if err := os.Symlink(own, filepath.Join(linked, "CLAUDE.local.md")); err != nil {
		t.Fatal(err)
	}

	result, err := link(main, linked, []string{"CLAUDE.local.md"})
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(result.Blocked, []string{"CLAUDE.local.md"}) {
		t.Fatalf("got %+v, want the path left alone", result)
	}
	target, err := os.Readlink(filepath.Join(linked, "CLAUDE.local.md"))
	if err != nil {
		t.Fatal(err)
	}
	if target != own {
		t.Fatalf("the symlink now points at %q, want %q", target, own)
	}
}

func TestLinkKeepsSymlinkToSameRelativePath(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.WriteFile(t, filepath.Join(main, ".claude", "settings.local.json"), "{}\n")
	linked := testutil.AddWorktree(t, main, "feature")
	// Shared settings sitting at the very same path inside their own directory: by
	// the suffix alone holt would read one as its own from a moved main checkout.
	shared := filepath.Join(t.TempDir(), "dotfiles", ".claude", "settings.local.json")
	testutil.WriteFile(t, shared, `{"mine": true}`)
	if err := os.MkdirAll(filepath.Join(linked, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(shared, filepath.Join(linked, ".claude", "settings.local.json")); err != nil {
		t.Fatal(err)
	}

	result, err := link(main, linked, []string{".claude/settings.local.json"})
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(result.Blocked, []string{".claude/settings.local.json"}) {
		t.Fatalf("got %+v, want the path left alone", result)
	}
	target, err := os.Readlink(filepath.Join(linked, ".claude", "settings.local.json"))
	if err != nil {
		t.Fatal(err)
	}
	if target != shared {
		t.Fatalf("the symlink now points at %q, want %q", target, shared)
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
}

func TestUnlinkOnlyOwnSymlinks(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.WriteFile(t, filepath.Join(main, "CLAUDE.local.md"), "personal notes\n")
	testutil.WriteFile(t, filepath.Join(main, "elsewhere.md"), "another file\n")
	mine := testutil.AddWorktree(t, main, "mine")
	theirs := testutil.AddWorktree(t, main, "theirs")

	if _, err := link(main, mine, []string{"CLAUDE.local.md"}); err != nil {
		t.Fatal(err)
	}
	// The same name, pointed somewhere of the user's own choosing.
	if err := os.Symlink(filepath.Join(main, "elsewhere.md"), filepath.Join(theirs, "CLAUDE.local.md")); err != nil {
		t.Fatal(err)
	}

	removedMine, err := unlink(main, mine, []string{"CLAUDE.local.md"})
	if err != nil {
		t.Fatal(err)
	}
	removedTheirs, err := unlink(main, theirs, []string{"CLAUDE.local.md"})
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(removedMine, []string{"CLAUDE.local.md"}) {
		t.Fatalf("got %v, want holt's own symlink removed", removedMine)
	}
	if len(removedTheirs) != 0 {
		t.Fatalf("got %v, want a symlink holt did not create left in place", removedTheirs)
	}
	if _, err := os.Readlink(filepath.Join(theirs, "CLAUDE.local.md")); err != nil {
		t.Fatal("the user's own symlink was removed")
	}
}

func TestUnlinkKeepsDanglingLink(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.WriteFile(t, filepath.Join(main, "CLAUDE.local.md"), "personal notes\n")
	linked := testutil.AddWorktree(t, main, "feature")
	// A dangling link could be holt's stranded one or the user's own with the target
	// gone. Replacing it leaves something working; removing it is a loss on a guess.
	dangling := filepath.Join(filepath.Dir(main), "gone", "CLAUDE.local.md")
	if err := os.Symlink(dangling, filepath.Join(linked, "CLAUDE.local.md")); err != nil {
		t.Fatal(err)
	}

	removed, err := unlink(main, linked, []string{"CLAUDE.local.md"})
	if err != nil {
		t.Fatal(err)
	}

	if len(removed) != 0 {
		t.Fatalf("got %v, want nothing removed", removed)
	}
	if _, err := os.Lstat(filepath.Join(linked, "CLAUDE.local.md")); err != nil {
		t.Error("a symlink holt could not attribute to itself was deleted")
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

	results, err := Sync(open(t, main), []string{"CLAUDE.local.md"})
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

	results, err := Sync(open(t, main), []string{"CLAUDE.local.md"})
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

	_, err := SyncOne(open(t, main), []string{"CLAUDE.local.md"}, t.TempDir())

	if err == nil {
		t.Fatal("an unrelated directory was accepted as a worktree")
	}
}

func TestLinkLeavesADirectoryItCannotLookInside(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads a directory whatever its mode says")
	}
	main := testutil.NewRepo(t)
	testutil.WriteFile(t, filepath.Join(main, ".claude", "skills", "one.md"), "skill\n")
	linked := testutil.AddWorktree(t, main, "feature")
	// A directory of the user's own at the mirrored path, closed to holt, so what is
	// inside is unknown and not holt's to take.
	if err := os.MkdirAll(filepath.Join(linked, ".claude", "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	testutil.WriteFile(t, filepath.Join(linked, ".claude", "skills", "mine.md"), "theirs\n")
	if err := os.Chmod(filepath.Join(linked, ".claude", "skills"), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(linked, ".claude", "skills"), 0o755) })

	result, err := link(main, linked, []string{".claude/skills"})
	if err != nil {
		t.Fatal(err)
	}

	// Read as holt's own it goes whole, with only the permission bits in the way.
	if !reflect.DeepEqual(result.Blocked, []string{filepath.Join(".claude", "skills")}) {
		t.Fatalf("got %+v, want the directory reported and left alone", result)
	}
	if _, statErr := os.Lstat(filepath.Join(linked, ".claude", "skills")); statErr != nil {
		t.Errorf("the directory holt could not look inside was removed: %v", statErr)
	}
}

func TestUnlinkKeepsALinkOfTheUsersAimedAtAnAlias(t *testing.T) {
	main := testutil.NewRepo(t)
	repo := open(t, main)
	testutil.WriteFile(t, filepath.Join(main, "CLAUDE.local.md"), "notes\n")
	// The user's own alias beside the file, with their link aimed at it rather than
	// at the file holt mirrors.
	if err := os.Symlink("CLAUDE.local.md", filepath.Join(main, "CLAUDE.local.alias")); err != nil {
		t.Fatal(err)
	}
	linked := testutil.AddWorktree(t, main, "feature")
	own := filepath.Join(main, "CLAUDE.local.alias")
	if err := os.Symlink(own, filepath.Join(linked, "CLAUDE.local.md")); err != nil {
		t.Fatal(err)
	}

	links, _, err := Unsync(repo, []string{"CLAUDE.local.md"})
	if err != nil {
		t.Fatal(err)
	}

	// Followed rather than read, both reach one file and holt takes a link it did not write.
	if links != 0 {
		t.Errorf("holt unlinked %d links, want the user's own left alone", links)
	}
	target, readErr := os.Readlink(filepath.Join(linked, "CLAUDE.local.md"))
	if readErr != nil || target != own {
		t.Errorf("the link is now %q (%v), want the user's %q", target, readErr, own)
	}
}

func TestUnlinkKeepsAParentTheUserMadeASymlink(t *testing.T) {
	main := testutil.NewRepo(t)
	repo := open(t, main)
	testutil.WriteFile(t, filepath.Join(main, ".claude", "settings.local.json"), "{}\n")
	linked := testutil.AddWorktree(t, main, "feature")
	// The parent replaced by the user's own symlink, aimed elsewhere in the worktree.
	testutil.WriteFile(t, filepath.Join(linked, "config", "claude", "keep.txt"), "mine\n")
	if err := os.Symlink(filepath.Join("config", "claude"), filepath.Join(linked, ".claude")); err != nil {
		t.Fatal(err)
	}
	if _, err := Sync(repo, []string{".claude/settings.local.json"}); err != nil {
		t.Fatal(err)
	}

	if _, _, err := Unsync(repo, []string{".claude/settings.local.json"}); err != nil {
		t.Fatal(err)
	}

	// os.Remove takes a symlink whatever it points at, so emptiness is asked of the
	// link itself and not of what it leads to.
	if _, err := os.Lstat(filepath.Join(linked, ".claude")); err != nil {
		t.Errorf("a symlink holt did not create was removed: %v", err)
	}
}

func TestSyncOneTakesTheWorktreeSpelledAnotherWay(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.WriteFile(t, filepath.Join(main, "CLAUDE.local.md"), "personal notes\n")
	linked := testutil.AddWorktree(t, main, "feature")

	// git records the path resolved, so a text comparison turns away the very
	// worktree the caller stands in.
	result, err := SyncOne(open(t, main), []string{"CLAUDE.local.md"}, linked+string(filepath.Separator))
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(result.Linked, []string{"CLAUDE.local.md"}) {
		t.Fatalf("got %+v, want the path linked", result)
	}
}

func TestSyncOneMissingPathIsNotAWorktree(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.WriteFile(t, filepath.Join(main, "CLAUDE.local.md"), "personal notes\n")

	_, err := SyncOne(open(t, main), []string{"CLAUDE.local.md"}, filepath.Join(t.TempDir(), "gone"))

	if err == nil {
		t.Fatal("a path that is not there was accepted as a worktree")
	}
	// Go's own words for the stat would name a call the user never made.
	if strings.Contains(err.Error(), "stat ") {
		t.Errorf("error %q is Go's rather than holt's", err)
	}
}

func TestSyncRefusesAWorktreePathHoldingAnotherRepository(t *testing.T) {
	main := testutil.NewRepo(t)
	repo := open(t, main)
	testutil.WriteFile(t, filepath.Join(main, "CLAUDE.local.md"), "notes\n")
	linked := testutil.AddWorktree(t, main, "feature")
	if err := os.RemoveAll(linked); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(linked, 0o755); err != nil {
		t.Fatal(err)
	}
	testutil.Git(t, linked, "init", "--quiet", ".")

	results, err := Sync(repo, []string{"CLAUDE.local.md"})
	if err != nil {
		t.Fatal(err)
	}

	// Written into, holt leaves a symlink in somebody else's project, untracked in
	// their "git status" and committable there.
	if len(results) != 1 || !errors.Is(results[0].Err, ErrForeignWorktree) {
		t.Fatalf("got %+v, want the foreign repository refused", results)
	}
	if _, statErr := os.Lstat(filepath.Join(linked, "CLAUDE.local.md")); !os.IsNotExist(statErr) {
		t.Errorf("holt wrote into a repository of somebody else's: %v", statErr)
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
	t.Cleanup(func() { _ = os.Chmod(unreadable, 0o755) })

	results, err := Sync(open(t, main), []string{"CLAUDE.local.md"})
	if err != nil {
		t.Fatalf("one unreadable worktree failed the whole repair: %v", err)
	}

	if _, err := os.Readlink(filepath.Join(healthy, "CLAUDE.local.md")); err != nil {
		t.Errorf("the healthy worktree was not mirrored: %v", err)
	}
	for _, result := range results {
		if result.Worktree.Path != unreadable {
			continue
		}
		if result.Err == nil {
			t.Error("the unreadable worktree was reported as mirrored")
		}
	}
}

func TestLinkKeepsRelativeDanglingLink(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.WriteFile(t, filepath.Join(main, "CLAUDE.local.md"), "from the main checkout\n")
	linked := testutil.AddWorktree(t, main, "feature")
	// The user's own link with its file momentarily absent: the tail alone would hand
	// it to holt to replace, and holt's own links are never relative.
	if err := os.Symlink(filepath.Join("..", "elsewhere", "CLAUDE.local.md"),
		filepath.Join(linked, "CLAUDE.local.md")); err != nil {
		t.Fatal(err)
	}

	result, err := link(main, linked, []string{"CLAUDE.local.md"})
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(result.Blocked, []string{"CLAUDE.local.md"}) {
		t.Fatalf("got %+v, want the path left alone", result)
	}
	target, err := os.Readlink(filepath.Join(linked, "CLAUDE.local.md"))
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join("..", "elsewhere", "CLAUDE.local.md"); target != want {
		t.Fatalf("symlink now points at %q, want %q", target, want)
	}
}

func TestLinkPathInsideMirroredDirectory(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.WriteFile(t, filepath.Join(main, ".claude", "settings.local.json"), "{}\n")
	linked := testutil.AddWorktree(t, main, "feature")
	// A list naming a directory and a path inside it, which lands under holt's own
	// directory symlink and is already reachable.
	patterns := []string{".claude", ".claude/settings.local.json"}

	result, err := link(main, linked, patterns)
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Blocked) != 0 {
		t.Errorf("holt calls %v blocked, want nothing in the way of a path it already carries", result.Blocked)
	}
	content, err := os.ReadFile(filepath.Join(linked, ".claude", "settings.local.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "{}\n" {
		t.Fatalf("the mirrored file reads %q", content)
	}

	check, err := Inspect(main, linked, patterns)
	if err != nil {
		t.Fatal(err)
	}
	// doctor reads this, and a warning here would never clear.
	if len(check.Blocked) != 0 {
		t.Errorf("doctor sees %v blocked, want the path counted as carried", check.Blocked)
	}
}

func TestLinkKeepsSymlinkWithUnreadableTarget(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.WriteFile(t, filepath.Join(main, "CLAUDE.local.md"), "from the main checkout\n")
	linked := testutil.AddWorktree(t, main, "feature")
	// The user's own link whose file sits behind a directory holt may not open:
	// reading that as "the target is gone" would hand a deliberate link over.
	vault := filepath.Join(t.TempDir(), "vault")
	if err := os.Mkdir(vault, 0o755); err != nil {
		t.Fatal(err)
	}
	own := filepath.Join(vault, "CLAUDE.local.md")
	testutil.WriteFile(t, own, "written by hand\n")
	if err := os.Symlink(own, filepath.Join(linked, "CLAUDE.local.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(vault, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(vault, 0o755) })

	if _, err := link(main, linked, []string{"CLAUDE.local.md"}); err != nil {
		t.Fatal(err)
	}

	target, err := os.Readlink(filepath.Join(linked, "CLAUDE.local.md"))
	if err != nil {
		t.Fatal(err)
	}
	if target != own {
		t.Fatalf("the link now points at %q, want the user's %q", target, own)
	}
}

func TestLinkDirectoryAddedAfterPathInsideIt(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.WriteFile(t, filepath.Join(main, ".claude", "settings.local.json"), "{}\n")
	linked := testutil.AddWorktree(t, main, "feature")
	// The order a list ends in when a file is mirrored first and its directory later:
	// the inner link makes a real directory the second pattern then has to take over.
	patterns := []string{".claude/settings.local.json", ".claude"}

	result, err := link(main, linked, patterns)
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Blocked) != 0 {
		t.Errorf("holt calls %v blocked, want nothing in the way of a directory it makes itself", result.Blocked)
	}
	// The worktree ends with one link, the directory's, and the given order would
	// report the inner path linked over a link the directory took away.
	if want := []string{".claude"}; !reflect.DeepEqual(result.Linked, want) {
		t.Errorf("got %v, want %v", result.Linked, want)
	}
	// The point of mirroring the directory: whatever lands in it later arrives.
	testutil.WriteFile(t, filepath.Join(main, ".claude", "skills", "x.md"), "skill\n")
	content, err := os.ReadFile(filepath.Join(linked, ".claude", "skills", "x.md"))
	if err != nil {
		t.Fatalf("a file added to the mirrored directory never reached the worktree: %v", err)
	}
	if string(content) != "skill\n" {
		t.Fatalf("the file reads %q", content)
	}
}

func TestLinkFileWhereParentDirectoryBelongs(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.WriteFile(t, filepath.Join(main, "config", "local.env"), "SECRET=1\n")
	linked := testutil.AddWorktree(t, main, "feature")
	// A real file where the mirrored path needs a directory, which a branch tracking
	// config as a file leaves; reading it as a broken worktree abandons the rest.
	testutil.WriteFile(t, filepath.Join(linked, "config"), "old single-file config\n")
	testutil.WriteFile(t, filepath.Join(main, "CLAUDE.local.md"), "notes\n")
	patterns := []string{"config/local.env", "CLAUDE.local.md"}

	result, err := link(main, linked, patterns)
	if err != nil {
		t.Fatalf("holt gave up on the whole worktree: %v", err)
	}

	if !slices.Contains(result.Blocked, "config/local.env") {
		t.Errorf("got %+v, want the path reported as occupied", result)
	}
	if !slices.Contains(result.Linked, "CLAUDE.local.md") {
		t.Errorf("got %+v, want the healthy path mirrored anyway", result)
	}

	check, err := Inspect(main, linked, patterns)
	if err != nil {
		t.Fatalf("doctor gave up on the whole worktree: %v", err)
	}
	if !slices.Contains(check.Blocked, "config/local.env") {
		t.Errorf("doctor sees %+v, want the path reported as occupied", check)
	}
}

func TestLinkReportsOverlappingPatternsOnce(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.WriteFile(t, filepath.Join(main, "CLAUDE.local.md"), "notes\n")
	linked := testutil.AddWorktree(t, main, "feature")
	// A file of the user's own, so the path is reported rather than linked.
	testutil.WriteFile(t, filepath.Join(linked, "CLAUDE.local.md"), "mine\n")

	result, err := link(main, linked, []string{"CLAUDE.*.md", "CLAUDE.local.md"})
	if err != nil {
		t.Fatal(err)
	}

	// Without this the hook would name the path once per pattern that reached it.
	if len(result.Blocked) != 1 {
		t.Fatalf("got %v, want the path named once", result.Blocked)
	}
}

func TestLinkSkipsGitDirectoryReachedByGlob(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.WriteFile(t, filepath.Join(main, ".env"), "SECRET=1\n")
	linked := testutil.AddWorktree(t, main, "feature")
	// A pattern sweeping up every dotfile reaches .git, which every worktree keeps
	// its own of, so linking it is never possible.
	result, err := link(main, linked, []string{".*"})
	if err != nil {
		t.Fatal(err)
	}

	if slices.Contains(result.Blocked, ".git") {
		t.Errorf("got %+v, want .git passed over rather than reported forever", result)
	}
	if !slices.Contains(result.Linked, ".env") {
		t.Errorf("got %+v, want the rest of the glob mirrored", result)
	}
}

func TestLinkKeepsDanglingLinkAimedElsewhere(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.WriteFile(t, filepath.Join(main, "CLAUDE.local.md"), "from the main checkout\n")
	linked := testutil.AddWorktree(t, main, "feature")
	// Absolute and dangling like holt's stranded links, but with another tail, which
	// is all that tells the two apart.
	own := filepath.Join(t.TempDir(), "gone", "notes-of-my-own.md")
	if err := os.Symlink(own, filepath.Join(linked, "CLAUDE.local.md")); err != nil {
		t.Fatal(err)
	}

	result, err := link(main, linked, []string{"CLAUDE.local.md"})
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(result.Blocked, []string{"CLAUDE.local.md"}) {
		t.Fatalf("got %+v, want the path left alone", result)
	}
	target, err := os.Readlink(filepath.Join(linked, "CLAUDE.local.md"))
	if err != nil {
		t.Fatal(err)
	}
	if target != own {
		t.Fatalf("the symlink now points at %q, want %q", target, own)
	}
}

func TestLinkLeavesItsOwnLinkAlone(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.WriteFile(t, filepath.Join(main, "CLAUDE.local.md"), "notes\n")
	linked := testutil.AddWorktree(t, main, "feature")
	if _, err := link(main, linked, []string{"CLAUDE.local.md"}); err != nil {
		t.Fatal(err)
	}

	// The hook runs on every checkout, so a link already pointing right is ordinary.
	result, err := link(main, linked, []string{"CLAUDE.local.md"})
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Blocked) != 0 {
		t.Fatalf("got %+v, want holt's own link recognised", result)
	}
	if !slices.Contains(result.Linked, "CLAUDE.local.md") {
		t.Errorf("got %+v, want the path counted as linked", result)
	}
}

func TestLinkTellsUnreadableFromGone(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads a directory whatever its mode says")
	}
	main := testutil.NewRepo(t)
	testutil.WriteFile(t, filepath.Join(main, "CLAUDE.local.md"), "notes\n")
	// Closing the parent is what makes holt's own look fail: a worktree at 000 still stats.
	parent := filepath.Join(t.TempDir(), "closed")
	worktreePath := filepath.Join(parent, "feature")
	testutil.Git(t, main, "worktree", "add", "--quiet", "-b", "feature", worktreePath)
	if err := os.Chmod(parent, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(parent, 0o755) })

	_, err := link(main, worktreePath, []string{"CLAUDE.local.md"})

	if err == nil {
		t.Fatal("a worktree holt cannot look into was mirrored anyway")
	}
	// Called gone, holt sends the user to prune a worktree still in place.
	if errors.Is(err, ErrWorktreeGone) {
		t.Errorf("error %q calls an unreadable worktree gone", err)
	}
}

func TestLinkOwnsItsLinkUnderAnotherSpelling(t *testing.T) {
	main := testutil.NewRepo(t)
	repo := open(t, main)
	if ignoreCase, _, err := repo.Config("core.ignorecase"); err != nil || ignoreCase != "true" {
		t.Skip("this filesystem tells the two spellings apart")
	}
	testutil.WriteFile(t, filepath.Join(main, "notes.txt"), "secret\n")
	worktree := testutil.AddWorktree(t, main, "feature")

	// One file under two spellings: read as text the targets differ, and holt calls
	// its own link a stranger's on every checkout, with no sync able to settle it.
	if _, err := Sync(repo, []string{"Notes.txt"}); err != nil {
		t.Fatal(err)
	}
	results, err := Sync(repo, []string{"Notes.txt", "notes.txt"})
	if err != nil {
		t.Fatal(err)
	}

	for _, result := range results {
		if result.Worktree.Path != worktree {
			continue
		}
		if len(result.Result.Blocked) > 0 {
			t.Errorf("holt reports its own link blocked: %v", result.Result.Blocked)
		}
	}

	// Inspect is the other reader of the same link, and doctor goes by it.
	check, err := Inspect(main, worktree, []string{"Notes.txt", "notes.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if len(check.Diverted) > 0 {
		t.Errorf("doctor would call holt's own link one pointing elsewhere: %v", check.Diverted)
	}
}

func TestLinkReadsRelativeTargetFromTheLinkDirectory(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.WriteFile(t, filepath.Join(main, "CLAUDE.local.md"), "from the main checkout\n")
	linked := testutil.AddWorktree(t, main, "feature")
	// A link pointing at itself, which resolves onto the mirrored file if read from
	// holt's working directory rather than the one holding it.
	if err := os.Remove(filepath.Join(linked, "CLAUDE.local.md")); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if err := os.Symlink("CLAUDE.local.md", filepath.Join(linked, "CLAUDE.local.md")); err != nil {
		t.Fatal(err)
	}
	t.Chdir(main)

	result, err := link(main, linked, []string{"CLAUDE.local.md"})
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(result.Blocked, []string{"CLAUDE.local.md"}) {
		t.Fatalf("got %+v, want the broken link reported rather than claimed", result)
	}
}

func TestUnlinkRemovesWhatInspectCallsMirrored(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.WriteFile(t, filepath.Join(main, "CLAUDE.local.md"), "from the main checkout\n")
	linked := testutil.AddWorktree(t, main, "feature")
	// The main checkout under another name: Inspect goes by the file landed on and
	// calls it holt's, so unlinking has to agree or the path is left behind.
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(main, alias); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(linked, "CLAUDE.local.md")
	if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(alias, "CLAUDE.local.md"), target); err != nil {
		t.Fatal(err)
	}

	check, err := Inspect(main, linked, []string{"CLAUDE.local.md"})
	if err != nil {
		t.Fatal(err)
	}
	if len(check.Linked) != 1 {
		t.Fatalf("Inspect reports %+v, so the fixture no longer builds a link holt claims", check)
	}

	removed, err := unlink(main, linked, []string{"CLAUDE.local.md"})
	if err != nil {
		t.Fatal(err)
	}

	if len(removed) != 1 {
		t.Errorf("unlink removed %v, want the link Inspect called holt's own", removed)
	}
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		t.Error("the link holt reported as mirrored is still there")
	}
}

func TestUnlinkKeepsLinkToWhatTheMainCheckoutOnlyPointsAt(t *testing.T) {
	main := testutil.NewRepo(t)
	// The main checkout's copy is itself a link into a dotfiles directory, and the
	// user aims the worktree straight at the dotfile.
	dotfiles := t.TempDir()
	shared := filepath.Join(dotfiles, "CLAUDE.local.md")
	testutil.WriteFile(t, shared, "shared\n")
	if err := os.Remove(filepath.Join(main, "CLAUDE.local.md")); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if err := os.Symlink(shared, filepath.Join(main, "CLAUDE.local.md")); err != nil {
		t.Fatal(err)
	}
	linked := testutil.AddWorktree(t, main, "feature")
	target := filepath.Join(linked, "CLAUDE.local.md")
	if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if err := os.Symlink(shared, target); err != nil {
		t.Fatal(err)
	}

	// Followed all the way both reach the dotfile, but what is holt's is the link it
	// writes, not whatever that link finally lands on.
	check, err := Inspect(main, linked, []string{"CLAUDE.local.md"})
	if err != nil {
		t.Fatal(err)
	}
	if len(check.Linked) > 0 {
		t.Errorf("holt claims a link it did not write: %+v", check)
	}

	removed, err := unlink(main, linked, []string{"CLAUDE.local.md"})
	if err != nil {
		t.Fatal(err)
	}

	if len(removed) > 0 {
		t.Errorf("unlink removed %v, which holt never wrote", removed)
	}
	if _, err := os.Lstat(target); err != nil {
		t.Errorf("the user's own link is gone: %v", err)
	}
}

func TestUnsyncCarriesOnPastAWorktreeItCannotWrite(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes a directory whatever its mode says")
	}
	main := testutil.NewRepo(t)
	repo := open(t, main)
	testutil.WriteFile(t, filepath.Join(main, "CLAUDE.local.md"), "notes\n")
	blocked := testutil.AddWorktree(t, main, "blocked")
	reachable := testutil.AddWorktree(t, main, "reachable")
	if _, err := Sync(repo, []string{"CLAUDE.local.md"}); err != nil {
		t.Fatal(err)
	}
	// The list is saved before unmirroring, so a worktree skipped here keeps a link
	// no later "holt mirror rm" will offer to take.
	if err := os.Chmod(blocked, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(blocked, 0o755) })

	links, worktrees, err := Unsync(repo, []string{"CLAUDE.local.md"})

	if err == nil {
		t.Fatal("a worktree holt could not write was reported as unmirrored")
	}
	// The counts are the only record: the list is saved already, so a link left out
	// of them is one no later command mentions.
	if links != 1 || worktrees != 1 {
		t.Errorf("got %d links from %d worktrees, want the one that worked counted", links, worktrees)
	}
	if _, statErr := os.Lstat(filepath.Join(reachable, "CLAUDE.local.md")); !os.IsNotExist(statErr) {
		t.Error("one worktree holt could not write cost the removal from the rest")
	}
}

func TestUnsyncTakesBackTheDirectoriesItMade(t *testing.T) {
	main := testutil.NewRepo(t)
	repo := open(t, main)
	testutil.WriteFile(t, filepath.Join(main, ".claude", "deep", "settings.local.json"), "{}\n")
	testutil.WriteFile(t, filepath.Join(main, ".claude", "keep.local"), "K\n")
	linked := testutil.AddWorktree(t, main, "feature")
	if _, err := Sync(repo, []string{".claude/deep/settings.local.json", ".claude/keep.local"}); err != nil {
		t.Fatal(err)
	}

	if _, _, err := Unsync(repo, []string{".claude/deep/settings.local.json"}); err != nil {
		t.Fatal(err)
	}

	// Left behind it sits in every worktree, holt's own doing, with no command
	// listing it or taking it back.
	if _, err := os.Lstat(filepath.Join(linked, ".claude", "deep")); !os.IsNotExist(err) {
		t.Errorf("the emptied directory is still there: %v", err)
	}
	// Its parent still holds the other link, so it is not holt's to take.
	if _, err := os.Lstat(filepath.Join(linked, ".claude", "keep.local")); err != nil {
		t.Errorf("pruning went past the directory that was still in use: %v", err)
	}
}

func TestUnsyncLeavesADirectoryHoldingSomethingOfTheUsers(t *testing.T) {
	main := testutil.NewRepo(t)
	repo := open(t, main)
	testutil.WriteFile(t, filepath.Join(main, ".claude", "settings.local.json"), "{}\n")
	linked := testutil.AddWorktree(t, main, "feature")
	if _, err := Sync(repo, []string{".claude/settings.local.json"}); err != nil {
		t.Fatal(err)
	}
	scratch := filepath.Join(linked, ".claude", "scratch.txt")
	testutil.WriteFile(t, scratch, "mine\n")

	if _, _, err := Unsync(repo, []string{".claude/settings.local.json"}); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Lstat(scratch); err != nil {
		t.Errorf("a file holt did not put there went with the directory: %v", err)
	}
}

func TestUnsyncReportsAWorktreeItCannotRead(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads a directory whatever its mode says")
	}
	main := testutil.NewRepo(t)
	repo := open(t, main)
	testutil.WriteFile(t, filepath.Join(main, "CLAUDE.local.md"), "notes\n")
	unreadable := testutil.AddWorktree(t, main, "unreadable")
	reachable := testutil.AddWorktree(t, main, "reachable")
	if _, err := Sync(repo, []string{"CLAUDE.local.md"}); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(unreadable, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(unreadable, 0o755) })

	links, worktrees, err := Unsync(repo, []string{"CLAUDE.local.md"})

	// Glob says "nothing matched, nothing wrong", so the worktree reads as one that
	// held no link, while info/exclude is already rewritten around it.
	if err == nil {
		t.Fatal("a worktree holt could not read was reported as unmirrored")
	}
	if links != 1 || worktrees != 1 {
		t.Errorf("got %d links from %d worktrees, want the one that worked counted", links, worktrees)
	}
	if _, statErr := os.Lstat(filepath.Join(reachable, "CLAUDE.local.md")); !os.IsNotExist(statErr) {
		t.Error("one worktree holt could not read cost the removal from the rest")
	}
}

func TestUnsyncCountsWhatCameAwayFromAPartlyBlockedWorktree(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes a directory whatever its mode says")
	}
	main := testutil.NewRepo(t)
	repo := open(t, main)
	testutil.WriteFile(t, filepath.Join(main, "a", "local.md"), "A\n")
	testutil.WriteFile(t, filepath.Join(main, "b", "local.md"), "B\n")
	linked := testutil.AddWorktree(t, main, "feature")
	if _, err := Sync(repo, []string{"*/local.md"}); err != nil {
		t.Fatal(err)
	}
	// One removal fails after the other already happened, and counting neither
	// understates what can no longer be undone.
	if err := os.Chmod(filepath.Join(linked, "b"), 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(linked, "b"), 0o755) })

	links, worktrees, err := Unsync(repo, []string{"*/local.md"})

	if err == nil {
		t.Fatal("a link holt could not remove was reported as unmirrored")
	}
	if links != 1 || worktrees != 1 {
		t.Errorf("got %d links from %d worktrees, want the one that did come away counted", links, worktrees)
	}
}
