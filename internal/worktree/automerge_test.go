package worktree

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MaxSiominDev/holt/internal/config"
	"github.com/MaxSiominDev/holt/internal/testutil"
)

const (
	changelogBefore   = "# Changelog\n\n## [Unreleased]\n\n### Added\n\n- the entry that was there\n"
	changelogMine     = "# Changelog\n\n## [Unreleased]\n\n### Added\n\n- the entry that was there\n- what this branch did\n"
	changelogUpstream = "# Changelog\n\n## [Unreleased]\n\n### Added\n\n- the entry that was there\n- what landed first\n"
	changelogMerged   = "# Changelog\n\n## [Unreleased]\n\n### Added\n\n- the entry that was there\n- what this branch did\n- what landed first\n"
)

func TestRebaseMergesLinesBothSidesAdded(t *testing.T) {
	clone, origin := changelogRepo(t)
	feature := testutil.AddWorktree(t, clone, "feature")
	testutil.CommitTo(t, feature, "CHANGELOG.md", changelogMine)
	// Someone else's entry landed on the default branch first.
	upstream := testutil.CommitTo(t, origin, "CHANGELOG.md", changelogUpstream)

	err := Rebase(open(t, feature), true, mergeList(t, "CHANGELOG.md"), io.Discard)
	if err != nil {
		t.Fatal(err)
	}

	if got := readFile(t, feature, "CHANGELOG.md"); got != changelogMerged {
		t.Errorf("the merged file is\n%q\nwant\n%q", got, changelogMerged)
	}
	if parent := testutil.Git(t, feature, "rev-parse", "HEAD~1"); parent != upstream {
		t.Errorf("the branch sits on %s, want it replanted onto %s", parent, upstream)
	}
	// The merge belongs to the commit, not to a dirty worktree left behind.
	if dirty := testutil.Git(t, feature, "status", "--porcelain"); dirty != "" {
		t.Errorf("the worktree is left holding %q", dirty)
	}
	if committed := testutil.Git(t, feature, "show", "HEAD:CHANGELOG.md"); committed != strings.TrimRight(changelogMerged, "\n") {
		t.Errorf("the commit holds\n%q", committed)
	}
}

func TestRebaseMergesOnEveryCommitItStopsAt(t *testing.T) {
	clone, origin := changelogRepo(t)
	feature := testutil.AddWorktree(t, clone, "feature")
	testutil.CommitTo(t, feature, "CHANGELOG.md", changelogMine)
	// A second commit of this branch touching the same file, so the rebase has
	// more than one stop to get through.
	second := changelogMine + "- and what it did next\n"
	testutil.CommitTo(t, feature, "CHANGELOG.md", second)
	testutil.CommitTo(t, origin, "CHANGELOG.md", changelogUpstream)

	var progress bytes.Buffer
	err := Rebase(open(t, feature), true, mergeList(t, "CHANGELOG.md"), &progress)
	if err != nil {
		t.Fatal(err)
	}

	for _, entry := range []string{"- what this branch did", "- and what it did next", "- what landed first"} {
		if !strings.Contains(readFile(t, feature, "CHANGELOG.md"), entry) {
			t.Errorf("%q is not in the merged file:\n%s", entry, readFile(t, feature, "CHANGELOG.md"))
		}
	}
	// Named once however many stops it took, and the entries above are what says
	// both stops were settled: a stop holt could not take would have ended it.
	if merges := strings.Count(progress.String(), "holt: merged CHANGELOG.md\n"); merges != 1 {
		t.Errorf("holt reported %d merges, want the file named once however many stops it took:\n%s", merges, &progress)
	}
	// git's advice names a --continue, a --skip and an abort that this run has
	// taken away. It is turned off for the first stop; the later ones need it too.
	if strings.Contains(progress.String(), "git rebase --skip") {
		t.Errorf("output %q still offers git's advice for a rebase holt finished", &progress)
	}
}

