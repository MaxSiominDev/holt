package worktree

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MaxSiominDev/holt/internal/testutil"
)

func TestWorktreePath(t *testing.T) {
	tests := []struct {
		name   string
		main   string
		branch string
		want   string
	}{
		{
			name: "beside the main checkout",
			main: filepath.Join("/code", "project"), branch: "feature",
			want: filepath.Join("/code", "project-worktrees", "feature"),
		},
		{
			name: "a branch name with a slash nests",
			main: filepath.Join("/code", "project"), branch: "PROJ-1/fix",
			want: filepath.Join("/code", "project-worktrees", "PROJ-1", "fix"),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := worktreePath(test.main, test.branch); got != test.want {
				t.Fatalf("got %q, want %q", got, test.want)
			}
		})
	}
}

func TestCreateHeadIsNotTakenForARemoteBranch(t *testing.T) {
	clone, _ := testutil.NewClonedRepo(t)
	// refs/remotes/origin/HEAD is in any clone; read as a branch, holt fetches a ref
	// origin lacks and blames a pruning that would change nothing.
	if _, err := Create(open(t, clone), "HEAD", CreateOptions{}, io.Discard); err == nil {
		t.Fatal("a worktree was made for a name git will not take as a branch")
	} else if strings.Contains(err.Error(), "fetch --prune") {
		t.Errorf("error %q sends the user to a prune that changes nothing", err)
	}
}

func TestCreateFetchesDefault(t *testing.T) {
	clone, origin := testutil.NewClonedRepo(t)
	// A commit the clone has never seen.
	upstream := testutil.CommitTo(t, origin, "upstream.txt", "added after the clone\n")

	path, err := Create(open(t, clone), "feature", CreateOptions{}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}

	if want := filepath.Join(filepath.Dir(clone), filepath.Base(clone)+"-worktrees", "feature"); path != want {
		t.Fatalf("got %q, want %q", path, want)
	}
	// Branching off a stale origin/main would miss it.
	if head := testutil.Git(t, path, "rev-parse", "HEAD"); head != upstream {
		t.Fatalf("the new branch is at %s, want the freshly fetched %s", head, upstream)
	}
}

