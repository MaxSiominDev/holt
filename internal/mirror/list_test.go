package mirror

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/MaxSiominDev/holt/internal/git"
	"github.com/MaxSiominDev/holt/internal/testutil"
)

func TestListSaveAndReload(t *testing.T) {
	repo := openRepo(t, testutil.NewRepo(t))
	list, err := LoadList(repo)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"CLAUDE.local.md", ".claude/settings.local.json"} {
		if _, err := list.Add(path); err != nil {
			t.Fatal(err)
		}
	}
	if err := list.Save(); err != nil {
		t.Fatal(err)
	}

	reloaded, err := LoadList(repo)
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"CLAUDE.local.md", ".claude/settings.local.json"}
	if got := reloaded.Paths(); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestListSharedAcrossWorktrees(t *testing.T) {
	main := testutil.NewRepo(t)
	linked := testutil.AddWorktree(t, main, "feature")

	list, err := LoadList(openRepo(t, main))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := list.Add("CLAUDE.local.md"); err != nil {
		t.Fatal(err)
	}
	if err := list.Save(); err != nil {
		t.Fatal(err)
	}

	// A worktree has its own git directory; a list in the wrong one is invisible here.
	fromWorktree, err := LoadList(openRepo(t, linked))
	if err != nil {
		t.Fatal(err)
	}
	if got := fromWorktree.Paths(); !reflect.DeepEqual(got, []string{"CLAUDE.local.md"}) {
		t.Fatalf("got %v from the linked worktree, want the list written in the main checkout", got)
	}
}

func TestLoadListWithoutFile(t *testing.T) {
	list, err := LoadList(openRepo(t, testutil.NewRepo(t)))
	if err != nil {
		t.Fatal(err)
	}

	if got := list.Paths(); len(got) != 0 {
		t.Fatalf("got %v, want no paths", got)
	}
}

func TestAddReportsChange(t *testing.T) {
	list := &List{}

	first, err := list.Add("CLAUDE.local.md")
	if err != nil {
		t.Fatal(err)
	}
	second, err := list.Add("./CLAUDE.local.md")
	if err != nil {
		t.Fatal(err)
	}

	if !first {
		t.Error("the first add did not report a change")
	}
	if second {
		t.Error("the same path in another spelling was added twice")
	}
}

func TestRemoveReportsChange(t *testing.T) {
	list := &List{}
	if _, err := list.Add("CLAUDE.local.md"); err != nil {
		t.Fatal(err)
	}

	removed, err := list.Remove("CLAUDE.local.md")
	if err != nil {
		t.Fatal(err)
	}
	again, err := list.Remove("CLAUDE.local.md")
	if err != nil {
		t.Fatal(err)
	}

	if !removed {
		t.Error("removing a listed path did not report a change")
	}
	if again {
		t.Error("removing an absent path reported a change")
	}
}

func TestSaveOutsideWorkTree(t *testing.T) {
	main := testutil.NewRepo(t)
	list, err := LoadList(openRepo(t, main))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := list.Add("CLAUDE.local.md"); err != nil {
		t.Fatal(err)
	}
	if err := list.Save(); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(main, ".git", "holt", "mirror.list")); err != nil {
		t.Fatal(err)
	}
	// The list lives in the git directory, out of the project.
	if status := testutil.Git(t, main, "status", "--porcelain"); status != "" {
		t.Fatalf("the mirror list showed up in git status:\n%s", status)
	}
}

func TestCleanRelPath(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		want    string
		wantErr bool
	}{
		{name: "plain file", path: "CLAUDE.local.md", want: "CLAUDE.local.md"},
		{name: "nested file", path: ".claude/settings.local.json", want: ".claude/settings.local.json"},
		{name: "glob is kept intact", path: ".claude/skills/*-local", want: ".claude/skills/*-local"},
		{name: "leading dot slash is normalised", path: "./CLAUDE.local.md", want: "CLAUDE.local.md"},
		{name: "surrounding spaces are trimmed", path: "  CLAUDE.local.md  ", want: "CLAUDE.local.md"},
		{name: "inner traversal is normalised", path: ".claude/../CLAUDE.local.md", want: "CLAUDE.local.md"},
		{name: "absolute path is rejected", path: "/etc/passwd", wantErr: true},
		{name: "escaping the repository is rejected", path: "../secrets", wantErr: true},
		{name: "the root itself is rejected", path: ".", wantErr: true},
		{name: "empty is rejected", path: "   ", wantErr: true},
		{name: "malformed glob is rejected", path: ".claude/[unclosed", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := CleanPath(test.path)

			if test.wantErr {
				if err == nil {
					t.Fatalf("got %q, want an error", got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("got %q, want %q", got, test.want)
			}
		})
	}
}

func TestParseListSkipsComments(t *testing.T) {
	content := strings.Join([]string{
		"# Paths mirrored into every worktree",
		"",
		"CLAUDE.local.md",
		"   ",
		"  .claude/settings.local.json  ",
		"# trailing note",
	}, "\n")

	got, err := parseList(content)
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"CLAUDE.local.md", ".claude/settings.local.json"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestLoadListRejectsBadEntry(t *testing.T) {
	main := testutil.NewRepo(t)
	// Hand-edited, so the bad entry has to be caught on load.
	testutil.WriteFile(t, filepath.Join(main, ".git", "holt", "mirror.list"), "CLAUDE.local.md\n../outside.md\n")

	_, err := LoadList(openRepo(t, main))

	if err == nil {
		t.Fatal("a path pointing outside the repository was loaded")
	}
	if !strings.Contains(err.Error(), "line 2") {
		t.Errorf("error %q does not say which line is wrong", err)
	}
}

func TestCheckAmbiguous(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.WriteFile(t, filepath.Join(main, "notes[1].md"), "the file the user meant\n")
	testutil.WriteFile(t, filepath.Join(main, "notes1.md"), "what the glob matches instead\n")
	testutil.WriteFile(t, filepath.Join(main, "CLAUDE.local.md"), "plain name\n")

	tests := []struct {
		name    string
		pattern string
		wantErr bool
	}{
		{name: "a glob that also names a file here", pattern: "notes[1].md", wantErr: true},
		{name: "the same name escaped", pattern: `notes\[1\].md`},
		{name: "a glob matching nothing in particular", pattern: "*.local"},
		{name: "a plain name", pattern: "CLAUDE.local.md"},
		{name: "a glob whose literal form is not here", pattern: "logs/*.txt"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := CheckAmbiguous(main, test.pattern)

			if test.wantErr {
				if err == nil {
					t.Fatal("the pattern was accepted, so holt would mirror the other file")
				}
				// Refusing is only useful with the way out attached.
				if !strings.Contains(err.Error(), `notes\[1\].md`) {
					t.Errorf("error %q does not show the escaped form to use", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("got %v, want the pattern accepted", err)
			}
		})
	}
}

func TestAddRejectsMalformedGlob(t *testing.T) {
	list := &List{}

	added, err := list.Add(".claude/[unclosed")

	if err == nil {
		t.Fatal("a pattern that filepath.Glob cannot parse was accepted")
	}
	if added {
		t.Error("the malformed pattern was recorded anyway")
	}
}

func openRepo(t *testing.T, dir string) *git.Repo {
	t.Helper()
	repo, err := git.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	return repo
}
