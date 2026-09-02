package worktree

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MaxSiominDev/holt/internal/testutil"
)

func TestUncommitTakesTheLastCommitOff(t *testing.T) {
	repo := testutil.NewRepo(t)
	before := testutil.Git(t, repo, "rev-parse", "HEAD")
	testutil.CommitTo(t, repo, "later.txt", "written later\n")

	if err := Uncommit(open(t, repo), io.Discard); err != nil {
		t.Fatal(err)
	}

	if head := testutil.Git(t, repo, "rev-parse", "HEAD"); head != before {
		t.Fatalf("the branch is at %s, want %s", head, before)
	}
	if status := testutil.Git(t, repo, "status", "--porcelain"); status != "A  later.txt" {
		t.Errorf("status is %q, want the file of the undone commit staged", status)
	}
	// --soft leaves the working tree alone, so the file is still written out.
	content, err := os.ReadFile(filepath.Join(repo, "later.txt"))
	if err != nil || string(content) != "written later\n" {
		t.Errorf("the file reads %q (%v), want it untouched", content, err)
	}
}

func TestUncommitNamesTheCommitItTookBack(t *testing.T) {
	repo := testutil.NewRepo(t)
	testutil.CommitTo(t, repo, "later.txt", "written later\n")
	short := testutil.Git(t, repo, "rev-parse", "--short", "HEAD")

	var progress strings.Builder
	if err := Uncommit(open(t, repo), &progress); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(progress.String(), short) {
		t.Errorf("%q does not name the commit %s it took back", progress.String(), short)
	}
	if !strings.Contains(progress.String(), "add later.txt") {
		t.Errorf("%q does not carry the subject of the commit", progress.String())
	}
}

func TestUncommitKeepsWorkThatWasNeverCommitted(t *testing.T) {
	repo := testutil.NewRepo(t)
	testutil.CommitTo(t, repo, "later.txt", "written later\n")
	testutil.WriteFile(t, filepath.Join(repo, "staged.txt"), "staged by hand\n")
	testutil.Git(t, repo, "add", "staged.txt")
	testutil.WriteFile(t, filepath.Join(repo, "README.md"), "edited and never staged\n")

	// A dirty tree is no reason to refuse: --soft touches neither it nor the index.
	if err := Uncommit(open(t, repo), io.Discard); err != nil {
		t.Fatal(err)
	}

	status := testutil.Git(t, repo, "status", "--porcelain")
	if status != " M README.md\nA  later.txt\nA  staged.txt" {
		t.Errorf("status is %q, want the undone commit staged beside the work that was already there", status)
	}
}

func TestUncommitMergeCommitGoesToTheFirstParent(t *testing.T) {
	repo := testutil.NewRepo(t)
	testutil.Git(t, repo, "switch", "--quiet", "--create", "side")
	testutil.CommitTo(t, repo, "theirs.txt", "from the side branch\n")
	testutil.Git(t, repo, "switch", "--quiet", "main")
	mine := testutil.CommitTo(t, repo, "mine.txt", "on main\n")
	testutil.Git(t, repo, "merge", "--quiet", "--no-ff", "-m", "merge side", "side")

	if err := Uncommit(open(t, repo), io.Discard); err != nil {
		t.Fatal(err)
	}

	if head := testutil.Git(t, repo, "rev-parse", "HEAD"); head != mine {
		t.Fatalf("the branch is at %s, want the first parent %s", head, mine)
	}
	if status := testutil.Git(t, repo, "status", "--porcelain"); status != "A  theirs.txt" {
		t.Errorf("status is %q, want the merged side staged", status)
	}
}

func TestUncommitInLinkedWorktree(t *testing.T) {
	main := testutil.NewRepo(t)
	feature := testutil.AddWorktree(t, main, "feature")
	onMain := testutil.CommitTo(t, main, "on-main.txt", "committed in the main checkout\n")
	before := testutil.Git(t, feature, "rev-parse", "HEAD")
	testutil.CommitTo(t, feature, "later.txt", "written in the worktree\n")

	if err := Uncommit(open(t, feature), io.Discard); err != nil {
		t.Fatal(err)
	}

	if head := testutil.Git(t, feature, "rev-parse", "HEAD"); head != before {
		t.Fatalf("the branch is at %s, want %s", head, before)
	}
	// HEAD is per-worktree, and only the branch this one holds may move.
	if head := testutil.Git(t, main, "rev-parse", "HEAD"); head != onMain {
		t.Errorf("the main checkout moved to %s, want it left at %s", head, onMain)
	}
}

