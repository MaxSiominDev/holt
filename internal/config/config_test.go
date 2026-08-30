package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestMergeListWithoutAFile(t *testing.T) {
	at(t, t.TempDir())

	list, err := LoadMergeList()
	if err != nil {
		t.Fatal(err)
	}

	// holt as it arrives: every conflict is the user's until the file names a file.
	if patterns := list.Patterns(); len(patterns) > 0 {
		t.Errorf("got %v, want nothing listed", patterns)
	}
	if list.Matches("CHANGELOG.md") {
		t.Error("a list with no file in it matched one")
	}
}

func TestMergeListReadsPatterns(t *testing.T) {
	list := write(t, "# what holt merges itself\n\nCHANGELOG.md\n  docs/*.md  \nCHANGELOG.md\n")

	if got := list.Patterns(); !slices.Equal(got, []string{"CHANGELOG.md", "docs/*.md"}) {
		t.Errorf("got %v, want the comment and the blank line left out and the repeat listed once", got)
	}
	if rejected := list.Rejected(); len(rejected) > 0 {
		t.Errorf("got %v, want nothing rejected", rejected)
	}
}

func TestMergeListMatchesAsGitNamesFiles(t *testing.T) {
	list := write(t, "CHANGELOG.md\ndocs/*.md\n")

	for _, file := range []string{"CHANGELOG.md", "docs/design.md"} {
		if !list.Matches(file) {
			t.Errorf("%s is listed and did not match", file)
		}
	}
	// A star stops at a separator, so a pattern covers the directory it names and
	// nothing under it, and an unlisted file is not merged because it looks alike.
	for _, file := range []string{"docs/old/design.md", "sub/CHANGELOG.md", "CHANGELOG.md.bak"} {
		if list.Matches(file) {
			t.Errorf("%s is not listed and matched", file)
		}
	}
}

func TestMergeListRejectsWhatItCannotUse(t *testing.T) {
	list := write(t, "CHANGELOG.md\nnotes.txt\n/etc/passwd\n../outside.md\ndocs/**/*.md\nbad[.md\n")

	// The readable line survives its neighbours: a hand written file is not thrown
	// out whole over one bad line.
	if got := list.Patterns(); !slices.Equal(got, []string{"CHANGELOG.md"}) {
		t.Errorf("got %v, want the one line holt can use", got)
	}
	rejected := list.Rejected()
	if len(rejected) != 5 {
		t.Fatalf("got %d rejected lines, want one for each of the five: %v", len(rejected), rejected)
	}
	for index, want := range []string{"line 2", "line 3", "line 4", "line 5", "line 6"} {
		if !strings.HasPrefix(rejected[index], want) {
			t.Errorf("%q does not start with %q, so it does not say where to look", rejected[index], want)
		}
	}
	if !strings.Contains(rejected[0], ".md") {
		t.Errorf("%q does not say why a .txt is not merged", rejected[0])
	}
}

func TestMergeListIgnoresAByteOrderMark(t *testing.T) {
	list := write(t, "\ufeff# holt's own file\nCHANGELOG.md\n")

	if got := list.Patterns(); !slices.Equal(got, []string{"CHANGELOG.md"}) {
		t.Errorf("got %v, want the mark taken off the comment it hides in", got)
	}
}

func TestMergeListPathFallsBackToHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// The specification calls a relative value invalid, and following one would
	// have holt read a different file from every directory it runs in.
	t.Setenv("XDG_CONFIG_HOME", "relative/config")

	list, err := LoadMergeList()
	if err != nil {
		t.Fatal(err)
	}

	if want := filepath.Join(home, ".config", "holt", "merge.list"); list.Path() != want {
		t.Errorf("got %s, want %s", list.Path(), want)
	}
}

func write(t *testing.T, content string) *MergeList {
	t.Helper()
	dir := t.TempDir()
	at(t, dir)
	if err := os.MkdirAll(filepath.Join(dir, "holt"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "holt", "merge.list"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	list, err := LoadMergeList()
	if err != nil {
		t.Fatal(err)
	}
	return list
}

func at(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", dir)
}
