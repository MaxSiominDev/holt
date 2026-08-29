package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MaxSiominDev/holt/internal/testutil"
)

func TestRemoveRefusesCurrent(t *testing.T) {
	main := testutil.NewRepo(t)
	feature := testutil.AddWorktree(t, main, "feature")
	t.Chdir(feature)

	// git would delete it and leave the shell in a directory that is gone.
	err := runHoltExpectingFailure(t, "rm", "feature")

	if !strings.Contains(err.Error(), "holt home") {
		t.Errorf("error %q does not say how to get out first", err)
	}
	if _, statErr := os.Stat(feature); statErr != nil {
		t.Error("the worktree was removed anyway")
	}
}

func TestRemoveRefusesFromSubdirectory(t *testing.T) {
	main := testutil.NewRepo(t)
	feature := testutil.AddWorktree(t, main, "feature")
	nested := filepath.Join(feature, "deep", "nested")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(nested)

	runHoltExpectingFailure(t, "rm", "feature")

	if _, err := os.Stat(feature); err != nil {
		t.Error("the worktree the shell was standing in was removed")
	}
}

func TestRemoveDeletesSiblingPrefix(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.AddWorktree(t, main, "feature")
	sibling := testutil.AddWorktree(t, main, "feature-2")
	t.Chdir(sibling)

	// Compared as strings, feature-2, where the shell stands, sits inside feature.
	runHolt(t, "rm", "feature")

	if _, err := os.Stat(filepath.Join(filepath.Dir(sibling), "feature")); !os.IsNotExist(err) {
		t.Error("holt reported success but left the worktree behind")
	}
}

func TestRemoveRefusesMainCheckout(t *testing.T) {
	main := testutil.NewRepo(t)
	t.Chdir(main)

	err := runHoltExpectingFailure(t, "rm", "main")

	if !strings.Contains(err.Error(), "main checkout") {
		t.Errorf("error %q does not say why", err)
	}
}

func TestRemoveNamesIgnoredFiles(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.WriteFile(t, filepath.Join(main, ".gitignore"), ".env\n")
	testutil.Git(t, main, "add", ".gitignore")
	testutil.Git(t, main, "commit", "-m", "ignore .env")
	feature := testutil.AddWorktree(t, main, "feature")
	// git takes this with the worktree, and nothing else has a copy of it.
	testutil.WriteFile(t, filepath.Join(feature, ".env"), "SECRET_TOKEN=hunter2\n")
	t.Chdir(main)

	stdout, _ := runHolt(t, "rm", "feature")

	if !strings.Contains(stdout, ".env") {
		t.Errorf("stdout %q never names the ignored file that went with the worktree", stdout)
	}
}

func TestRemoveNamesAnIgnoredLinkPointingInsideTheWorktree(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.WriteFile(t, filepath.Join(main, ".gitignore"), "*.env\nsub/\n")
	testutil.Git(t, main, "add", ".gitignore")
	testutil.Git(t, main, "commit", "-m", "ignore env")
	feature := testutil.AddWorktree(t, main, "feature")
	testutil.WriteFile(t, filepath.Join(feature, "sub", "real.txt"), "the only copy\n")
	// The user's own link, relative and into the worktree as a person writes one:
	// both go with the worktree, so both belong in the warning.
	if err := os.Symlink(filepath.Join("sub", "real.txt"), filepath.Join(feature, "local.env")); err != nil {
		t.Fatal(err)
	}
	t.Chdir(main)

	stdout, _ := runHolt(t, "rm", "feature")

	if !strings.Contains(stdout, "local.env") {
		t.Errorf("stdout %q leaves out a link that leads nowhere else", stdout)
	}
}