func TestCreateStaleOriginHead(t *testing.T) {
	clone, origin := testutil.NewClonedRepo(t)
	current := testutil.Git(t, clone, "rev-parse", "origin/main")
	// origin/HEAD still names the default from clone time, and still resolves.
	testutil.AddRemoteBranch(t, clone, "initial-pr")
	testutil.Git(t, clone, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/initial-pr")
	testutil.Git(t, origin, "branch", "initial-pr")
	testutil.CommitTo(t, origin, "moved-on.txt", "main has moved on\n")

	path, err := Create(open(t, clone), "feature", CreateOptions{}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}

	base := testutil.Git(t, path, "rev-parse", "HEAD")
	if base == current {
		t.Fatal("the branch was built on the stale origin/HEAD")
	}
	if base != testutil.Git(t, origin, "rev-parse", "main") {
		t.Fatalf("the branch is at %s, want the tip of origin's real default branch", base)
	}
}

func TestCreateLeavesMainCheckout(t *testing.T) {
	clone, _ := testutil.NewClonedRepo(t)
	testutil.Git(t, clone, "switch", "--quiet", "--create", "work-in-progress")
	testutil.WriteFile(t, filepath.Join(clone, "README.md"), "half-finished edit\n")

	if _, err := Create(open(t, clone), "feature", CreateOptions{}, io.Discard); err != nil {
		t.Fatal(err)
	}

	// The shell script holt replaced checked the default branch out here first.
	if branch := testutil.Git(t, clone, "rev-parse", "--abbrev-ref", "HEAD"); branch != "work-in-progress" {
		t.Errorf("the main checkout moved to %q", branch)
	}
	content, err := os.ReadFile(filepath.Join(clone, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "half-finished edit\n" {
		t.Errorf("the uncommitted edit was changed, the file holds %q", content)
	}
}

func TestCreateExistingBranch(t *testing.T) {
	clone, _ := testutil.NewClonedRepo(t)
	testutil.Git(t, clone, "switch", "--quiet", "--create", "existing")
	kept := testutil.CommitTo(t, clone, "work.txt", "work done earlier\n")
	testutil.Git(t, clone, "switch", "--quiet", "main")

	path, err := Create(open(t, clone), "existing", CreateOptions{}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}

	if head := testutil.Git(t, path, "rev-parse", "HEAD"); head != kept {
		t.Fatalf("the worktree is at %s, want the branch's own commit %s", head, kept)
	}
}

func TestAddNewBranchKeepsForeignBranch(t *testing.T) {
	clone, _ := testutil.NewClonedRepo(t)
	// The window Create cannot close: something made the branch between the look
	// and "worktree add".
	testutil.Git(t, clone, "switch", "--quiet", "--create", "precious")
	head := testutil.CommitTo(t, clone, "work.txt", "exists nowhere else\n")
	testutil.Git(t, clone, "switch", "--quiet", "main")

	path := filepath.Join(t.TempDir(), "wt")
	err := addNewBranch(open(t, clone), "precious", path, io.Discard,
		"-b", "precious", path, "origin/main")

	if err == nil {
		t.Fatal("worktree add was expected to refuse over the existing branch")
	}
	if !localBranchExists(open(t, clone), "precious") {
		t.Fatal("a branch holt did not create was deleted")
	}
	if got := testutil.Git(t, clone, "rev-parse", "refs/heads/precious"); got != head {
		t.Fatalf("the branch is at %s, want the commit it held %s", got, head)
	}
}

func TestCreateBlockedParentLeavesNoBranch(t *testing.T) {
	clone, _ := testutil.NewClonedRepo(t)
	// A file where the worktrees directory belongs, so the occupied-path guard holds
	// its fire and git reaches the branch before failing.
	testutil.WriteFile(t, filepath.Join(filepath.Dir(clone), filepath.Base(clone)+"-worktrees"), "in the way\n")

	_, err := Create(open(t, clone), "feature", CreateOptions{}, io.Discard)

	if err == nil {
		t.Fatal("a worktree was created under a file")
	}
	if localBranchExists(open(t, clone), "feature") {
		t.Error("a branch was left behind by the failed attempt")
	}
}

func TestCreateSingleBranchClone(t *testing.T) {
	_, origin := testutil.NewClonedRepo(t)
	testutil.Git(t, origin, "branch", "release")
	root := filepath.Dir(origin)
	single := filepath.Join(root, "single")
	// Covers only "release", so fetching main by name leaves no origin/main.
	testutil.Git(t, root, "clone", "--quiet", "--single-branch", "--branch", "release", origin, single)

	path, err := Create(open(t, single), "feature", CreateOptions{}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}

	want := testutil.Git(t, origin, "rev-parse", "main")
	if head := testutil.Git(t, path, "rev-parse", "HEAD"); head != want {
		t.Fatalf("the branch is at %s, want the tip of origin's default branch %s", head, want)
	}
}

func TestCreateNewBranchDoesNotTrack(t *testing.T) {
	clone, _ := testutil.NewClonedRepo(t)

	path, err := Create(open(t, clone), "feature", CreateOptions{}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}

	// branch.autoSetupMerge would aim a fresh branch at origin/<default>, so a bare
	// "git pull" merges the default branch into the work.
	if _, err := open(t, path).Output("rev-parse", "--abbrev-ref", "feature@{upstream}"); err == nil {
		t.Fatal("a branch started from the default branch was set to track it")
	}
}

func TestCreateTracksWithoutAutoSetup(t *testing.T) {
	clone, origin := testutil.NewClonedRepo(t)
	// autoSetupMerge would set the upstream itself and hide whether holt asked.
	testutil.Git(t, clone, "config", "branch.autoSetupMerge", "false")
	testutil.Git(t, origin, "switch", "--quiet", "--create", "colleague")
	testutil.CommitTo(t, origin, "theirs.txt", "someone else's work\n")
	testutil.Git(t, origin, "switch", "--quiet", "main")
	testutil.Git(t, clone, "fetch", "--quiet", "origin", "colleague")

	path, err := Create(open(t, clone), "colleague", CreateOptions{}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}

	upstream := testutil.Git(t, path, "rev-parse", "--abbrev-ref", "colleague@{upstream}")
	if upstream != "origin/colleague" {
		t.Fatalf("the branch tracks %q, want origin/colleague", upstream)
	}
}

func TestCreateTracksRemoteBranch(t *testing.T) {
	clone, origin := testutil.NewClonedRepo(t)
	// A colleague's branch, known here but with no local branch of its own.
	testutil.Git(t, origin, "switch", "--quiet", "--create", "colleague")
	theirWork := testutil.CommitTo(t, origin, "theirs.txt", "someone else's work\n")
	testutil.Git(t, origin, "switch", "--quiet", "main")
	testutil.Git(t, clone, "fetch", "--quiet", "origin", "colleague")

	path, err := Create(open(t, clone), "colleague", CreateOptions{}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}

	if head := testutil.Git(t, path, "rev-parse", "HEAD"); head != theirWork {
		t.Fatalf("the worktree is at %s, want the colleague's commit %s", head, theirWork)
	}
	upstream := testutil.Git(t, path, "rev-parse", "--abbrev-ref", "colleague@{upstream}")
	if upstream != "origin/colleague" {
		t.Fatalf("the branch tracks %q, want origin/colleague", upstream)
	}
}

func TestCreateFetchesRemoteBranch(t *testing.T) {
	clone, origin := testutil.NewClonedRepo(t)
	testutil.Git(t, origin, "switch", "--quiet", "--create", "colleague")
	testutil.CommitTo(t, origin, "theirs.txt", "their first commit\n")
	testutil.Git(t, clone, "fetch", "--quiet", "origin", "colleague")
	// They keep working after our last fetch.
	newest := testutil.CommitTo(t, origin, "theirs-again.txt", "their second commit\n")
	testutil.Git(t, origin, "switch", "--quiet", "main")

	path, err := Create(open(t, clone), "colleague", CreateOptions{}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}

	if head := testutil.Git(t, path, "rev-parse", "HEAD"); head != newest {
		t.Fatalf("the worktree is at %s, want the freshly fetched %s", head, newest)
	}
}

func TestCreateDroppedRemoteBranch(t *testing.T) {
	clone, origin := testutil.NewPushableClone(t)
	testutil.Git(t, clone, "switch", "--quiet", "--create", "done-branch")
	testutil.CommitTo(t, clone, "work.txt", "merged long ago\n")
	testutil.Git(t, clone, "push", "--quiet", "origin", "done-branch")
	testutil.Git(t, clone, "switch", "--quiet", "main")
	// What "holt rm" leaves after a merge, and normal without remote.origin.prune.
	testutil.Git(t, clone, "branch", "--delete", "--force", "done-branch")
	testutil.Git(t, origin, "update-ref", "-d", "refs/heads/done-branch")

	_, err := Create(open(t, clone), "done-branch", CreateOptions{}, io.Discard)

	if err == nil {
		t.Fatal("a worktree was created from a branch origin no longer has")
	}
	// git only says "couldn't find remote ref", with no way out.
	if !strings.Contains(err.Error(), "git fetch --prune") {
		t.Errorf("error %q does not say how to get out of it", err)
	}
}

func TestCreateOccupiedPath(t *testing.T) {
	clone, _ := testutil.NewClonedRepo(t)
	occupied := filepath.Join(filepath.Dir(clone), filepath.Base(clone)+"-worktrees", "feature")
	testutil.WriteFile(t, filepath.Join(occupied, "something.txt"), "in the way\n")

	_, err := Create(open(t, clone), "feature", CreateOptions{}, io.Discard)

	if err == nil {
		t.Fatal("a worktree was created over an occupied directory")
	}
	// git refuses too; the guard buys holt looking first and saying it usefully.
	if !strings.Contains(err.Error(), "already exists, so there is nowhere to put the worktree") {
		t.Errorf("error %q is git's rather than holt's", err)
	}
	// Cheap to check, though the guard fires before git adds anything; the rollback
	// case is TestCreateBlockedParentLeavesNoBranch.
	if localBranchExists(open(t, clone), "feature") {
		t.Error("a branch was left behind by the failed attempt")
	}
}

func TestCreateNoFetchWithoutOrigin(t *testing.T) {
	clone, origin := testutil.NewClonedRepo(t)
	// Removing the origin is the only proof holt did not reach for it.
	if err := os.RemoveAll(origin); err != nil {
		t.Fatal(err)
	}

	path, err := Create(open(t, clone), "feature", CreateOptions{SkipFetch: true}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

func TestCreateNoFetchLocalDefault(t *testing.T) {
	clone, origin := testutil.NewClonedRepo(t)
	local := testutil.Git(t, clone, "rev-parse", "origin/main")
	// --no-fetch must not pick this up.
	testutil.CommitTo(t, origin, "upstream.txt", "added after the clone\n")

	path, err := Create(open(t, clone), "feature", CreateOptions{SkipFetch: true}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}

	if head := testutil.Git(t, path, "rev-parse", "HEAD"); head != local {
		t.Fatalf("the branch is at %s, want the local origin/main at %s", head, local)
	}
}

func TestCreateNoFetchNoDefault(t *testing.T) {
	main := testutil.NewRepo(t)

	_, err := Create(open(t, main), "feature", CreateOptions{SkipFetch: true}, io.Discard)

	if err == nil {
		t.Fatal("a branch was created off a ref that does not exist")
	}
	// git echoes origin/main back too, so only holt's wording proves anything.
	if !strings.Contains(err.Error(), "no origin remote") {
		t.Errorf("error %q is git's, not holt's", err)
	}
}

func TestCreateExistingBranchOffline(t *testing.T) {
	clone, origin := testutil.NewClonedRepo(t)
	testutil.Git(t, clone, "switch", "--quiet", "--create", "existing")
	kept := testutil.CommitTo(t, clone, "work.txt", "work done earlier\n")
	testutil.Git(t, clone, "switch", "--quiet", "main")
	// The branch has its own history, so fetching buys nothing.
	if err := os.RemoveAll(origin); err != nil {
		t.Fatal(err)
	}

	path, err := Create(open(t, clone), "existing", CreateOptions{}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}

	if head := testutil.Git(t, path, "rev-parse", "HEAD"); head != kept {
		t.Fatalf("the worktree is at %s, want the branch's own commit %s", head, kept)
	}
}

func TestCreateSurvivesFailingForeignHook(t *testing.T) {
	clone, _ := testutil.NewClonedRepo(t)
	// A hook of the user's own, as husky installs: git runs it after the worktree is
	// made and registered, then passes its status on.
	testutil.WriteFile(t, filepath.Join(clone, ".git", "hooks", "post-checkout"), "#!/bin/sh\nexit 1\n")
	if err := os.Chmod(filepath.Join(clone, ".git", "hooks", "post-checkout"), 0o755); err != nil {
		t.Fatal(err)
	}

	path, err := Create(open(t, clone), "feature", CreateOptions{SkipFetch: true}, io.Discard)

	if err != nil {
		t.Fatalf("holt called a worktree it created a failure: %v", err)
	}
	// Without the path the shell function has nowhere to move the user to.
	if path == "" {
		t.Fatal("holt printed no path for a worktree that exists")
	}
	if _, statErr := os.Stat(filepath.Join(path, "README.md")); statErr != nil {
		t.Errorf("the worktree is not usable: %v", statErr)
	}
	if !localBranchExists(open(t, clone), "feature") {
		t.Error("the branch git created and checked out was deleted")
	}
}

func TestCreateSurvivesFailingForeignHookUnderASymlinkedRoot(t *testing.T) {
	clone, _ := testutil.NewClonedRepo(t)
	testutil.WriteFile(t, filepath.Join(clone, ".git", "hooks", "post-checkout"), "#!/bin/sh\nexit 1\n")
	if err := os.Chmod(filepath.Join(clone, ".git", "hooks", "post-checkout"), 0o755); err != nil {
		t.Fatal(err)
	}
	// The worktrees root reached through a symlink, a disk or a synced directory:
	// git records it resolved, holt names it through the link, and text never matches.
	elsewhere := filepath.Join(t.TempDir(), "real")
	if err := os.Mkdir(elsewhere, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, worktreesRoot(clone)); err != nil {
		t.Fatal(err)
	}

	path, err := Create(open(t, clone), "feature", CreateOptions{SkipFetch: true}, io.Discard)

	if err != nil {
		t.Fatalf("holt called a worktree it created a failure: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(path, "README.md")); statErr != nil {
		t.Errorf("the worktree is not usable: %v", statErr)
	}
}

func TestCreateQuotesTheBranchNameInTheAdviceItPrints(t *testing.T) {
	clone, origin := testutil.NewClonedRepo(t)
	testutil.Git(t, origin, "branch", "feat&fix")
	// A single-branch clone, which the advice is for, and a branch name holding a
	// character the shell acts on, which git allows and holt made itself.
	testutil.Git(t, clone, "config", "--replace-all", "remote.origin.fetch",
		"+refs/heads/main:refs/remotes/origin/main")
	testutil.AddRemoteBranch(t, clone, "feat&fix")

	var progress strings.Builder
	if _, err := Create(open(t, clone), "feat&fix", CreateOptions{SkipFetch: true}, &progress); err != nil {
		t.Fatal(err)
	}

	// Unquoted, the shell takes the line apart and runs something else.
	if !strings.Contains(progress.String(), `--set-upstream-to='origin/feat&fix' 'feat&fix'`) {
		t.Errorf("progress %q hands back a command the shell would take apart", progress.String())
	}
}

func TestCreateRefusesABranchNameReadAsAFlag(t *testing.T) {
	clone, _ := testutil.NewClonedRepo(t)
	repo := open(t, clone)

	_, err := Create(repo, "-f", CreateOptions{SkipFetch: true}, io.Discard)

	if err == nil {
		t.Fatal("a name git will not have as a branch was accepted")
	}
	// "worktree add -b" runs "git branch", where "-f" is swallowed and the start point
	// becomes the branch: the local refs/remotes/origin/main left behind makes every
	// later start point ambiguous, and the cleanup finds no branch called "-f" to undo.
	out, listErr := repo.Output("branch", "--list", "--format=%(refname)")
	if listErr != nil {
		t.Fatal(listErr)
	}
	if strings.Contains(out, "refs/heads/refs/remotes/") {
		t.Errorf("branches are now:\n%s", out)
	}
}

func TestCreateNamesTheDirectoryThatIsReallyThere(t *testing.T) {
	clone, _ := testutil.NewClonedRepo(t)
	made, err := Create(open(t, clone), "feature", CreateOptions{SkipFetch: true}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Lstat(filepath.Join(filepath.Dir(made), "FEATURE")); statErr != nil {
		t.Skip("this filesystem tells the two names apart")
	}

	_, err = Create(open(t, clone), "FEATURE", CreateOptions{SkipFetch: true}, io.Discard)

	if err == nil {
		t.Fatal("two worktrees were made in one directory")
	}
	// The refusal is right, the directory being unmakeable; naming FEATURE would send
	// the user looking for what "ls" says is not there.
	if !strings.Contains(err.Error(), "feature") || strings.Contains(err.Error(), "FEATURE") {
		t.Errorf("error %q names the spelling asked for rather than the one on disk", err)
	}
}

func TestCreateNamesTheOfflineModeWhenOriginCannotBeRead(t *testing.T) {
	clone, _ := testutil.NewClonedRepo(t)
	testutil.Git(t, clone, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "gone.git"))

	var progress strings.Builder
	_, err := Create(open(t, clone), "feature", CreateOptions{}, &progress)

	if err == nil {
		t.Fatal("holt reached an origin that is not there")
	}
	// git's message is about the remote and never says holt can make the branch anyway.
	if !strings.Contains(progress.String(), "--no-fetch") {
		t.Errorf("progress %q leaves out the mode that works offline", progress.String())
	}
}

func TestCreateKeepsQuietAboutTheOfflineModeWhereItWouldFailToo(t *testing.T) {
	clone, _ := testutil.NewClonedRepo(t)
	testutil.Git(t, clone, "symbolic-ref", "--delete", "refs/remotes/origin/HEAD")
	// Nothing names a default branch offline, so "--no-fetch" would fail again.
	for _, ref := range []string{"main", "master"} {
		_, _ = open(t, clone).Output("update-ref", "-d", "refs/remotes/origin/"+ref)
	}
	testutil.Git(t, clone, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "gone.git"))

	var progress strings.Builder
	_, err := Create(open(t, clone), "feature", CreateOptions{}, &progress)

	if err == nil {
		t.Fatal("holt reached an origin that is not there")
	}
	if strings.Contains(progress.String(), "--no-fetch") {
		t.Errorf("progress %q offers a way out that fails as well", progress.String())
	}
}

func TestCreateReportsMissingButRegisteredWorktree(t *testing.T) {
	clone, _ := testutil.NewClonedRepo(t)
	path, err := Create(open(t, clone), "feature", CreateOptions{SkipFetch: true}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	// git keeps the registration and refuses with a way out; reporting success would
	// send the shell into a path that is gone.
	if err := os.RemoveAll(path); err != nil {
		t.Fatal(err)
	}

	var progress strings.Builder
	_, err = Create(open(t, clone), "feature", CreateOptions{SkipFetch: true}, &progress)

	if err == nil {
		t.Fatal("holt reported success for a worktree git refused to make")
	}
	// git names the way out, and holt forwards it to stderr for the user.
	if !strings.Contains(progress.String(), "registered") {
		t.Errorf("progress %q loses the remedy git names", progress.String())
	}
}

func TestCreateChecksOutBranchPushedSinceLastFetch(t *testing.T) {
	clone, origin := testutil.NewClonedRepo(t)
	theirs := testutil.CommitTo(t, origin, "theirs.txt", "their work\n")
	// A colleague's branch made since the last fetch, with no tracking ref here yet.
	testutil.Git(t, origin, "branch", "shared-work", theirs)

	path, err := Create(open(t, clone), "shared-work", CreateOptions{}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}

	// Started off the default branch, their work would be missing and git's refused
	// push would be the first word of it.
	if _, statErr := os.Stat(filepath.Join(path, "theirs.txt")); statErr != nil {
		t.Errorf("the branch was started afresh rather than checked out: %v", statErr)
	}
	if upstream, _, _ := open(t, clone).Config("branch.shared-work.merge"); upstream != "refs/heads/shared-work" {
		t.Errorf("upstream is %q, want the branch on origin", upstream)
	}
}

func TestCreateWithoutTrackingInSingleBranchClone(t *testing.T) {
	clone, origin := testutil.NewClonedRepo(t)
	theirs := testutil.CommitTo(t, origin, "theirs.txt", "their work\n")
	testutil.Git(t, origin, "branch", "shared-work", theirs)
	// What --single-branch carries: one branch mapped, off which git reads "is this a
	// branch" and refuses --track for every other.
	testutil.Git(t, clone, "config", "remote.origin.fetch", "+refs/heads/main:refs/remotes/origin/main")

	var progress strings.Builder
	path, err := Create(open(t, clone), "shared-work", CreateOptions{}, &progress)
	if err != nil {
		t.Fatalf("the worktree was refused rather than made without an upstream: %v", err)
	}

	// The checkout is what the user asked for; only the upstream cannot be set.
	if _, statErr := os.Stat(filepath.Join(path, "theirs.txt")); statErr != nil {
		t.Errorf("their work is missing: %v", statErr)
	}
	// Both commands, widening first: it sets no upstream on the branch just made, and
	// until it runs git refuses to set one by hand.
	widen := `git config --add remote.origin.fetch '+refs/heads/*:refs/remotes/origin/*'`
	track := `git branch --set-upstream-to='origin/shared-work' 'shared-work'`
	note := progress.String()
	widenAt, trackAt := strings.Index(note, widen), strings.Index(note, track)
	if widenAt < 0 || trackAt < 0 || widenAt > trackAt {
		t.Errorf("progress %q does not name both commands with the widening first, so following it does not get an upstream", note)
	}
}

func TestCreateTracksABranchOriginSpellsDifferently(t *testing.T) {
	clone, origin := testutil.NewClonedRepo(t)
	decomposed := "fe\u0301ature"
	composed := "f\u00e9ature"
	// git precomposes what it is given, so origin's answer compared as text against
	// the user's own spelling looks like another branch, and holt cuts a fresh one.
	testutil.Git(t, origin, "switch", "--quiet", "--create", decomposed)
	if stored := testutil.Git(t, origin, "for-each-ref", "--format=%(refname:short)", "refs/heads/"+decomposed); stored != composed {
		t.Skipf("git stored the name as %q, so this platform does not precompose", stored)
	}
	// Only on their branch, so branching off the default is visibly the wrong answer.
	wanted := testutil.CommitTo(t, origin, "theirs.txt", "their work\n")
	testutil.Git(t, origin, "switch", "--quiet", "main")

	path, err := Create(open(t, clone), decomposed, CreateOptions{}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}

	if head := testutil.Git(t, path, "rev-parse", "HEAD"); head != wanted {
		t.Errorf("the worktree is at %s, want origin's %s: holt cut a new branch instead of taking theirs", head, wanted)
	}
	if upstream := testutil.Git(t, path, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}"); upstream != "origin/"+composed {
		t.Errorf("the branch tracks %q, want origin/%s", upstream, composed)
	}
}

func TestCreateDoesNotAskOriginWithAGlob(t *testing.T) {
	clone, _ := testutil.NewClonedRepo(t)

	// ls-remote reads its argument as a pattern, so "mai*" takes main for the named
	// branch, and holt then fetches and fails on the glob rather than on the name.
	_, err := Create(open(t, clone), "mai*", CreateOptions{}, io.Discard)

	if err == nil {
		t.Fatal("a name git will not take as a branch produced a worktree")
	}
	if strings.Contains(err.Error(), "refs/remotes/origin/mai*") {
		t.Errorf("error %q takes a pattern for a branch origin has", err)
	}
}

func TestCreateIgnoresBranchMatchingOnlyByTail(t *testing.T) {
	clone, origin := testutil.NewClonedRepo(t)
	// ls-remote matches a name's tail at a slash, so refs/heads/bar answers for this
	// one too, and holt would fetch a ref origin does not have.
	testutil.AddRemoteBranch(t, origin, "foo/refs/heads/bar")

	path, err := Create(open(t, clone), "bar", CreateOptions{}, io.Discard)
	if err != nil {
		t.Fatalf("a branch origin does not have was taken for one it does: %v", err)
	}

	if upstream, set, _ := open(t, clone).Config("branch.bar.merge"); set {
		t.Errorf("upstream is %q, want a branch of the user's own", upstream)
	}
	if _, statErr := os.Stat(path); statErr != nil {
		t.Errorf("no worktree was made: %v", statErr)
	}
}

func TestCreateWithoutTrackingWhenRefspecAimsElsewhere(t *testing.T) {
	clone, origin := testutil.NewClonedRepo(t)
	theirs := testutil.CommitTo(t, origin, "theirs.txt", "their work\n")
	testutil.Git(t, origin, "branch", "shared-work", theirs)
	// The refspec reads every branch but writes outside refs/remotes/origin, and git
	// goes by what a refspec produces, so the read side is the wrong question.
	testutil.Git(t, clone, "config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/elsewhere/*")

	path, err := Create(open(t, clone), "shared-work", CreateOptions{}, io.Discard)
	if err != nil {
		t.Fatalf("the worktree was refused rather than made without an upstream: %v", err)
	}

	if _, statErr := os.Stat(filepath.Join(path, "theirs.txt")); statErr != nil {
		t.Errorf("their work is missing: %v", statErr)
	}
}

func TestCreateWithoutTrackingWhenNoRefspecIsSet(t *testing.T) {
	clone, origin := testutil.NewClonedRepo(t)
	theirs := testutil.CommitTo(t, origin, "theirs.txt", "their work\n")
	testutil.Git(t, origin, "branch", "shared-work", theirs)
	testutil.Git(t, clone, "fetch", "--quiet", "origin", "refs/heads/shared-work:refs/remotes/origin/shared-work")
	// Nothing produces a remote-tracking ref, so nothing can be tracked whatever
	// refs are lying about.
	testutil.Git(t, clone, "config", "--unset-all", "remote.origin.fetch")

	path, err := Create(open(t, clone), "shared-work", CreateOptions{SkipFetch: true}, io.Discard)
	if err != nil {
		t.Fatalf("the worktree was refused rather than made without an upstream: %v", err)
	}

	if _, statErr := os.Stat(filepath.Join(path, "theirs.txt")); statErr != nil {
		t.Errorf("their work is missing: %v", statErr)
	}
}

func TestCreateWithoutTrackingWhenRefspecExcludesTheBranch(t *testing.T) {
	clone, origin := testutil.NewClonedRepo(t)
	theirs := testutil.CommitTo(t, origin, "theirs.txt", "their work\n")
	testutil.Git(t, origin, "branch", "shared-work", theirs)
	// A leading ^ means "not this one" and carries no destination, so reading only
	// destinations passes it over and git is handed a --track that kills the add.
	testutil.Git(t, clone, "config", "--add", "remote.origin.fetch", "^refs/heads/shared*")

	var progress strings.Builder
	path, err := Create(open(t, clone), "shared-work", CreateOptions{}, &progress)
	if err != nil {
		t.Fatalf("the worktree was refused rather than made without an upstream: %v", err)
	}

	if _, statErr := os.Stat(filepath.Join(path, "theirs.txt")); statErr != nil {
		t.Errorf("their work is missing: %v", statErr)
	}
	if !strings.Contains(progress.String(), "^refs/heads/shared*") {
		t.Errorf("progress %q does not name the line that excludes the branch", progress.String())
	}
	// Taking the line out sets no upstream on the branch just made, so naming only
	// that leaves the user a command short.
	if !strings.Contains(progress.String(), `git branch --set-upstream-to='origin/shared-work' 'shared-work'`) {
		t.Errorf("progress %q stops before the command that gets an upstream", progress.String())
	}
	// Widening does not undo an exclusion, so the single-branch advice would not help here.
	if strings.Contains(progress.String(), "git config --add remote.origin.fetch") {
		t.Errorf("progress %q offers widening, which an exclusion survives", progress.String())
	}
}

func TestCreateNamesEveryLineExcludingTheBranch(t *testing.T) {
	clone, origin := testutil.NewClonedRepo(t)
	theirs := testutil.CommitTo(t, origin, "theirs.txt", "their work\n")
	testutil.Git(t, origin, "branch", "shared-work", theirs)
	// Two lines both covering the branch: taking out the one named leaves the other.
	testutil.Git(t, clone, "config", "--add", "remote.origin.fetch", "^refs/heads/shared*")
	testutil.Git(t, clone, "config", "--add", "remote.origin.fetch", "^refs/heads/shared-work")

	var progress strings.Builder
	if _, err := Create(open(t, clone), "shared-work", CreateOptions{}, &progress); err != nil {
		t.Fatal(err)
	}

	for _, line := range []string{"^refs/heads/shared*", "^refs/heads/shared-work"} {
		if !strings.Contains(progress.String(), line) {
			t.Errorf("progress %q does not name %s, which also excludes the branch", progress.String(), line)
		}
	}
}