func TestUncommitDuringACherryPick(t *testing.T) {
	repo := testutil.NewRepo(t)
	testutil.Git(t, repo, "switch", "--quiet", "--create", "side")
	testutil.CommitTo(t, repo, "shared.txt", "from the side branch\n")
	testutil.Git(t, repo, "switch", "--quiet", "main")
	testutil.CommitTo(t, repo, "shared.txt", "on main\n")
	testutil.GitStopping(t, repo, "cherry-pick", "side")
	// Resolved but not continued, which is where git stops refusing the reset and
	// starts taking the cherry-pick down with it.
	testutil.WriteFile(t, filepath.Join(repo, "shared.txt"), "resolved by hand\n")
	testutil.Git(t, repo, "add", "shared.txt")
	before := testutil.Git(t, repo, "rev-parse", "HEAD")

	err := Uncommit(open(t, repo), io.Discard)

	if err == nil {
		t.Fatal("the branch was taken back from under an unfinished cherry-pick")
	}
	if !strings.Contains(err.Error(), "cherry-pick") {
		t.Errorf("error %q does not name what is unfinished", err)
	}
	if head := testutil.Git(t, repo, "rev-parse", "HEAD"); head != before {
		t.Errorf("the branch moved to %s, want it left at %s", head, before)
	}
	gitDir := testutil.Git(t, repo, "rev-parse", "--absolute-git-dir")
	if _, err := os.Stat(filepath.Join(gitDir, "CHERRY_PICK_HEAD")); err != nil {
		t.Errorf("the cherry-pick is no longer there to continue: %v", err)
	}
}

func TestUncommitInBareRepository(t *testing.T) {
	source := testutil.NewRepo(t)
	testutil.CommitTo(t, source, "later.txt", "written later\n")
	bare := filepath.Join(filepath.Dir(source), "bare.git")
	testutil.Git(t, source, "clone", "--quiet", "--bare", source, bare)
	before := testutil.Git(t, bare, "rev-parse", "HEAD")

	err := Uncommit(open(t, bare), io.Discard)

	if err == nil {
		t.Fatal("a repository with no working tree to stage the changes in was uncommitted")
	}
	if !strings.Contains(err.Error(), "bare") {
		t.Errorf("error %q does not say what is wrong", err)
	}
	if head := testutil.Git(t, bare, "rev-parse", "HEAD"); head != before {
		t.Errorf("the branch moved to %s, want it left at %s", head, before)
	}
}

func TestUncommitDetachedHead(t *testing.T) {
	repo := testutil.NewRepo(t)
	testutil.CommitTo(t, repo, "later.txt", "written later\n")
	testutil.Git(t, repo, "checkout", "--quiet", "--detach")
	before := testutil.Git(t, repo, "rev-parse", "HEAD")

	err := Uncommit(open(t, repo), io.Discard)

	if err == nil {
		t.Fatal("a detached HEAD was uncommitted, which takes the commit off no branch")
	}
	if !strings.Contains(err.Error(), "no branch") {
		t.Errorf("error %q does not say what is wrong", err)
	}
	if head := testutil.Git(t, repo, "rev-parse", "HEAD"); head != before {
		t.Errorf("HEAD moved to %s, want it left at %s", head, before)
	}
}

func TestUncommitFirstCommitOfTheHistory(t *testing.T) {
	repo := testutil.NewRepo(t)
	before := testutil.Git(t, repo, "rev-parse", "HEAD")

	err := Uncommit(open(t, repo), io.Discard)

	if err == nil {
		t.Fatal("the commit with nothing behind it was taken off the branch")
	}
	if !strings.Contains(err.Error(), "nothing behind it") {
		t.Errorf("error %q does not say what is wrong", err)
	}
	// git's own refusal quotes the HEAD~1 the user never wrote.
	if strings.Contains(err.Error(), "HEAD~1") {
		t.Errorf("error %q quotes git about an argument the user never wrote", err)
	}
	if head := testutil.Git(t, repo, "rev-parse", "HEAD"); head != before {
		t.Errorf("the branch moved to %s, want it left at %s", head, before)
	}
}

func TestUncommitBranchWithoutCommits(t *testing.T) {
	repo := testutil.NewEmptyRepo(t)

	err := Uncommit(open(t, repo), io.Discard)

	if err == nil {
		t.Fatal("a branch holding no commit at all was uncommitted")
	}
	if !strings.Contains(err.Error(), "no commit") {
		t.Errorf("error %q does not say what is wrong", err)
	}
}

func TestUncommitCommitWithoutAMessage(t *testing.T) {
	repo := testutil.NewRepo(t)
	testutil.WriteFile(t, filepath.Join(repo, "later.txt"), "written later\n")
	testutil.Git(t, repo, "add", "later.txt")
	testutil.Git(t, repo, "commit", "--allow-empty-message", "--message", "")
	short := testutil.Git(t, repo, "rev-parse", "--short", "HEAD")

	var progress strings.Builder
	if err := Uncommit(open(t, repo), &progress); err != nil {
		t.Fatal(err)
	}

	// Nothing to name the commit by but the hash, and no room for the space
	// that would otherwise trail it.
	if progress.String() != "holt: took back "+short+"\n" {
		t.Errorf("the line is %q, want the bare hash", progress.String())
	}
}