func TestRemoveNamesOnlyRealIgnoredFiles(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.WriteFile(t, filepath.Join(main, ".gitignore"), "*.local\n")
	testutil.Git(t, main, "add", ".gitignore")
	testutil.Git(t, main, "commit", "-m", "ignore")
	testutil.WriteFile(t, filepath.Join(main, "CLAUDE.local"), "mirrored\n")
	feature := testutil.AddWorktree(t, main, "feature")
	t.Chdir(main)
	runHolt(t, "mirror", "add", "*.local")
	// A file of the user's own that the glob happens to match, which filtering by
	// pattern would hide, the very harm the warning prevents.
	testutil.WriteFile(t, filepath.Join(feature, "secrets.local"), "TOKEN=real\n")

	stdout, _ := runHolt(t, "rm", "feature")

	if !strings.Contains(stdout, "secrets.local") {
		t.Errorf("stdout %q never names the file with no copy anywhere else", stdout)
	}
	// holt's own symlink leaves its file in the main checkout.
	if strings.Contains(stdout, "CLAUDE.local\n") || strings.Contains(stdout, "CLAUDE.local,") {
		t.Errorf("stdout %q names holt's own symlink among the losses", stdout)
	}
}

func TestRemoveNamesLinkInsideTheWorktree(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.WriteFile(t, filepath.Join(main, ".gitignore"), "*.local\n")
	testutil.Git(t, main, "add", ".gitignore")
	testutil.Git(t, main, "commit", "-m", "ignore")
	feature := testutil.AddWorktree(t, main, "feature")
	// The user's own link into the same worktree: both go when it does, unlike a
	// link out of it.
	testutil.WriteFile(t, filepath.Join(feature, "config", "real.local"), "TOKEN=real\n")
	if err := os.Symlink(filepath.Join(feature, "config", "real.local"), filepath.Join(feature, "linked.local")); err != nil {
		t.Fatal(err)
	}
	t.Chdir(main)

	stdout, _ := runHolt(t, "rm", "feature")

	if !strings.Contains(stdout, "linked.local") {
		t.Errorf("stdout %q never names a link whose file goes with the worktree", stdout)
	}
}

func TestRemoveIgnoredDirectoryOfLinks(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.WriteFile(t, filepath.Join(main, ".gitignore"), ".claude/\n")
	testutil.Git(t, main, "add", ".gitignore")
	testutil.Git(t, main, "commit", "-m", "ignore")
	testutil.WriteFile(t, filepath.Join(main, ".claude", "settings.local.json"), "{}\n")
	testutil.AddWorktree(t, main, "feature")
	// The hook mirrors into the second worktree below and needs a holt to call: left
	// to os.Executable it runs this test binary as a suite that never settles.
	holtOnPath(t)
	t.Chdir(main)
	// ".claude/" matches an ignore pattern itself, so git reports the directory
	// rather than the links inside it.
	runHolt(t, "mirror", "add", ".claude/settings.local.json")

	quiet, _ := runHolt(t, "rm", "feature")

	if strings.Contains(quiet, "which git ignores") {
		t.Errorf("stdout %q warns about a directory holding nothing but holt's links", quiet)
	}

	// The same directory holding something of the user's own as well does go.
	second := testutil.AddWorktree(t, main, "second")
	if _, err := os.Lstat(filepath.Join(second, ".claude", "settings.local.json")); err != nil {
		t.Fatalf("the hook did not mirror into the new worktree, so the directory is not the mixed one: %v", err)
	}
	testutil.WriteFile(t, filepath.Join(second, ".claude", "notes.md"), "written by hand\n")

	loud, _ := runHolt(t, "rm", "second")

	if !strings.Contains(loud, "took 1 path with it, which git ignores: .claude/") {
		t.Errorf("stdout %q says nothing about a file that goes with the worktree", loud)
	}
}

func TestRemoveDeletesMergedBranch(t *testing.T) {
	clone, _ := testutil.NewClonedRepo(t)
	feature := testutil.AddWorktree(t, clone, "feature")
	t.Chdir(clone)

	stdout, _ := runHolt(t, "rm", "feature")

	if _, err := os.Stat(feature); !os.IsNotExist(err) {
		t.Error("the directory is still there")
	}
	if !strings.Contains(stdout, "deleted branch feature") {
		t.Errorf("stdout %q does not report what happened to the branch", stdout)
	}
}

