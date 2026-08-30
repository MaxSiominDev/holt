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
	if merges := strings.Count(progress.String(), "holt: merged"); merges != 2 {
		t.Errorf("holt reported %d merges, want one for each of the two commits it stopped at:\n%s", merges, &progress)
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
	if !strings.Contains(err.Error(), "CHANGELOG.md") {
		t.Errorf("error %q does not name the file", err)
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
	if !strings.Contains(err.Error(), "NOTES.md") {
		t.Errorf("error %q does not name the file", err)
	}
}

func TestRebaseMergesWhatItCanBeforeStopping(t *testing.T) {
	clone, origin := changelogRepo(t)
	feature := testutil.AddWorktree(t, clone, "feature")
	// One commit a side touching both files: the changelog holt merges, and a
	// conflict it will not touch.
	commitBoth(t, feature, changelogMine, "the branch's version\n")
	commitBoth(t, origin, changelogUpstream, "the default branch's version\n")

	err := Rebase(open(t, feature), false, mergeList(t, "CHANGELOG.md"), &bytes.Buffer{})

	if !errors.Is(err, ErrRebaseStopped) {
		t.Fatalf("got %v, want ErrRebaseStopped", err)
	}
	if !strings.Contains(err.Error(), "shared.txt") {
		t.Errorf("error %q does not name the conflict that is left", err)
	}
	// The point of merging the rest: what holt can do is done, so only the file it
	// will not touch is left to resolve by hand.
	if unmerged := testutil.Git(t, feature, "diff", "--name-only", "--diff-filter=U"); unmerged != "shared.txt" {
		t.Errorf("git still calls %q unmerged, want only shared.txt", unmerged)
	}
	if got := readFile(t, feature, "CHANGELOG.md"); got != changelogMerged {
		t.Errorf("the merged file is\n%q\nwant\n%q", got, changelogMerged)
	}
}

func TestRebaseWithoutAMergeListAbortsAsBefore(t *testing.T) {
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

func commitBoth(t *testing.T, dir, changelog, shared string) {
	t.Helper()
	testutil.WriteFile(t, filepath.Join(dir, "CHANGELOG.md"), changelog)
	testutil.WriteFile(t, filepath.Join(dir, "shared.txt"), shared)
	testutil.Git(t, dir, "add", "-A")
	testutil.Git(t, dir, "commit", "-m", "the changelog and the file it comes with")
}

func mergeList(t *testing.T, patterns ...string) *config.MergeList {
	t.Helper()
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
