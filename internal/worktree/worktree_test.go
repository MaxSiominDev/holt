package worktree

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/MaxSiominDev/holt/internal/git"
	"github.com/MaxSiominDev/holt/internal/testutil"
)

func TestParseList(t *testing.T) {
	tests := []struct {
		name string
		out  string
		want []Worktree
	}{
		{
			name: "main checkout and linked worktree",
			out: strings.Join([]string{
				"worktree /repos/project",
				"HEAD 1111111111111111111111111111111111111111",
				"branch refs/heads/master",
				"",
				"worktree /repos/project-worktrees/feature",
				"HEAD 2222222222222222222222222222222222222222",
				"branch refs/heads/feature",
				"",
			}, "\x00"),
			want: []Worktree{
				{Path: "/repos/project", Branch: "master"},
				{Path: "/repos/project-worktrees/feature", Branch: "feature"},
			},
		},
		{
			name: "detached head has no branch",
			out: strings.Join([]string{
				"worktree /repos/project",
				"HEAD 3333333333333333333333333333333333333333",
				"detached",
				"",
			}, "\x00"),
			want: []Worktree{
				{Path: "/repos/project", Detached: true},
			},
		},
		{
			name: "bare repository reports no head",
			out:  "worktree /repos/project.git\x00bare\x00",
			want: []Worktree{
				{Path: "/repos/project.git", Bare: true},
			},
		},
		{
			name: "path containing spaces is kept whole",
			out: strings.Join([]string{
				"worktree /repos/my project/feature branch",
				"HEAD 4444444444444444444444444444444444444444",
				"branch refs/heads/feature",
				"",
			}, "\x00"),
			want: []Worktree{
				{Path: "/repos/my project/feature branch", Branch: "feature"},
			},
		},
		{
			name: "a lock is read, and its reason and prunable are skipped",
			out: strings.Join([]string{
				"worktree /repos/project",
				"HEAD 5555555555555555555555555555555555555555",
				"branch refs/heads/master",
				"locked reason for the lock",
				"prunable gitdir file points to non-existent location",
				"",
			}, "\x00"),
			want: []Worktree{
				{Path: "/repos/project", Branch: "master", Locked: true},
			},
		},
		{
			name: "path containing a newline is kept whole",
			out: strings.Join([]string{
				"worktree /repos/my project/odd\nname",
				"HEAD 6666666666666666666666666666666666666666",
				"branch refs/heads/odd",
				"",
			}, "\x00"),
			want: []Worktree{
				{Path: "/repos/my project/odd\nname", Branch: "odd"},
			},
		},
		{
			name: "empty output yields no worktrees",
			out:  "",
			want: nil,
		},
		{
			name: "attribute before the first record has nothing to attach to",
			out:  "HEAD 7777777777777777777777777777777777777777\x00bare\x00",
			want: nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := parseList(test.out)

			if !reflect.DeepEqual(got, test.want) {
				t.Errorf("got %+v, want %+v", got, test.want)
			}
		})
	}
}

func TestDefaultBranchNoOriginHead(t *testing.T) {
	main := testutil.NewRepo(t)
	// An origin with no recorded default and no main or master on it, whose remedy
	// differs from the no-origin one.
	testutil.Git(t, main, "remote", "add", "origin", filepath.Join(main, "..", "origin.git"))

	_, err := DefaultBranch(open(t, main))

	if !errors.Is(err, ErrNoDefaultBranch) {
		t.Fatalf("error %q is not ErrNoDefaultBranch", err)
	}
	if !strings.Contains(err.Error(), "remote set-head") {
		t.Errorf("error %q does not say how to restore origin/HEAD", err)
	}
}

func TestFetchDefaultBranchNoOrigin(t *testing.T) {
	main := testutil.NewRepo(t)

	_, err := FetchDefaultBranch(open(t, main), io.Discard)

	if !errors.Is(err, ErrNoDefaultBranch) {
		t.Fatalf("error %q is not ErrNoDefaultBranch", err)
	}
	// ls-remote's own wording sounds like holt lost the repository, not the remote.
	if !strings.Contains(err.Error(), "no origin remote") {
		t.Errorf("error %q is git's rather than holt's", err)
	}
}

func TestListPathWithNewline(t *testing.T) {
	main := testutil.NewRepo(t)
	// Without NUL-terminated output holt would work on the truncated path.
	path := filepath.Join(filepath.Dir(main), "odd\nname")
	testutil.Git(t, main, "worktree", "add", "--quiet", "-b", "odd", path)

	worktrees, err := List(open(t, main))
	if err != nil {
		t.Fatal(err)
	}

	var found *Worktree
	for index := range worktrees {
		if worktrees[index].Branch == "odd" {
			found = &worktrees[index]
		}
	}
	if found == nil {
		t.Fatalf("the worktree is missing from %+v", worktrees)
	}
	if found.Path != path {
		t.Fatalf("got path %q, want %q", found.Path, path)
	}
}