func TestRemoveKeepsUnmergedBranch(t *testing.T) {
	clone, _ := testutil.NewClonedRepo(t)
	feature := testutil.AddWorktree(t, clone, "feature")
	testutil.CommitTo(t, feature, "work.txt", "exists nowhere else\n")
	t.Chdir(clone)

	stdout, _ := runHolt(t, "rm", "feature")

	if !strings.Contains(stdout, "it holds commits the default branch does not") {
		t.Errorf("stdout %q does not report why the branch was kept", stdout)
	}
}

func TestRemoveKeepsUnverifiable(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.AddWorktree(t, main, "feature")
	t.Chdir(main)

	// No origin, so nothing justifies deleting a branch.
	stdout, _ := runHolt(t, "rm", "feature")

	if !strings.Contains(stdout, "no default branch here to check it against") {
		t.Errorf("stdout %q does not say why the branch was kept", stdout)
	}
}

func TestRemoveGoneWorktree(t *testing.T) {
	main := testutil.NewRepo(t)
	feature := testutil.AddWorktree(t, main, "feature")
	if err := os.RemoveAll(feature); err != nil {
		t.Fatal(err)
	}
	t.Chdir(main)

	// "holt ls" reports it gone, so removing it has to work, as plain git does.
	runHolt(t, "rm", "feature")

	if listed := testutil.Git(t, main, "worktree", "list"); strings.Contains(listed, "feature") {
		t.Fatalf("git still lists the worktree:\n%s", listed)
	}
}

func TestRemoveSaysNothingWhenGitRefuses(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.WriteFile(t, filepath.Join(main, ".gitignore"), ".env\n")
	testutil.Git(t, main, "add", ".gitignore")
	testutil.Git(t, main, "commit", "-m", "ignore .env")
	feature := testutil.AddWorktree(t, main, "feature")
	testutil.WriteFile(t, filepath.Join(feature, ".env"), "SECRET_TOKEN=hunter2\n")
	// Enough for "git worktree remove" to refuse without --force.
	testutil.WriteFile(t, filepath.Join(feature, "README.md"), "edited\n")
	t.Chdir(main)

	root, stdout, _ := holtCommand(t, "rm", "feature")
	if err := root.Execute(); err == nil {
		t.Fatal("git accepted a worktree with modified files, the fixture no longer refuses")
	}

	if strings.Contains(stdout.String(), "git ignores") {
		t.Errorf("stdout %q reports a loss that the refusal prevented", stdout)
	}
	if _, err := os.Stat(filepath.Join(feature, ".env")); err != nil {
		t.Errorf("the file holt spoke about is gone: %v", err)
	}
}

func TestRemoveIgnoresEmptyDirectory(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.WriteFile(t, filepath.Join(main, ".gitignore"), ".claude/\n")
	testutil.Git(t, main, "add", ".gitignore")
	testutil.Git(t, main, "commit", "-m", "ignore")
	testutil.WriteFile(t, filepath.Join(main, ".claude", "settings.local.json"), "{}\n")
	feature := testutil.AddWorktree(t, main, "feature")
	t.Chdir(main)
	runHolt(t, "mirror", "add", ".claude/settings.local.json")
	// The directory matches a pattern itself and arrives as one entry, and an empty
	// directory beside holt's links takes nothing with it.
	if err := os.MkdirAll(filepath.Join(feature, ".claude", "cache"), 0o755); err != nil {
		t.Fatal(err)
	}

	stdout, _ := runHolt(t, "rm", "feature")

	if strings.Contains(stdout, "git ignores") {
		t.Errorf("stdout %q warns about a directory holding nothing", stdout)
	}
}