func TestUncommitCommitWithAMessageOfSeveralLines(t *testing.T) {
	repo := testutil.NewRepo(t)
	testutil.WriteFile(t, filepath.Join(repo, "later.txt"), "written later\n")
	testutil.Git(t, repo, "add", "later.txt")
	// git folds a subject running over several lines into one, which is what
	// keeps the fields holt reads to a line each.
	testutil.Git(t, repo, "commit", "--message", "first line\nsecond line\n\nthe body")

	var progress strings.Builder
	if err := Uncommit(open(t, repo), &progress); err != nil {
		t.Fatal(err)
	}

	if line := progress.String(); !strings.HasSuffix(line, " first line second line\n") {
		t.Errorf("the line is %q, want the subject folded onto it", line)
	}
}

func TestUncommitWithAFileNamedLikeTheRevision(t *testing.T) {
	repo := testutil.NewRepo(t)
	before := testutil.Git(t, repo, "rev-parse", "HEAD")
	testutil.CommitTo(t, repo, "later.txt", "written later\n")
	// Left unstaged and untracked: git refuses an argument that is both a
	// revision and a filename rather than choose between them.
	testutil.WriteFile(t, filepath.Join(repo, "HEAD~1"), "a file with an awkward name\n")

	if err := Uncommit(open(t, repo), io.Discard); err != nil {
		t.Fatal(err)
	}

	if head := testutil.Git(t, repo, "rev-parse", "HEAD"); head != before {
		t.Fatalf("the branch is at %s, want %s", head, before)
	}
}

func TestUncommitWhenGitShowsSignatures(t *testing.T) {
	repo := testutil.NewRepo(t)
	before := testutil.Git(t, repo, "rev-parse", "HEAD")
	testutil.Git(t, repo, "update-ref", "refs/heads/main", signedCommit(t, repo, before))
	short := testutil.Git(t, repo, "rev-parse", "--short", "HEAD")
	showSignatures(t, repo)

	var progress strings.Builder
	if err := Uncommit(open(t, repo), &progress); err != nil {
		t.Fatal(err)
	}

	if head := testutil.Git(t, repo, "rev-parse", "HEAD"); head != before {
		t.Fatalf("the branch is at %s, want %s", head, before)
	}
	// Matched whole: what gpg says lands in the field %P belongs to, so its
	// absence alone would not tell that the fields still line up.
	if line := progress.String(); line != "holt: took back "+short+" signed subject\n" {
		t.Errorf("the line is %q, want the signed commit named and nothing else", line)
	}
}

func TestUncommitFirstCommitWhenGitShowsSignatures(t *testing.T) {
	repo := testutil.NewRepo(t)
	// A signed commit with no parent: what gpg says lands where %P belongs, and
	// read as a parent it would carry the reset through to git's own refusal.
	testutil.Git(t, repo, "update-ref", "refs/heads/main", signedCommit(t, repo, ""))
	showSignatures(t, repo)

	err := Uncommit(open(t, repo), io.Discard)

	if err == nil {
		t.Fatal("the commit with nothing behind it was taken off the branch")
	}
	if !strings.Contains(err.Error(), "nothing behind it") {
		t.Errorf("error %q does not say what is wrong", err)
	}
	if strings.Contains(err.Error(), "HEAD~1") {
		t.Errorf("error %q quotes git about an argument the user never wrote", err)
	}
}

// A commit carrying a signature for git to hand to gpg, written out by hand:
// the porcelain signs only with a key the machine actually holds. An empty
// parent makes it the first commit of a history.
func signedCommit(t *testing.T, repo, parent string) string {
	t.Helper()
	body := "tree " + testutil.Git(t, repo, "rev-parse", "HEAD^{tree}") + "\n"
	if parent != "" {
		body += "parent " + parent + "\n"
	}
	body += "author holt <holt@example.com> 1700000000 +0000\n" +
		"committer holt <holt@example.com> 1700000000 +0000\n" +
		"gpgsig -----BEGIN PGP SIGNATURE-----\n \n not a real signature\n -----END PGP SIGNATURE-----\n" +
		"\nsigned subject\n"

	object := filepath.Join(t.TempDir(), "commit")
	testutil.WriteFile(t, object, body)
	return testutil.Git(t, repo, "hash-object", "-w", "-t", "commit", object)
}

// gpg writes its verification to its own stderr, which git repeats on stdout
// ahead of anything a --format asked for. The stand-in keeps the test off
// whatever gpg the machine happens to have.
func showSignatures(t *testing.T, repo string) {
	t.Helper()
	gpg := filepath.Join(t.TempDir(), "gpg")
	testutil.WriteFile(t, gpg, "#!/bin/sh\necho 'gpg: fake verification noise' >&2\nexit 1\n")
	if err := os.Chmod(gpg, 0o755); err != nil {
		t.Fatal(err)
	}
	testutil.Git(t, repo, "config", "gpg.program", gpg)
	testutil.Git(t, repo, "config", "log.showSignature", "true")
}
