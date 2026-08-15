package mirror

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MaxSiominDev/holt/internal/testutil"
)

func TestWriteExcludesHidesSymlinks(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.WriteFile(t, filepath.Join(main, ".claude", "skills", "cdc-local", "SKILL.md"), "skill\n")
	worktreePath := testutil.AddWorktree(t, main, "feature")
	repo := openRepo(t, main)

	if err := WriteExcludes(repo, []string{".claude/skills/*-local"}); err != nil {
		t.Fatal(err)
	}
	if _, err := link(main, worktreePath, []string{".claude/skills/*-local"}); err != nil {
		t.Fatal(err)
	}

	// git shares info/exclude, so one write in the main checkout covers all.
	if status := testutil.Git(t, worktreePath, "status", "--porcelain"); status != "" {
		t.Fatalf("the mirrored symlink is visible to git:\n%s", status)
	}
}

func TestWriteExcludesKeepsLines(t *testing.T) {
	main := testutil.NewRepo(t)
	exclude := filepath.Join(main, ".git", "info", "exclude")
	testutil.WriteFile(t, exclude, "# written by hand\nscratch.txt\n")
	repo := openRepo(t, main)

	if err := WriteExcludes(repo, []string{"CLAUDE.local.md"}); err != nil {
		t.Fatal(err)
	}

	content, err := os.ReadFile(exclude)
	if err != nil {
		t.Fatal(err)
	}
	want := "# written by hand\nscratch.txt\n" + excludeBegin + "\n/CLAUDE.local.md\n" + excludeEnd + "\n"
	if string(content) != want {
		t.Fatalf("got:\n%s\nwant:\n%s", content, want)
	}
}

func TestWriteExcludesRewritesBlock(t *testing.T) {
	main := testutil.NewRepo(t)
	exclude := filepath.Join(main, ".git", "info", "exclude")
	// git seeds the file with a comment header of its own.
	seeded, err := os.ReadFile(exclude)
	if err != nil {
		t.Fatal(err)
	}
	repo := openRepo(t, main)

	if err := WriteExcludes(repo, []string{"CLAUDE.local.md"}); err != nil {
		t.Fatal(err)
	}
	if err := WriteExcludes(repo, []string{"CLAUDE.local.md", ".claude/settings.local.json"}); err != nil {
		t.Fatal(err)
	}

	content, err := os.ReadFile(exclude)
	if err != nil {
		t.Fatal(err)
	}
	want := string(seeded) + excludeBegin + "\n/CLAUDE.local.md\n/.claude/settings.local.json\n" + excludeEnd + "\n"
	if string(content) != want {
		t.Fatalf("got:\n%s\nwant:\n%s", content, want)
	}
	if count := strings.Count(string(content), excludeBegin); count != 1 {
		t.Fatalf("the block appears %d times, want exactly one", count)
	}
}

func TestWriteExcludesDropsBlock(t *testing.T) {
	main := testutil.NewRepo(t)
	exclude := filepath.Join(main, ".git", "info", "exclude")
	testutil.WriteFile(t, exclude, "scratch.txt\n")
	repo := openRepo(t, main)

	if err := WriteExcludes(repo, []string{"CLAUDE.local.md"}); err != nil {
		t.Fatal(err)
	}
	if err := WriteExcludes(repo, nil); err != nil {
		t.Fatal(err)
	}

	content, err := os.ReadFile(exclude)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "scratch.txt\n" {
		t.Fatalf("got %q, want the hand-written line on its own", content)
	}
}

func TestWriteExcludesAnchored(t *testing.T) {
	main := testutil.NewRepo(t)
	repo := openRepo(t, main)
	if err := WriteExcludes(repo, []string{"CLAUDE.local.md"}); err != nil {
		t.Fatal(err)
	}

	// An unanchored pattern would hide this unrelated file too.
	testutil.WriteFile(t, filepath.Join(main, "sub", "CLAUDE.local.md"), "someone else's file\n")

	// Without --untracked-files=all the new directory collapses into one entry.
	status := testutil.Git(t, main, "status", "--porcelain", "--untracked-files=all")
	if !strings.Contains(status, filepath.Join("sub", "CLAUDE.local.md")) {
		t.Fatalf("git status is %q, want the nested file still visible", status)
	}
}

func TestWriteExcludesNoEndMarker(t *testing.T) {
	main := testutil.NewRepo(t)
	exclude := filepath.Join(main, ".git", "info", "exclude")
	testutil.WriteFile(t, exclude, excludeBegin+"\n/CLAUDE.local.md\nkept-by-hand.txt\n")

	if err := WriteExcludes(openRepo(t, main), []string{".claude/settings.local.json"}); err != nil {
		t.Fatal(err)
	}

	content, err := os.ReadFile(exclude)
	if err != nil {
		t.Fatal(err)
	}
	// With no closing marker holt gives up the begin line alone and keeps the rest.
	if !strings.Contains(string(content), "kept-by-hand.txt") {
		t.Fatalf("a hand-written line was swallowed, the file now holds:\n%s", content)
	}
}

func TestHasExcludeBlock(t *testing.T) {
	repo := openRepo(t, testutil.NewRepo(t))

	before, err := HasExcludeBlock(repo)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteExcludes(repo, []string{"CLAUDE.local.md"}); err != nil {
		t.Fatal(err)
	}
	after, err := HasExcludeBlock(repo)
	if err != nil {
		t.Fatal(err)
	}

	if before {
		t.Error("an untouched repository reported holt's block as present")
	}
	if !after {
		t.Error("the block was not found after being written")
	}
}