func TestRemoveRefusesWorktreeMidRebase(t *testing.T) {
	clone, origin := testutil.NewClonedRepo(t)
	feature := testutil.AddWorktree(t, clone, "feature")
	testutil.CommitTo(t, feature, "shared.txt", "the branch's version\n")
	testutil.CommitTo(t, origin, "shared.txt", "the default branch's version\n")
	t.Chdir(feature)
	// holt's own doing: a rebase it stopped on a conflict.
	runHoltExpectingFailure(t, "rebase")
	t.Chdir(clone)

	err := runHoltExpectingFailure(t, "rm", "feature")

	if !strings.Contains(err.Error(), "rebase") {
		t.Errorf("error %q does not say what is in the way", err)
	}
	if _, statErr := os.Stat(feature); statErr != nil {
		t.Errorf("the worktree was removed with the rebase inside it: %v", statErr)
	}
}

func TestRemoveShortensALongListOfIgnoredFiles(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.WriteFile(t, filepath.Join(main, ".gitignore"), "*.log\n")
	testutil.Git(t, main, "add", ".gitignore")
	testutil.Git(t, main, "commit", "-m", "ignore logs")
	feature := testutil.AddWorktree(t, main, "feature")
	// Build output alone runs to hundreds of files, and nobody reads such a warning.
	for _, name := range []string{"a", "b", "c", "d", "e", "f", "g"} {
		testutil.WriteFile(t, filepath.Join(feature, name+".log"), "noise\n")
	}
	t.Chdir(main)

	stdout, _ := runHolt(t, "rm", "feature")

	if !strings.Contains(stdout, "and 2 more") {
		t.Errorf("stdout %q lists every one of them", stdout)
	}
}

func TestRemoveRefusesWorktreeMidSingleCommitCherryPick(t *testing.T) {
	clone, _ := testutil.NewClonedRepo(t)
	source := testutil.AddWorktree(t, clone, "source")
	testutil.CommitTo(t, source, "shared.txt", "the other line\n")
	feature := testutil.AddWorktree(t, clone, "feature")
	testutil.CommitTo(t, feature, "shared.txt", "this line\n")
	// One commit rather than a range leaves no sequencer, only the marker, and
	// resolved to what is here the tree is clean and git says nothing.
	testutil.GitStopping(t, feature, "cherry-pick", "source")
	testutil.Git(t, feature, "checkout", "--ours", "shared.txt")
	testutil.Git(t, feature, "add", "shared.txt")
	marker := testutil.Git(t, feature, "rev-parse", "--git-path", "CHERRY_PICK_HEAD")
	if clean := testutil.Git(t, feature, "status", "--porcelain"); clean != "" {
		t.Fatalf("the fixture leaves %q behind, so git would refuse on its own", clean)
	}
	t.Chdir(clone)

	err := runHoltExpectingFailure(t, "rm", "feature")

	if !strings.Contains(err.Error(), "cherry-pick") {
		t.Errorf("error %q does not say what is in the way", err)
	}
	if _, statErr := os.Stat(marker); statErr != nil {
		t.Errorf("the cherry-pick state is gone: %v", statErr)
	}
}

func TestRemoveRefusesWorktreeMidSingleCommitRevert(t *testing.T) {
	clone, _ := testutil.NewClonedRepo(t)
	feature := testutil.AddWorktree(t, clone, "feature")
	testutil.CommitTo(t, feature, "shared.txt", "first\n")
	testutil.CommitTo(t, feature, "shared.txt", "second\n")
	// Reverting under the commit on top conflicts, and resolved to what is here the
	// tree is clean with only REVERT_HEAD left.
	testutil.GitStopping(t, feature, "revert", "HEAD~1")
	testutil.Git(t, feature, "checkout", "--ours", "shared.txt")
	testutil.Git(t, feature, "add", "shared.txt")
	marker := testutil.Git(t, feature, "rev-parse", "--git-path", "REVERT_HEAD")
	if clean := testutil.Git(t, feature, "status", "--porcelain"); clean != "" {
		t.Fatalf("the fixture leaves %q behind, so git would refuse on its own", clean)
	}
	t.Chdir(clone)

	err := runHoltExpectingFailure(t, "rm", "feature")

	if !strings.Contains(err.Error(), "revert") {
		t.Errorf("error %q does not say what is in the way", err)
	}
	if _, statErr := os.Stat(marker); statErr != nil {
		t.Errorf("the revert state is gone: %v", statErr)
	}
}