func TestRebaseAbortsWhenTheOtherSideChangedALine(t *testing.T) {
	clone, origin := changelogRepo(t)
	feature := testutil.AddWorktree(t, clone, "feature")
	testutil.CommitTo(t, feature, "CHANGELOG.md", changelogMine)
	// Not an addition: the entry that was there is rewritten, which is the shape
	// holt refuses however small it is.
	testutil.CommitTo(t, origin, "CHANGELOG.md",
		strings.Replace(changelogBefore, "- the entry that was there\n", "- the entry that was there, reworded\n", 1))
	head := testutil.Git(t, feature, "rev-parse", "HEAD")

	err := Rebase(open(t, feature), true, mergeList(t, "CHANGELOG.md"), io.Discard)

	if !errors.Is(err, ErrRebaseStopped) {
		t.Fatalf("got %v, want ErrRebaseStopped", err)
	}
	if !strings.Contains(err.Error(), "the default branch") {
		t.Errorf("error %q does not name the side that changed a line", err)
	}
	if now := testutil.Git(t, feature, "rev-parse", "HEAD"); now != head {
		t.Errorf("the branch sits on %s, want it back on %s", now, head)
	}
}

func TestRebaseAbortsWhenTheFileIsNewOnBothSides(t *testing.T) {
	clone, origin := changelogRepo(t)
	feature := testutil.AddWorktree(t, clone, "feature")
	// No ancestor to add lines to: each side wrote the file from nothing.
	testutil.CommitTo(t, feature, "NOTES.md", "- this branch's note\n")
	testutil.CommitTo(t, origin, "NOTES.md", "- the default branch's note\n")

	err := Rebase(open(t, feature), true, mergeList(t, "*.md"), io.Discard)

	if !errors.Is(err, ErrRebaseStopped) {
		t.Fatalf("got %v, want ErrRebaseStopped", err)
	}
	if !strings.Contains(err.Error(), "missing from one of the three versions") {
		t.Errorf("error %q does not give the reason holt would not settle NOTES.md", err)
	}
}

func TestRebaseMergesWhatItCanBeforeStopping(t *testing.T) {
	clone, origin := changelogRepo(t)
	feature := testutil.AddWorktree(t, clone, "feature")
	// One commit a side touching both files: the changelog holt merges, and a
	// conflict it will not touch.
	commitBoth(t, feature, changelogMine, "the branch's version\n")
	commitBoth(t, origin, changelogUpstream, "the default branch's version\n")

	var progress bytes.Buffer
	err := Rebase(open(t, feature), false, mergeList(t, "CHANGELOG.md"), &progress)

	if !errors.Is(err, ErrRebaseStopped) {
		t.Fatalf("got %v, want ErrRebaseStopped", err)
	}
	// The merges survive here, so this is the one path that has to name them.
	if !strings.Contains(progress.String(), "holt: merged CHANGELOG.md") {
		t.Errorf("output %q does not say what holt settled before it stopped", &progress)
	}
	if !strings.Contains(err.Error(), "API.md") {
		t.Errorf("error %q does not name the conflict that is left", err)
	}
	// The point of merging the rest: git hands the files over in path order, so
	// the one holt will not touch comes first here, and the changelog after it is
	// settled all the same.
	if unmerged := testutil.Git(t, feature, "diff", "--name-only", "--diff-filter=U"); unmerged != "API.md" {
		t.Errorf("git still calls %q unmerged, want only API.md", unmerged)
	}
	if got := readFile(t, feature, "CHANGELOG.md"); got != changelogMerged {
		t.Errorf("the merged file is\n%q\nwant\n%q", got, changelogMerged)
	}
}

func TestRebaseWithNothingListedAborts(t *testing.T) {
	clone, origin := changelogRepo(t)
	feature := testutil.AddWorktree(t, clone, "feature")
	testutil.CommitTo(t, feature, "CHANGELOG.md", changelogMine)
	testutil.CommitTo(t, origin, "CHANGELOG.md", changelogUpstream)

	// The list every user starts with: holt merges nothing until it names a file.
	err := Rebase(open(t, feature), true, mergeList(t), io.Discard)

	if !errors.Is(err, ErrRebaseStopped) {
		t.Fatalf("got %v, want ErrRebaseStopped", err)
	}
	if operation, _ := OperationInProgress(open(t, feature)); operation != "" {
		t.Errorf("got operation %q, want the rebase holt started undone", operation)
	}
}

// A clone whose default branch already carries the changelog, so a conflict in
// it has an ancestor to compare against.
func changelogRepo(t *testing.T) (clone, origin string) {
	t.Helper()
	clone, origin = testutil.NewClonedRepo(t)
	testutil.CommitTo(t, origin, "CHANGELOG.md", changelogBefore)
	testutil.Git(t, clone, "pull", "--quiet", "origin", "main")
	return clone, origin
}