func TestListMarksMainCheckout(t *testing.T) {
	repo := testutil.NewRepo(t)
	linked := testutil.AddWorktree(t, repo, "feature")

	worktrees, err := List(open(t, linked))
	if err != nil {
		t.Fatal(err)
	}

	if len(worktrees) != 2 {
		t.Fatalf("got %d worktrees, want 2", len(worktrees))
	}
	// Listed from inside the linked worktree, the main checkout still leads.
	if worktrees[0].Path != repo || !worktrees[0].Main {
		t.Errorf("first entry is %+v, want the main checkout at %s", worktrees[0], repo)
	}
	if worktrees[1].Main {
		t.Error("the linked worktree was marked as main")
	}
}

func TestMainFromWorktree(t *testing.T) {
	repo := testutil.NewRepo(t)
	linked := testutil.AddWorktree(t, repo, "feature")

	main, err := Main(open(t, linked))
	if err != nil {
		t.Fatal(err)
	}

	if main.Path != repo {
		t.Fatalf("got %q, want %q", main.Path, repo)
	}
}

func TestFindByBranch(t *testing.T) {
	repo := testutil.NewRepo(t)
	want := testutil.AddWorktree(t, repo, "feature")

	found, err := Find(open(t, repo), "feature")
	if err != nil {
		t.Fatal(err)
	}

	if found.Path != want {
		t.Fatalf("got %q, want %q", found.Path, want)
	}
}

func TestFindUnknownBranch(t *testing.T) {
	repo := testutil.NewRepo(t)

	_, err := Find(open(t, repo), "feature")

	if err == nil {
		t.Fatal("expected an error for a branch that has no worktree")
	}
	if !strings.Contains(err.Error(), "feature") {
		t.Errorf("error %q does not name the branch", err)
	}
}

func TestDefaultBranchUnusualName(t *testing.T) {
	repo := testutil.NewRepo(t)
	// holt has no setting for this; the name has to come from git.
	testutil.Git(t, repo, "branch", "--move", "trunk")
	testutil.SetOriginHead(t, repo, "trunk")

	branch, err := DefaultBranch(open(t, repo))
	if err != nil {
		t.Fatal(err)
	}

	if branch != "trunk" {
		t.Fatalf("got %q, want %q", branch, "trunk")
	}
}