func TestRemoveRefusesWorktreeMidCherryPick(t *testing.T) {
	clone, _ := testutil.NewClonedRepo(t)
	source := testutil.AddWorktree(t, clone, "source")
	first := testutil.CommitTo(t, source, "one.txt", "one\n")
	testutil.CommitTo(t, source, "two.txt", "two\n")
	feature := testutil.AddWorktree(t, clone, "feature")
	// The first commit is applied already, so replaying the pair stops on it as empty
	// with a clean tree, and git would take the sequencer down without a word.
	testutil.Git(t, feature, "cherry-pick", first)
	testutil.GitStopping(t, feature, "cherry-pick", "main..source")
	// Resolved before the removal: afterwards git cannot run in a directory that is gone.
	state := testutil.Git(t, feature, "rev-parse", "--git-path", "sequencer")
	t.Chdir(clone)

	err := runHoltExpectingFailure(t, "rm", "feature")

	if !strings.Contains(err.Error(), "cherry-pick") {
		t.Errorf("error %q does not say what is in the way", err)
	}
	if _, statErr := os.Stat(state); statErr != nil {
		t.Errorf("the cherry-pick state is gone: %v", statErr)
	}
}

func TestRemoveGuardsTheOperationFromACaseFoldedDirectory(t *testing.T) {
	clone, _ := testutil.NewClonedRepo(t)
	source := testutil.AddWorktree(t, clone, "source")
	for _, content := range []string{"a\n", "b\n"} {
		testutil.WriteFile(t, filepath.Join(source, "shared.txt"), content)
		testutil.Git(t, source, "add", ".")
		testutil.Git(t, source, "commit", "--quiet", "-m", content)
	}
	feature := testutil.AddWorktree(t, clone, "feature")
	testutil.WriteFile(t, filepath.Join(feature, "shared.txt"), "mine\n")
	testutil.Git(t, feature, "add", ".")
	testutil.Git(t, feature, "commit", "--quiet", "-m", "mine")
	testutil.GitStopping(t, feature, "cherry-pick", "main..source")
	testutil.WriteFile(t, filepath.Join(feature, "shared.txt"), "a\n")
	testutil.Git(t, feature, "add", ".")
	testutil.Git(t, feature, "commit", "--quiet", "-m", "resolved")
	todo := testutil.Git(t, feature, "rev-parse", "--git-path", "sequencer/todo")

	// The repository under a spelling the filesystem folds: compared as text holt takes
	// it for another one, skips the guard, and the commits still to be picked go.
	base := filepath.Base(clone)
	folded := filepath.Join(filepath.Dir(clone), strings.ToUpper(base))
	if folded == clone {
		t.Skip("the directory name has no other case to try")
	}
	if _, err := os.Stat(folded); err != nil {
		t.Skip("this filesystem tells the two spellings apart")
	}
	t.Chdir(folded)

	err := runHoltExpectingFailure(t, "rm", "feature")

	if !strings.Contains(err.Error(), "cherry-pick") {
		t.Errorf("error %q does not say what is in the way", err)
	}
	if _, statErr := os.Stat(todo); statErr != nil {
		t.Errorf("the commits still to be picked are gone: %v", statErr)
	}
}