// The second file sorts before the changelog, which is the order git hands the
// conflicts over in.
func commitBoth(t *testing.T, dir, changelog, other string) {
	t.Helper()
	testutil.WriteFile(t, filepath.Join(dir, "CHANGELOG.md"), changelog)
	testutil.WriteFile(t, filepath.Join(dir, "API.md"), other)
	testutil.Git(t, dir, "add", "-A")
	testutil.Git(t, dir, "commit", "-m", "the changelog and the file it comes with")
}

func mergeList(t *testing.T, patterns ...string) *config.MergeList {
	t.Helper()
	if os.Getenv("XDG_CONFIG_HOME") == "" {
		t.Fatal("no XDG_CONFIG_HOME, so this would write holt's configuration into the working directory")
	}
	content := ""
	if len(patterns) > 0 {
		content = strings.Join(patterns, "\n") + "\n"
	}
	testutil.WriteFile(t, filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "holt", "merge.list"), content)

	list, err := config.LoadMergeList()
	if err != nil {
		t.Fatal(err)
	}
	if rejected := list.Rejected(); len(rejected) > 0 {
		t.Fatalf("the test's own merge list was not read: %v", rejected)
	}
	return list
}

func readFile(t *testing.T, dir, name string) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

func TestRebaseMergesWithAnEditorConfigured(t *testing.T) {
	clone, origin := changelogRepo(t)
	feature := testutil.AddWorktree(t, clone, "feature")
	testutil.CommitTo(t, feature, "CHANGELOG.md", changelogMine)
	testutil.CommitTo(t, origin, "CHANGELOG.md", changelogUpstream)
	// git opens an editor on the message of the commit that finishes a stopped
	// rebase, and this variable outranks the core.editor setting. Anyone who has
	// one set would otherwise be dropped into it, or held up by one that waits.
	t.Setenv("GIT_EDITOR", "false")

	if err := Rebase(open(t, feature), true, mergeList(t, "CHANGELOG.md"), io.Discard); err != nil {
		t.Fatal(err)
	}

	if got := readFile(t, feature, "CHANGELOG.md"); got != changelogMerged {
		t.Errorf("the merged file is\n%q\nwant\n%q", got, changelogMerged)
	}
}

func TestRebaseAbortsWithAFileAlreadyMerged(t *testing.T) {
	clone, origin := changelogRepo(t)
	feature := testutil.AddWorktree(t, clone, "feature")
	commitBoth(t, feature, changelogMine, "the branch's version\n")
	commitBoth(t, origin, changelogUpstream, "the default branch's version\n")
	head := testutil.Git(t, feature, "rev-parse", "HEAD")

	err := Rebase(open(t, feature), true, mergeList(t, "CHANGELOG.md"), io.Discard)

	if !errors.Is(err, ErrRebaseStopped) {
		t.Fatalf("got %v, want ErrRebaseStopped", err)
	}
	// The merge holt had already staged goes back with everything else, rather
	// than being left behind as a change nobody made.
	if now := testutil.Git(t, feature, "rev-parse", "HEAD"); now != head {
		t.Errorf("the branch sits on %s, want it back on %s", now, head)
	}
	if dirty := testutil.Git(t, feature, "status", "--porcelain"); dirty != "" {
		t.Errorf("the worktree is left holding %q", dirty)
	}
	if got := readFile(t, feature, "CHANGELOG.md"); got != changelogMine {
		t.Errorf("CHANGELOG.md is\n%q\nwant the branch's own version back:\n%q", got, changelogMine)
	}
}