func TestFetchDefaultBranchStale(t *testing.T) {
	clone, _ := testutil.NewClonedRepo(t)
	// Renamed on the server, so origin/HEAD names the old branch and resolves.
	testutil.AddRemoteBranch(t, clone, "initial-pr")
	testutil.Git(t, clone, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/initial-pr")
	if stale, err := DefaultBranch(open(t, clone)); err != nil || stale != "initial-pr" {
		t.Fatalf("the fixture gives %q, want the stale name to be believed first", stale)
	}

	var progress strings.Builder
	_, _ = FetchDefaultBranch(open(t, clone), &progress)

	branch, err := DefaultBranch(open(t, clone))
	if err != nil {
		t.Fatal(err)
	}
	if branch != "main" {
		t.Fatalf("got %q, want the name origin actually reports", branch)
	}
	// The base of the work about to be built just moved.
	if !strings.Contains(progress.String(), "initial-pr") {
		t.Errorf("progress is %q, want the change reported", progress.String())
	}
}

func TestFetchDefaultBranchQuiet(t *testing.T) {
	clone, _ := testutil.NewClonedRepo(t)

	var progress strings.Builder
	branch, err := FetchDefaultBranch(open(t, clone), &progress)
	if err != nil {
		t.Fatal(err)
	}
	if branch != "main" {
		t.Fatalf("got %q, so the fetch this is about never happened", branch)
	}

	// git's fetch output belongs here; a move that never happened does not.
	if strings.Contains(progress.String(), "holt:") {
		t.Fatalf("progress is %q, want no word from holt when nothing moved", progress.String())
	}
}

func TestDefaultBranchOriginHead(t *testing.T) {
	repo := testutil.NewRepo(t)
	testutil.SetOriginHead(t, repo, "main")

	branch, err := DefaultBranch(open(t, repo))
	if err != nil {
		t.Fatal(err)
	}

	if branch != "main" {
		t.Fatalf("got %q, want %q", branch, "main")
	}
}

func TestDefaultBranchRemoteMaster(t *testing.T) {
	repo := testutil.NewRepo(t)
	// The local branch is main, so "master" proves the remote decides.
	testutil.AddRemoteBranch(t, repo, "master")

	branch, err := DefaultBranch(open(t, repo))
	if err != nil {
		t.Fatal(err)
	}

	if branch != "master" {
		t.Fatalf("got %q, want %q", branch, "master")
	}
}

func TestDefaultBranchPrefersMain(t *testing.T) {
	repo := testutil.NewRepo(t)
	testutil.AddRemoteBranch(t, repo, "master")
	testutil.AddRemoteBranch(t, repo, "main")

	branch, err := DefaultBranch(open(t, repo))
	if err != nil {
		t.Fatal(err)
	}

	if branch != "main" {
		t.Fatalf("got %q, want %q", branch, "main")
	}
}

func TestDefaultBranchDanglingHead(t *testing.T) {
	repo := testutil.NewRepo(t)
	testutil.SetOriginHead(t, repo, "old-default")
	testutil.AddRemoteBranch(t, repo, "main")
	// origin/HEAD points at a ref that is gone, and symbolic-ref still reports it.
	testutil.RemoveRemoteBranch(t, repo, "old-default")

	branch, err := DefaultBranch(open(t, repo))
	if err != nil {
		t.Fatal(err)
	}

	if branch != "main" {
		t.Fatalf("got %q, want the dangling origin/HEAD skipped in favour of %q", branch, "main")
	}
}

func TestDefaultBranchNoRemote(t *testing.T) {
	repo := testutil.NewRepo(t)

	_, err := DefaultBranch(open(t, repo))

	if !errors.Is(err, ErrNoDefaultBranch) {
		t.Fatalf("error %q is not ErrNoDefaultBranch", err)
	}
	// "git remote set-head origin" fails without an origin.
	if strings.Contains(err.Error(), "set-head") {
		t.Errorf("error %q suggests a command that cannot work without an origin", err)
	}
	if !strings.Contains(err.Error(), "no origin remote") {
		t.Errorf("error %q does not say what is actually missing", err)
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

func TestFindRejectsEmptyBranch(t *testing.T) {
	repo := testutil.NewRepo(t)
	testutil.AddWorktree(t, repo, "keep")
	detached := filepath.Join(filepath.Dir(repo), "review")
	testutil.Git(t, repo, "worktree", "add", "--quiet", "--detach", detached)

	// The detached worktree is the one an empty name would reach.
	_, err := Find(open(t, repo), "")

	if err == nil {
		t.Fatal("an empty branch name matched a worktree")
	}
}

func TestListMainPathWithSeparateGitDir(t *testing.T) {
	root := testutil.Root(t)
	gitDir := filepath.Join(root, "projgit")
	checkout := filepath.Join(root, "proj")
	testutil.Git(t, root, "init", "--quiet", "--separate-git-dir", gitDir, checkout)
	testutil.Git(t, checkout, "config", "user.email", "holt@example.com")
	testutil.Git(t, checkout, "config", "user.name", "holt")
	testutil.WriteFile(t, filepath.Join(checkout, "README.md"), "x\n")
	testutil.Git(t, checkout, "add", ".")
	testutil.Git(t, checkout, "commit", "--quiet", "-m", "init")
	// The repository directory is not named .git, so git reports it as the main
	// worktree though it holds no checkout, and holt would mirror into it.
	if listed := testutil.Git(t, checkout, "worktree", "list", "--porcelain"); !strings.Contains(listed, gitDir) {
		t.Fatalf("the fixture no longer reproduces the case; git listed:\n%s", listed)
	}

	worktrees, err := List(open(t, checkout))
	if err != nil {
		t.Fatal(err)
	}

	if worktrees[0].Path != checkout {
		t.Fatalf("main checkout is %q, want the working tree at %q", worktrees[0].Path, checkout)
	}
}

func TestListMainPathFromALinkedWorktreeOfASubmodule(t *testing.T) {
	root := testutil.Root(t)
	gitDir := filepath.Join(root, "modules", "sub")
	checkout := filepath.Join(root, "sub")
	if err := os.MkdirAll(filepath.Dir(gitDir), 0o755); err != nil {
		t.Fatal(err)
	}
	testutil.Git(t, root, "init", "--quiet", "--separate-git-dir", gitDir, checkout)
	// What "git submodule add" leaves: the repository directory sits in the
	// superproject and records its checkout, which a --separate-git-dir clone does not.
	testutil.Git(t, checkout, "config", "core.worktree", filepath.Join("..", "..", "sub"))
	testutil.Git(t, checkout, "config", "user.email", "holt@example.com")
	testutil.Git(t, checkout, "config", "user.name", "holt")
	testutil.WriteFile(t, filepath.Join(checkout, "README.md"), "x\n")
	testutil.Git(t, checkout, "add", ".")
	testutil.Git(t, checkout, "commit", "--quiet", "-m", "init")
	linked := filepath.Join(root, "sub-worktrees", "feature")
	testutil.Git(t, checkout, "worktree", "add", "--quiet", "-b", "feature", linked)

	worktrees, err := List(open(t, linked))
	if err != nil {
		t.Fatal(err)
	}

	// git answers about the linked worktree, so the main entry comes from the
	// repository itself; at git's fallback "holt home" walks into the superproject's .git.
	if worktrees[0].Path != checkout {
		t.Fatalf("main checkout is %q, want the working tree at %q", worktrees[0].Path, checkout)
	}
}

func TestListLeavesTheMainPathGitAlreadyResolved(t *testing.T) {
	main := testutil.NewRepo(t)
	linked := testutil.AddWorktree(t, main, "feature")
	// core.worktree set where it is not needed names somewhere other than the checkout
	// git found, and read anyway it moves the main checkout out from under holt.
	testutil.Git(t, main, "config", "core.worktree", filepath.Join(main, "..", "elsewhere"))

	worktrees, err := List(open(t, linked))
	if err != nil {
		t.Fatal(err)
	}

	if worktrees[0].Path != main {
		t.Fatalf("main checkout is %q, want the working tree at %q", worktrees[0].Path, main)
	}
}

func TestListDoesNotTakeABranchNameFromAnotherRepository(t *testing.T) {
	main := testutil.NewRepo(t)
	linked := testutil.AddWorktree(t, main, "feature")
	testutil.Git(t, linked, "checkout", "--quiet", "--detach")
	// A worktree deleted by hand without a prune, its path now a project of its own
	// stopped in a rebase.
	if err := os.RemoveAll(linked); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(linked, 0o755); err != nil {
		t.Fatal(err)
	}
	testutil.Git(t, linked, "init", "--quiet", ".")
	testutil.WriteFile(t, filepath.Join(linked, ".git", "rebase-merge", "head-name"), "refs/heads/urgent-fix\n")

	worktrees, err := List(open(t, main))
	if err != nil {
		t.Fatal(err)
	}

	// Read out of that directory, a stranger's branch is listed, completed and
	// reached by "holt cd", shadowing a real worktree of the same name.
	for _, w := range worktrees {
		if w.Branch == "urgent-fix" {
			t.Fatalf("holt named %s after a branch of another repository", w.Path)
		}
	}
}

func TestDefaultBranchAdviceForASingleBranchClone(t *testing.T) {
	clone, _ := testutil.NewClonedRepo(t)
	// What --single-branch leaves: origin's default is a branch never fetched here.
	testutil.Git(t, clone, "config", "--replace-all", "remote.origin.fetch",
		"+refs/heads/other:refs/remotes/origin/other")
	testutil.Git(t, clone, "symbolic-ref", "--delete", "refs/remotes/origin/HEAD")
	for _, branch := range []string{"main", "master"} {
		_, _ = open(t, clone).Output("update-ref", "-d", "refs/remotes/origin/"+branch)
	}

	_, err := DefaultBranch(open(t, clone))

	if err == nil {
		t.Fatal("a default branch was named where nothing records one")
	}
	// set-head needs a remote-tracking ref no plain fetch brings in while the refspec
	// stays narrow, so naming it alone leaves the user no way out.
	if !strings.Contains(err.Error(), "remote.origin.fetch") {
		t.Errorf("error %q leaves out the widening the other steps need", err)
	}
	// A bare clone records no refspec and lands here too, so a count is unknowable.
	if strings.Contains(err.Error(), "one branch only") {
		t.Errorf("error %q counts branches it has not looked at", err)
	}
}

func TestDefaultBranchAdviceForAnOrdinaryClone(t *testing.T) {
	clone, _ := testutil.NewClonedRepo(t)
	testutil.Git(t, clone, "symbolic-ref", "--delete", "refs/remotes/origin/HEAD")
	for _, branch := range []string{"main", "master"} {
		_, _ = open(t, clone).Output("update-ref", "-d", "refs/remotes/origin/"+branch)
	}

	_, err := DefaultBranch(open(t, clone))

	if err == nil {
		t.Fatal("a default branch was named where nothing records one")
	}
	// Every branch is fetched already, so a fetch is all it takes, and advising a
	// widening would leave a second copy of the wildcard.
	if strings.Contains(err.Error(), "remote.origin.fetch") {
		t.Errorf("error %q asks for a widening this repository does not need", err)
	}
	if !strings.Contains(err.Error(), "git fetch origin") {
		t.Errorf("error %q leaves out the fetch that brings the branch in", err)
	}
}

func TestDefaultBranchBrokenOriginConfig(t *testing.T) {
	repo := testutil.NewRepo(t)
	testutil.Git(t, repo, "remote", "add", "origin", filepath.Join(repo, "..", "origin.git"))
	// A refspec git will not parse, which fails every command loading the remote,
	// though "there is no origin" is the one thing that is not wrong.
	testutil.Git(t, repo, "config", "--add", "remote.origin.fetch", "+refs/heads/main:refs/remotes/origin/*")

	_, err := DefaultBranch(open(t, repo))

	if err == nil {
		t.Fatal("a repository git itself refuses to read was accepted")
	}
	if strings.Contains(err.Error(), "no origin remote") {
		t.Errorf("error %q blames a missing remote for a broken one", err)
	}
	if !strings.Contains(err.Error(), "invalid refspec") {
		t.Errorf("error %q swallows what git actually said", err)
	}
}

func TestFindBranchSpelledWithACombiningAccent(t *testing.T) {
	clone, _ := testutil.NewClonedRepo(t)
	// git precomposes and stores what it is given, so compared byte for byte holt
	// makes a worktree it cannot afterwards find.
	decomposed := "cafe\u0301"
	composed := "caf\u00e9"
	testutil.Git(t, clone, "branch", decomposed)
	if stored := testutil.Git(t, clone, "for-each-ref", "--format=%(refname:short)", "refs/heads/"+decomposed); stored != composed {
		t.Skipf("git stored the name as %q, so this platform does not precompose", stored)
	}
	testutil.Git(t, clone, "worktree", "add", "--quiet", filepath.Join(clone, "..", "wt"), composed)
	// A tag of the same name, against which git disambiguates the short refname into
	// heads/<name>, which no worktree carries.
	testutil.Git(t, clone, "tag", composed)

	found, err := Find(open(t, clone), decomposed)

	if err != nil {
		t.Fatalf("holt cannot find the worktree it would have made: %v", err)
	}
	if found.Branch != composed {
		t.Errorf("found the worktree on %q, want the one on %q", found.Branch, composed)
	}
}

func TestFindRefusesANameThatOnlyPrefixesABranch(t *testing.T) {
	clone, _ := testutil.NewClonedRepo(t)
	// git reads refs/heads/feat as a prefix and answers feat/inner, so "holt rm feat"
	// would take a worktree the caller never named.
	testutil.Git(t, clone, "branch", "feat/inner")
	testutil.Git(t, clone, "worktree", "add", "--quiet", filepath.Join(clone, "..", "inner"), "feat/inner")

	if _, err := Find(open(t, clone), "feat"); err == nil {
		t.Error("a name that only prefixes a branch found the worktree on it")
	}
	// The same shape reached through a glob rather than a prefix.
	if _, err := Find(open(t, clone), "feat*"); err == nil {
		t.Error("a glob found the worktree on a branch it happened to match")
	}
}

func TestFindRefusesTheGlobCharactersGitForbidsInAName(t *testing.T) {
	clone, _ := testutil.NewClonedRepo(t)
	// Neither character is allowed in a refname and "branch --list" reads both as
	// pattern syntax, so each stands for whatever branch it happens to match.
	testutil.Git(t, clone, "branch", "ab")
	testutil.Git(t, clone, "worktree", "add", "--quiet", filepath.Join(clone, "..", "ab"), "ab")

	for _, name := range []string{"a\\b", "a[b]"} {
		if _, err := Find(open(t, clone), name); err == nil {
			t.Errorf("%q found the worktree on a branch it happened to match", name)
		}
	}
}

func TestFindRefusesANameReadAsAFlag(t *testing.T) {
	// One ref in the repository, so a lookup listing them all passes it off as the
	// name given, and "-a", git's flag for all of them, names no branch here. A clone
	// would hide it behind several lines of remote-tracking refs.
	repo := testutil.NewRepo(t)
	if refs := testutil.Git(t, repo, "for-each-ref", "--format=%(refname)"); refs != "refs/heads/main" {
		t.Fatalf("the fixture holds %q, want the single ref this case needs", refs)
	}

	if _, err := Find(open(t, repo), "-a"); err == nil {
		t.Error("a name read as a flag found a worktree")
	}
}
