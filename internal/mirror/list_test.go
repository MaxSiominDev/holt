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
	repo := open(t, testutil.NewRepo(t))
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

	list, err := LoadList(open(t, main))
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
	fromWorktree, err := LoadList(open(t, linked))
	if err != nil {
		t.Fatal(err)
	}
	if got := fromWorktree.Paths(); !reflect.DeepEqual(got, []string{"CLAUDE.local.md"}) {
		t.Fatalf("got %v from the linked worktree, want the list written in the main checkout", got)
	}
}

func TestLoadListWithoutFile(t *testing.T) {
	list, err := LoadList(open(t, testutil.NewRepo(t)))
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
	list, err := LoadList(open(t, main))
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

func TestCleanPath(t *testing.T) {
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
		// The list is one path per line, and a leading # is a comment there.
		{name: "a line break is rejected", path: "two\nlines.local", wantErr: true},
		{name: "a leading hash is rejected", path: "#notes.local", wantErr: true},
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

	got, rejected := (&List{}).parse(content)

	if len(rejected) != 0 {
		t.Fatalf("got %v rejected, want comments and blanks skipped silently", rejected)
	}
	want := []string{"CLAUDE.local.md", ".claude/settings.local.json"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestLoadListKeepsUsableEntries(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.WriteFile(t, filepath.Join(main, ".git", "holt", "mirror.list"), "CLAUDE.local.md\n../outside.md\n")

	list, err := LoadList(open(t, main))
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(list.Paths(), []string{"CLAUDE.local.md"}) {
		t.Fatalf("got %v, want the readable entry kept", list.Paths())
	}
	rejected := list.Rejected()
	if len(rejected) != 1 {
		t.Fatalf("got %v, want the bad line reported", rejected)
	}
	if !strings.Contains(rejected[0], "line 2") {
		t.Errorf("%q does not say which line is wrong", rejected[0])
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
				// Refusing is useful only with the way out attached, and quoted at
				// that, or the shell eats the backslashes on the way back in.
				if !strings.Contains(err.Error(), `'notes\[1\].md'`) {
					t.Errorf("error %q does not show a spelling that survives being typed again", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("got %v, want the pattern accepted", err)
			}
		})
	}
}

func TestCleanPathRejectsADoubleStar(t *testing.T) {
	patterns := []string{".claude/**/*.local.md", "**/*.local.md", ".claude/skills/**"}

	for _, pattern := range patterns {
		t.Run(pattern, func(t *testing.T) {
			// filepath.Match makes "**" no wider than "*", while the same line in
			// info/exclude reads as any depth, so git hides files holt never mirrors.
			if _, err := CleanPath(pattern); err == nil {
				t.Fatal("the pattern was accepted, so git and holt would disagree about what it covers")
			}
		})
	}
}

func TestCleanPathOnBracketsGitAndHoltReadDifferently(t *testing.T) {
	tests := []struct {
		pattern string
		wantErr bool
	}{
		// Go takes "!" for an ordinary member and git for negation, so git hides
		// what holt did not mirror and leaves the mirrored file showing.
		{pattern: "[!a].md", wantErr: true},
		// A POSIX class is git's alone and filepath.Match reads it letter by letter,
		// so the two reach different names.
		{pattern: "note[[:digit:]].md", wantErr: true},
		{pattern: ".claude/[!x]*.md", wantErr: true},
		// These three agree, so refusing them would take away patterns that work.
		{pattern: "[ab].md"},
		{pattern: "[a-c].md"},
		{pattern: "[^a].md"},
		// Escaped, the brackets are an ordinary name and no class at all.
		{pattern: `lit\[!\].md`},
	}

	for _, test := range tests {
		t.Run(test.pattern, func(t *testing.T) {
			_, err := CleanPath(test.pattern)

			if test.wantErr && err == nil {
				t.Fatal("the pattern was accepted, so git and holt would disagree about what it covers")
			}
			if !test.wantErr && err != nil {
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

func open(t *testing.T, dir string) *git.Repo {
	t.Helper()
	repo, err := git.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	return repo
}

func TestCleanPathRejectsGitDirectory(t *testing.T) {
	// The main checkout has .git as a directory, so the pattern looks healthy when
	// written and expand drops it later without a word. Mixed case too, which a
	// case-insensitive filesystem resolves to the same directory.
	for _, path := range []string{".git", ".git/config", ".GIT/config", ".Git"} {
		_, err := CleanPath(path)

		if err == nil {
			t.Errorf("%s was accepted", path)
			continue
		}
		// Every rejection quotes the path back, so matching ".git" alone would pass
		// off the path itself, at least where it is lower case.
		if !strings.Contains(err.Error(), "keeps its own .git") {
			t.Errorf("%s was refused for another reason: %v", path, err)
		}
	}
}

func TestLoadListSkipsAByteOrderMark(t *testing.T) {
	main := testutil.NewRepo(t)
	repo := open(t, main)
	list, err := LoadList(repo)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := list.Add("CLAUDE.local.md"); err != nil {
		t.Fatal(err)
	}
	if err := list.Save(); err != nil {
		t.Fatal(err)
	}
	commonDir, err := repo.CommonDir()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(commonDir, listFile)
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// What an editor saving as UTF-8 with a signature leaves behind.
	if err := os.WriteFile(path, append([]byte("\ufeff"), content...), 0o644); err != nil {
		t.Fatal(err)
	}

	reloaded, err := LoadList(repo)
	if err != nil {
		t.Fatal(err)
	}

	// holt's own header taken for a path, which "holt mirror ls" then reports missing
	// and doctor warns about.
	if want := []string{"CLAUDE.local.md"}; !reflect.DeepEqual(reloaded.Paths(), want) {
		t.Fatalf("got %v, want %v", reloaded.Paths(), want)
	}
	if len(reloaded.Rejected()) != 0 {
		t.Errorf("got %v, want the marked header passed over in silence", reloaded.Rejected())
	}
}

func TestLoadListSkipsHandWrittenDuplicate(t *testing.T) {
	main := testutil.NewRepo(t)
	repo := open(t, main)
	list, err := LoadList(repo)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := list.Add("CLAUDE.local.md"); err != nil {
		t.Fatal(err)
	}
	if err := list.Save(); err != nil {
		t.Fatal(err)
	}
	// A hand-edited file, whose second copy would survive every "holt mirror rm".
	commonDir, err := repo.CommonDir()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(commonDir, listFile)
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(content, []byte("CLAUDE.local.md\n")...), 0o644); err != nil {
		t.Fatal(err)
	}

	reloaded, err := LoadList(repo)
	if err != nil {
		t.Fatal(err)
	}

	if got := reloaded.Paths(); len(got) != 1 {
		t.Fatalf("the list holds %v, want the duplicate dropped", got)
	}
}

func TestAddRefusesCaseFoldingDuplicate(t *testing.T) {
	main := testutil.NewRepo(t)
	repo := open(t, main)
	if ignoreCase, _, err := repo.Config("core.ignorecase"); err != nil || ignoreCase != "true" {
		t.Skip("this filesystem tells the two spellings apart")
	}
	list, err := LoadList(repo)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := list.Add("CLAUDE.local.md"); err != nil {
		t.Fatal(err)
	}

	// One file, so one entry: kept apart, the second spelling counts as its own path.
	added, err := list.Add("claude.local.md")
	if err != nil {
		t.Fatal(err)
	}

	if added {
		t.Fatalf("the list holds %v, want the second spelling turned away", list.Paths())
	}
}

func TestRemoveFindsCaseFoldingSpelling(t *testing.T) {
	main := testutil.NewRepo(t)
	repo := open(t, main)
	if ignoreCase, _, err := repo.Config("core.ignorecase"); err != nil || ignoreCase != "true" {
		t.Skip("this filesystem tells the two spellings apart")
	}
	list, err := LoadList(repo)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := list.Add("claude.local.md"); err != nil {
		t.Fatal(err)
	}

	// Add treats the two spellings as one path, and a disagreeing Remove would leave
	// it both mirrored and not.
	removed, err := list.Remove("CLAUDE.local.md")
	if err != nil {
		t.Fatal(err)
	}

	if !removed {
		t.Fatalf("the list still holds %v", list.Paths())
	}
}

func TestParseSkipsCaseFoldingDuplicate(t *testing.T) {
	main := testutil.NewRepo(t)
	repo := open(t, main)
	if ignoreCase, _, err := repo.Config("core.ignorecase"); err != nil || ignoreCase != "true" {
		t.Skip("this filesystem tells the two spellings apart")
	}
	commonDir, err := repo.CommonDir()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(commonDir, listFile)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	// Written by hand, past Add: the second spelling would survive a "holt mirror rm"
	// of the first and leave the path both mirrored and not.
	if err := os.WriteFile(path, []byte("CLAUDE.local.md\nclaude.local.md\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	list, err := LoadList(repo)
	if err != nil {
		t.Fatal(err)
	}

	if got := list.Paths(); len(got) != 1 {
		t.Fatalf("the list holds %v, want one entry for one file", got)
	}
}

func TestListKeepsTwoSpellingsWhereTheFilesystemDoesNotFold(t *testing.T) {
	main := testutil.NewRepo(t)
	repo := open(t, main)
	// A filesystem that tells the two apart, the ordinary case off macOS: folded
	// anyway, the second path is refused and its file never reaches a worktree.
	testutil.Git(t, main, "config", "core.ignorecase", "false")
	list, err := LoadList(repo)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := list.Add("CLAUDE.local.md"); err != nil {
		t.Fatal(err)
	}

	added, err := list.Add("claude.local.md")
	if err != nil {
		t.Fatal(err)
	}

	if !added {
		t.Fatalf("the list holds %v, want an entry for each of the two files", list.Paths())
	}
}

func TestLoadListReadsGitsOwnBooleans(t *testing.T) {
	main := testutil.NewRepo(t)
	repo := open(t, main)
	// git reads 1, yes and on as true, and a string comparison would leave the
	// folding off while the filesystem still folds.
	testutil.Git(t, main, "config", "core.ignorecase", "1")
	list, err := LoadList(repo)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := list.Add("CLAUDE.local.md"); err != nil {
		t.Fatal(err)
	}

	added, err := list.Add("claude.local.md")
	if err != nil {
		t.Fatal(err)
	}

	if added {
		t.Fatalf("the list holds %v, want one entry for one file", list.Paths())
	}
}