func TestRebaseAbortsWhenTheFileModeDiffers(t *testing.T) {
	clone, origin := changelogRepo(t)
	feature := testutil.AddWorktree(t, clone, "feature")
	testutil.CommitTo(t, feature, "CHANGELOG.md", changelogMine)
	// A mode of its own on the other side. Which of the two the merged file should
	// have is a conflict of its own, and lines do not settle it.
	testutil.WriteFile(t, filepath.Join(origin, "CHANGELOG.md"), changelogUpstream)
	if err := os.Chmod(filepath.Join(origin, "CHANGELOG.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	testutil.Git(t, origin, "add", "-A")
	testutil.Git(t, origin, "commit", "-m", "the changelog, made executable")

	err := Rebase(open(t, feature), true, mergeList(t, "CHANGELOG.md"), io.Discard)

	if !errors.Is(err, ErrRebaseStopped) {
		t.Fatalf("got %v, want ErrRebaseStopped", err)
	}
	if !strings.Contains(err.Error(), "the same file mode on the two sides") {
		t.Errorf("error %q does not say what holt would not settle", err)
	}
}

func TestFinishStoppedRebaseWithNothingUnmerged(t *testing.T) {
	clone, origin := testutil.NewClonedRepo(t)
	feature := branchWorktree(t, clone, "feature", "shared.txt", "the branch's version\n")
	testutil.CommitTo(t, origin, "shared.txt", "the default branch's version\n")
	before := testutil.Git(t, feature, "rev-parse", "HEAD")
	if err := Rebase(open(t, feature), false, nil, io.Discard); !errors.Is(err, ErrRebaseStopped) {
		t.Fatalf("the fixture produced %v, want a stopped rebase", err)
	}
	// A rebase standing with nothing unmerged, which is what git leaves once the
	// conflict has been settled by hand. holt has nothing left to merge there and
	// nothing to continue on the strength of.
	testutil.WriteFile(t, filepath.Join(feature, "shared.txt"), "settled by hand\n")
	testutil.Git(t, feature, "add", "shared.txt")

	err := finishStoppedRebase(open(t, feature), true, nil, io.Discard)

	if !errors.Is(err, ErrRebaseStopped) {
		t.Fatalf("got %v, want ErrRebaseStopped", err)
	}
	if !strings.Contains(err.Error(), "nothing was left unmerged") {
		t.Errorf("error %q does not say why holt stopped", err)
	}
	if head := testutil.Git(t, feature, "rev-parse", "HEAD"); head != before {
		t.Errorf("the branch sits on %s, want it back on %s", head, before)
	}
}

func TestRebaseAbortsWithoutClaimingAnEarlierMerge(t *testing.T) {
	clone, origin := changelogRepo(t)
	feature := testutil.AddWorktree(t, clone, "feature")
	// One commit holt can settle and a second it cannot, so the rebase stops
	// twice and the abort takes both commits back.
	testutil.CommitTo(t, feature, "CHANGELOG.md", changelogMine)
	testutil.CommitTo(t, feature, "API.md", "the branch's version\n")
	commitBoth(t, origin, changelogUpstream, "the default branch's version\n")
	before := testutil.Git(t, feature, "rev-parse", "HEAD")
	var progress bytes.Buffer

	err := Rebase(open(t, feature), true, mergeList(t, "CHANGELOG.md"), &progress)

	if !errors.Is(err, ErrRebaseStopped) {
		t.Fatalf("got %v, want ErrRebaseStopped", err)
	}
	// The merge of the first commit went back with the abort, so claiming it
	// would send the user looking for a change that is not there.
	if strings.Contains(progress.String(), "holt: merged") {
		t.Errorf("output %q claims a merge the abort took back", &progress)
	}
	if head := testutil.Git(t, feature, "rev-parse", "HEAD"); head != before {
		t.Errorf("the branch sits on %s, want it back on %s", head, before)
	}
	if dirty := testutil.Git(t, feature, "status", "--porcelain"); dirty != "" {
		t.Errorf("the worktree is left holding %q", dirty)
	}
}

func TestKeepsEveryLine(t *testing.T) {
	tests := []struct {
		name     string
		ancestor string
		merged   string
		want     bool
	}{
		{"both sides added", "- old\n", "- old\n- mine\n- theirs\n", true},
		// git merge-file gives the result the ending of its last input, so a union
		// of a file that has no final newline has none either.
		{"the merge lost the final newline", "- old\n", "- old\n- mine\n- theirs", true},
		{"the ancestor had no final newline", "- old", "- old\n- mine\n", true},
		{"nothing to lose", "", "- mine\n- theirs", true},
		{"a line went missing", "- old\n- kept\n", "- kept\n- mine\n", false},
		{"the lines came back in the other order", "- first\n- second\n", "- second\n- first\n", false},
		{"a line was rewritten", "- old\n", "- old rewritten\n- mine\n", false},
		{"an empty line went missing", "\n- old\n", "- old\n", false},
		{"one of two identical lines went missing", "- old\n- old\n", "- old\n- mine\n", false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := keepsEveryLine([]byte(test.ancestor), []byte(test.merged)); got != test.want {
				t.Errorf("got %v, want %v", got, test.want)
			}
		})
	}
}

func TestRebaseAbortsWhenBothSidesChangedTheFileMode(t *testing.T) {
	clone, origin := changelogRepo(t)
	feature := testutil.AddWorktree(t, clone, "feature")
	executable(t, feature, changelogMine)
	// The sides agree about the mode and git settles it on its own, so the file
	// still arrives here with lines to merge. The mode changed all the same.
	executable(t, origin, changelogUpstream)

	err := Rebase(open(t, feature), true, mergeList(t, "CHANGELOG.md"), io.Discard)

	if !errors.Is(err, ErrRebaseStopped) {
		t.Fatalf("got %v, want ErrRebaseStopped", err)
	}
	if !strings.Contains(err.Error(), "had its file mode changed") {
		t.Errorf("error %q blames the two sides for a mode they agree on", err)
	}
}

func TestRebaseStagesOnlyTheFileItMerged(t *testing.T) {
	clone, origin := testutil.NewClonedRepo(t)
	// A tracked name holding a glob character, next to one the glob would catch.
	testutil.WriteFile(t, filepath.Join(origin, "a*.md"), "- was there\n")
	testutil.WriteFile(t, filepath.Join(origin, "ab.md"), "- was there\n")
	testutil.Git(t, origin, "add", "-A")
	testutil.Git(t, origin, "commit", "-m", "two notes")
	testutil.Git(t, clone, "pull", "--quiet", "origin", "main")
	feature := testutil.AddWorktree(t, clone, "feature")
	writeBoth(t, feature, "- was there\n- mine\n", "- was there\n- mine\n")
	// The second file is rewritten rather than added to, so holt will not settle it.
	writeBoth(t, origin, "- was there\n- theirs\n", "- rewritten\n")

	err := Rebase(open(t, feature), false, mergeList(t, "*.md"), io.Discard)

	if !errors.Is(err, ErrRebaseStopped) {
		t.Fatalf("got %v, want ErrRebaseStopped", err)
	}
	// git reads a path as a pattern, so staging the file holt merged must not
	// carry off the one it refused, conflict markers and all.
	if unmerged := testutil.Git(t, feature, "diff", "--name-only", "--diff-filter=U"); unmerged != "ab.md" {
		t.Errorf("git calls %q unmerged, want ab.md left as it was", unmerged)
	}
}

func executable(t *testing.T, dir, changelog string) {
	t.Helper()
	testutil.WriteFile(t, filepath.Join(dir, "CHANGELOG.md"), changelog)
	if err := os.Chmod(filepath.Join(dir, "CHANGELOG.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	testutil.Git(t, dir, "add", "-A")
	testutil.Git(t, dir, "commit", "-m", "the changelog, made executable")
}

func writeBoth(t *testing.T, dir, glob, plain string) {
	t.Helper()
	testutil.WriteFile(t, filepath.Join(dir, "a*.md"), glob)
	testutil.WriteFile(t, filepath.Join(dir, "ab.md"), plain)
	testutil.Git(t, dir, "add", "-A")
	testutil.Git(t, dir, "commit", "-m", "both notes")
}

func TestRebaseMergesFromASubdirectory(t *testing.T) {
	clone, origin := changelogRepo(t)
	feature := testutil.AddWorktree(t, clone, "feature")
	testutil.CommitTo(t, feature, "CHANGELOG.md", changelogMine)
	testutil.CommitTo(t, origin, "CHANGELOG.md", changelogUpstream)
	sub := filepath.Join(feature, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	// git lists the unmerged files under the directory it is run in, so a conflict
	// in the root is invisible from here unless holt asks from the top.
	if err := Rebase(open(t, sub), true, mergeList(t, "CHANGELOG.md"), io.Discard); err != nil {
		t.Fatal(err)
	}

	if got := readFile(t, feature, "CHANGELOG.md"); got != changelogMerged {
		t.Errorf("the merged file is\n%q\nwant\n%q", got, changelogMerged)
	}
}

func TestRebaseMergesAnExecutableFile(t *testing.T) {
	clone, origin := testutil.NewClonedRepo(t)
	executable(t, origin, changelogBefore)
	testutil.Git(t, clone, "pull", "--quiet", "origin", "main")
	feature := testutil.AddWorktree(t, clone, "feature")
	// Writing over a checked-out file leaves its mode alone, so all three
	// versions stay executable and the merge is about the lines only.
	testutil.CommitTo(t, feature, "CHANGELOG.md", changelogMine)
	testutil.CommitTo(t, origin, "CHANGELOG.md", changelogUpstream)

	if err := Rebase(open(t, feature), true, mergeList(t, "CHANGELOG.md"), io.Discard); err != nil {
		t.Fatal(err)
	}

	if got := readFile(t, feature, "CHANGELOG.md"); got != changelogMerged {
		t.Errorf("the merged file is\n%q\nwant\n%q", got, changelogMerged)
	}
	if staged := testutil.Git(t, feature, "ls-files", "--stage", "CHANGELOG.md"); !strings.HasPrefix(staged, "100755") {
		t.Errorf("git records %q, want the executable bit kept", staged)
	}
}

func TestRebaseAbortsOnABinaryFile(t *testing.T) {
	clone, origin := testutil.NewClonedRepo(t)
	// A .md name over content git counts as binary, which has no lines to add.
	binary := "- was there\n\x00\n"
	testutil.CommitTo(t, origin, "notes.md", binary)
	testutil.Git(t, clone, "pull", "--quiet", "origin", "main")
	feature := testutil.AddWorktree(t, clone, "feature")
	testutil.CommitTo(t, feature, "notes.md", binary+"- mine\n")
	testutil.CommitTo(t, origin, "notes.md", binary+"- theirs\n")

	err := Rebase(open(t, feature), true, mergeList(t, "notes.md"), io.Discard)

	if !errors.Is(err, ErrRebaseStopped) {
		t.Fatalf("got %v, want ErrRebaseStopped", err)
	}
	if !strings.Contains(err.Error(), "binary") {
		t.Errorf("error %q does not say why the file has no lines to add", err)
	}
}

func TestRebaseAbortsWhenTheUnionRepeatsLines(t *testing.T) {
	clone, origin := testutil.NewClonedRepo(t)
	// Two insertion points with only two lines between them, which git folds into
	// one conflicted region: a union then writes those two lines out twice, once
	// after each side's addition.
	testutil.CommitTo(t, origin, "notes.md", "top\nmiddle\nand middle\nbottom\n")
	testutil.Git(t, clone, "pull", "--quiet", "origin", "main")
	feature := testutil.AddWorktree(t, clone, "feature")
	testutil.CommitTo(t, feature, "notes.md", "top\nmine first\nmiddle\nand middle\nmine second\nbottom\n")
	testutil.CommitTo(t, origin, "notes.md", "top\ntheirs first\nmiddle\nand middle\ntheirs second\nbottom\n")

	err := Rebase(open(t, feature), true, mergeList(t, "notes.md"), io.Discard)

	if !errors.Is(err, ErrRebaseStopped) {
		t.Fatalf("got %v, want ErrRebaseStopped", err)
	}
	if !strings.Contains(err.Error(), "more often than the two sides hold it") {
		t.Errorf("error %q does not say what was wrong with the merge", err)
	}
	// Every line of the ancestor survives a union like that, so nothing but the
	// count says the file came out wrong.
	if got := readFile(t, feature, "notes.md"); strings.Count(got, "and middle") != 1 {
		t.Errorf("the file was left as\n%s", got)
	}
}

func TestRebaseAbortsWhenAUnionRepeatsALineBothSidesAdded(t *testing.T) {
	clone, origin := testutil.NewClonedRepo(t)
	// Both sides open with the same new line. Counted on each side it pays for one
	// repeat of an ancestor line, which is what the union then writes twice.
	testutil.CommitTo(t, origin, "notes.md", "- keep me\n")
	testutil.Git(t, clone, "pull", "--quiet", "origin", "main")
	feature := testutil.AddWorktree(t, clone, "feature")
	testutil.CommitTo(t, feature, "notes.md", "- both added this\n- keep me\n- mine one\n- mine two\n")
	testutil.CommitTo(t, origin, "notes.md", "- both added this\n- theirs one\n- keep me\n- theirs two\n")

	err := Rebase(open(t, feature), true, mergeList(t, "notes.md"), io.Discard)

	if !errors.Is(err, ErrRebaseStopped) {
		t.Fatalf("got %v, want ErrRebaseStopped", err)
	}
	if !strings.Contains(err.Error(), "more often than the two sides hold it") {
		t.Errorf("error %q does not say what was wrong with the merge", err)
	}
}

func TestRebaseAbortsWhenThisBranchChangedALine(t *testing.T) {
	clone, origin := changelogRepo(t)
	feature := testutil.AddWorktree(t, clone, "feature")
	// The rewrite is on this branch this time: both sides are held to the same
	// rule, and a merge that reverted it would be as wrong either way round.
	testutil.CommitTo(t, feature, "CHANGELOG.md",
		strings.Replace(changelogBefore, "- the entry that was there\n", "- the entry, reworded here\n", 1))
	testutil.CommitTo(t, origin, "CHANGELOG.md", changelogUpstream)

	err := Rebase(open(t, feature), true, mergeList(t, "CHANGELOG.md"), io.Discard)

	if !errors.Is(err, ErrRebaseStopped) {
		t.Fatalf("got %v, want ErrRebaseStopped", err)
	}
	if !strings.Contains(err.Error(), "this branch") {
		t.Errorf("error %q does not name the side that changed a line", err)
	}
}

func TestRebaseAbortsOnASymlink(t *testing.T) {
	clone, origin := testutil.NewClonedRepo(t)
	// A symlink under a listed name. All three versions are links, so the modes
	// agree, and what the two sides disagree about is where it points, which no
	// amount of merging lines settles.
	link(t, origin, "notes.md", "target.txt")
	testutil.Git(t, origin, "commit", "-m", "a link under a markdown name")
	testutil.Git(t, clone, "pull", "--quiet", "origin", "main")
	feature := testutil.AddWorktree(t, clone, "feature")
	link(t, feature, "notes.md", "mine.txt")
	testutil.Git(t, feature, "commit", "-m", "pointed at this branch's file")
	link(t, origin, "notes.md", "theirs.txt")
	testutil.Git(t, origin, "commit", "-m", "pointed at the default branch's file")

	err := Rebase(open(t, feature), true, mergeList(t, "notes.md"), io.Discard)

	if !errors.Is(err, ErrRebaseStopped) {
		t.Fatalf("got %v, want ErrRebaseStopped", err)
	}
	if !strings.Contains(err.Error(), "not a plain file") {
		t.Errorf("error %q does not say why the file has no lines to merge", err)
	}
}

func link(t *testing.T, dir, name, target string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	testutil.Git(t, dir, "add", name)
}

func TestRebaseMergesAFileInASubdirectory(t *testing.T) {
	clone, origin := testutil.NewClonedRepo(t)
	testutil.CommitTo(t, origin, filepath.Join("docs", "notes.md"), "- was there\n")
	testutil.Git(t, clone, "pull", "--quiet", "origin", "main")
	feature := testutil.AddWorktree(t, clone, "feature")
	testutil.CommitTo(t, feature, filepath.Join("docs", "notes.md"), "- was there\n- mine\n")
	testutil.CommitTo(t, origin, filepath.Join("docs", "notes.md"), "- was there\n- theirs\n")

	// git names the file with slashes whatever the platform, which is what both
	// the pattern and the pathspec are written in and what the write path is not.
	if err := Rebase(open(t, feature), true, mergeList(t, "docs/*.md"), io.Discard); err != nil {
		t.Fatal(err)
	}

	want := "- was there\n- mine\n- theirs\n"
	if got := readFile(t, feature, filepath.Join("docs", "notes.md")); got != want {
		t.Errorf("the merged file is\n%q\nwant\n%q", got, want)
	}
}

func TestReachesOutside(t *testing.T) {
	tests := []struct {
		name string
		path string
		want bool
	}{
		{"a file in the root", "CHANGELOG.md", false},
		{"a file in a directory", "docs/notes.md", false},
		{"a path that cleans back inside", "docs/../notes.md", false},
		{"a name that begins with two dots", "..notes.md", false},
		{"a directory that begins with .git", ".github/workflows/ci.yml", false},
		{"the parent itself", "..", true},
		{"a path through the parent", "../escape.md", true},
		{"a path back out of a directory", "docs/../../escape.md", true},
		{"an absolute path", "/etc/notes.md", true},
		{"git's own directory", ".git/hooks/post-checkout", true},
		{"git's own directory in another case", ".GIT/config", true},
		// Legal names on Linux, and separators once Windows joins them.
		{"a traversal written with backslashes", `..\escape.md`, true},
		{"a traversal out of a directory with backslashes", `docs\..\..\escape.md`, true},
		{"git's own directory with backslashes", `.git\hooks\post-checkout`, true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := reachesOutside(test.path); got != test.want {
				t.Errorf("got %v, want %v", got, test.want)
			}
		})
	}
}