func TestRemoveDoesNotReadAnOuterRepositoryForTheOperation(t *testing.T) {
	clone, _ := testutil.NewClonedRepo(t)
	feature := testutil.AddWorktree(t, clone, "feature")
	// A repository above the arrangement, part-way through a merge of its own, which
	// discovery walks up to from a worktree that lost its .git file.
	outer := filepath.Dir(clone)
	testutil.Git(t, outer, "init", "-b", "main")
	testutil.Git(t, outer, "config", "user.email", "holt@example.com")
	testutil.Git(t, outer, "config", "user.name", "holt")
	testutil.WriteFile(t, filepath.Join(outer, "shared.txt"), "one\n")
	testutil.Git(t, outer, "add", "shared.txt")
	testutil.Git(t, outer, "commit", "--quiet", "-m", "one")
	testutil.Git(t, outer, "switch", "--quiet", "--create", "other")
	testutil.WriteFile(t, filepath.Join(outer, "shared.txt"), "theirs\n")
	testutil.Git(t, outer, "commit", "--quiet", "--all", "-m", "theirs")
	testutil.Git(t, outer, "switch", "--quiet", "main")
	testutil.WriteFile(t, filepath.Join(outer, "shared.txt"), "mine\n")
	testutil.Git(t, outer, "commit", "--quiet", "--all", "-m", "mine")
	testutil.GitStopping(t, outer, "merge", "other")

	if err := os.Remove(filepath.Join(feature, ".git")); err != nil {
		t.Fatal(err)
	}
	t.Chdir(clone)

	err := runHoltExpectingFailure(t, "rm", "feature")

	// Whatever holt says must not be about the outer merge, which is somebody else's.
	if strings.Contains(err.Error(), "unfinished merge") {
		t.Errorf("error %q reports an operation from a repository holt is not in", err)
	}
}

func TestRemoveRefusesWorktreeMidSequence(t *testing.T) {
	clone, _ := testutil.NewClonedRepo(t)
	source := testutil.AddWorktree(t, clone, "source")
	for _, content := range []string{"a\n", "b\n", "c\n"} {
		testutil.WriteFile(t, filepath.Join(source, "shared.txt"), content)
		testutil.Git(t, source, "add", ".")
		testutil.Git(t, source, "commit", "--quiet", "-m", content)
	}
	feature := testutil.AddWorktree(t, clone, "feature")
	testutil.WriteFile(t, filepath.Join(feature, "shared.txt"), "mine\n")
	testutil.Git(t, feature, "add", ".")
	testutil.Git(t, feature, "commit", "--quiet", "-m", "mine")
	testutil.GitStopping(t, feature, "cherry-pick", "main..source")
	// Resolved by hand rather than with --continue, which clears CHERRY_PICK_HEAD and
	// leaves the rest of the operation recorded only in git's todo list.
	testutil.WriteFile(t, filepath.Join(feature, "shared.txt"), "a\n")
	testutil.Git(t, feature, "add", ".")
	testutil.Git(t, feature, "commit", "--quiet", "-m", "resolved")
	todo := testutil.Git(t, feature, "rev-parse", "--git-path", "sequencer/todo")
	if _, err := os.Stat(todo); err != nil {
		t.Fatalf("the fixture no longer leaves commits in the sequencer: %v", err)
	}
	t.Chdir(clone)

	err := runHoltExpectingFailure(t, "rm", "feature")

	if !strings.Contains(err.Error(), "cherry-pick") {
		t.Errorf("error %q does not say what is in the way", err)
	}
	if _, statErr := os.Stat(todo); statErr != nil {
		t.Errorf("the commits still to be picked are gone: %v", statErr)
	}
}

func TestRemoveRefusesWorktreeMidRevertSequence(t *testing.T) {
	clone, _ := testutil.NewClonedRepo(t)
	feature := testutil.AddWorktree(t, clone, "feature")
	for _, content := range []string{"a\n", "b\n", "c\n"} {
		testutil.WriteFile(t, filepath.Join(feature, "shared.txt"), content)
		testutil.Git(t, feature, "add", ".")
		testutil.Git(t, feature, "commit", "--quiet", "-m", content)
	}
	// Reverting the pair conflicts on the first, and a resolution by hand clears
	// REVERT_HEAD, leaving the sequence only in a todo list of "revert" lines.
	testutil.GitStopping(t, feature, "revert", "--no-edit", "HEAD~1", "HEAD")
	testutil.WriteFile(t, filepath.Join(feature, "shared.txt"), "a\n")
	testutil.Git(t, feature, "add", ".")
	testutil.Git(t, feature, "commit", "--quiet", "-m", "resolved")
	todo := testutil.Git(t, feature, "rev-parse", "--git-path", "sequencer/todo")
	if _, err := os.Stat(todo); err != nil {
		t.Fatalf("the fixture no longer leaves commits in the sequencer: %v", err)
	}
	t.Chdir(clone)

	err := runHoltExpectingFailure(t, "rm", "feature")

	if !strings.Contains(err.Error(), "revert") {
		t.Errorf("error %q does not name the operation, which decides the abort to run", err)
	}
	if _, statErr := os.Stat(todo); statErr != nil {
		t.Errorf("the commits still to be reverted are gone: %v", statErr)
	}
}

func TestRemoveRefusesWorktreeMidPatchApply(t *testing.T) {
	clone, _ := testutil.NewClonedRepo(t)
	patches := t.TempDir()
	source := testutil.AddWorktree(t, clone, "source")
	testutil.WriteFile(t, filepath.Join(source, "shared.txt"), "theirs\n")
	testutil.Git(t, source, "add", ".")
	testutil.Git(t, source, "commit", "--quiet", "-m", "theirs")
	testutil.Git(t, source, "format-patch", "-1", "-o", patches)

	feature := testutil.AddWorktree(t, clone, "feature")
	testutil.WriteFile(t, filepath.Join(feature, "shared.txt"), "mine\n")
	testutil.Git(t, feature, "add", ".")
	testutil.Git(t, feature, "commit", "--quiet", "-m", "mine")
	// git am and git rebase --apply share the rebase-apply directory, and calling this
	// a rebase sends the user to an abort that refuses.
	testutil.GitStopping(t, feature, "am", filepath.Join(patches, "0001-theirs.patch"))
	t.Chdir(clone)

	err := runHoltExpectingFailure(t, "rm", "feature")

	if !strings.Contains(err.Error(), "git am --abort") {
		t.Errorf("error %q does not name a command that works here", err)
	}
	if strings.Contains(err.Error(), "rebase") {
		t.Errorf("error %q calls it a rebase", err)
	}
}

func TestRemoveRefusesWorktreeMidBisect(t *testing.T) {
	clone, _ := testutil.NewClonedRepo(t)
	feature := testutil.AddWorktree(t, clone, "feature")
	testutil.CommitTo(t, feature, "one.txt", "one\n")
	// A bisect leaves a clean tree at every step, so git takes it down without a
	// word, and it has no --continue of its own.
	testutil.Git(t, feature, "bisect", "start")
	testutil.Git(t, feature, "bisect", "bad")
	t.Chdir(clone)

	err := runHoltExpectingFailure(t, "rm", "feature")

	if !strings.Contains(err.Error(), "git bisect reset") {
		t.Errorf("error %q does not name the command that ends a bisect", err)
	}
	if strings.Contains(err.Error(), "--abort") {
		t.Errorf("error %q offers an --abort that bisect does not have", err)
	}
}

func TestRemoveRefusesWorktreeStoppedOnRebaseBreak(t *testing.T) {
	clone, _ := testutil.NewClonedRepo(t)
	feature := testutil.AddWorktree(t, clone, "feature")
	testutil.CommitTo(t, feature, "work.txt", "my work\n")
	// A rebase parked on a "break" conflicts with nothing, so git takes the worktree
	// down, rebase and all, without a word.
	testutil.Git(t, feature, "-c", "sequence.editor=sed -i'' -e '1i\\\nbreak\\\n'",
		"rebase", "--interactive", "HEAD~1")
	t.Chdir(clone)

	err := runHoltExpectingFailure(t, "rm", "feature")

	if !strings.Contains(err.Error(), "rebase") {
		t.Errorf("error %q does not say what is in the way", err)
	}
	if _, statErr := os.Stat(feature); statErr != nil {
		t.Errorf("the worktree went, and the rebase with it: %v", statErr)
	}
}
