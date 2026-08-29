package worktree

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/MaxSiominDev/holt/internal/git"
	"github.com/MaxSiominDev/holt/internal/shell"
)

// /code/project keeps its worktrees in /code/project-worktrees/<branch>.
const worktreeDirSuffix = "-worktrees"

type CreateOptions struct {
	// SkipFetch works from the refs already in this repository, with no network.
	SkipFetch bool
}

// Create adds a worktree for branch and returns its path: an existing branch as it
// stands, one of origin's, tracked where the refspec allows, and otherwise a branch
// cut from the freshly fetched default, which is origin's tip.
func Create(repo *git.Repo, branch string, options CreateOptions, progress io.Writer) (string, error) {
	main, err := Main(repo)
	if err != nil {
		return "", err
	}
	// "worktree add -b" makes the branch through "git branch", where a leading dash reads
	// as a flag: "-f" is swallowed, the start point becomes the branch, and the local ref
	// left behind makes every later start point ambiguous.
	if strings.HasPrefix(branch, "-") {
		return "", fmt.Errorf("%q is not a valid branch name: git reads a leading dash as a flag", branch)
	}
	path := worktreePath(main.Path, branch)

	// git would create the branch first and only then notice the directory.
	if _, err := os.Lstat(path); err == nil {
		return "", fmt.Errorf("%s already exists, so there is nowhere to put the worktree", occupant(path))
	}

	// An existing branch carries its own history: no default branch, no network.
	if localBranchExists(repo, branch) {
		if err := repo.Run(progress, "worktree", "add", path, branch); err != nil && !registered(repo, branch, path) {
			return "", err
		}
		return path, nil
	}

	// The name is taken on origin, so it is tracked rather than shadowed, and the start
	// point is spelled in full, since a tag or local branch called origin/<name> makes the
	// short form ambiguous and git refuses the add outright.
	if remoteBranchExists(repo, branch) || (!options.SkipFetch && originHasBranch(repo, branch)) {
		if !options.SkipFetch {
			// Output rather than Run: staleRemoteBranch reads git's message.
			if _, err := repo.Output("fetch", "origin", fetchRefspec(branch)); err != nil {
				return "", staleRemoteBranch(err, branch)
			}
		}
		start := "refs/remotes/origin/" + branch
		args := []string{"--track", "-b", branch, path, start}
		if excluded := excludedFromFetch(repo, branch); excluded != "" {
			// Widening undoes no exclusion, so the advice below is wrong here.
			fmt.Fprintf(progress, "holt: no upstream for %s: origin's fetch refspec excludes it, and git will not track a branch it is told not to fetch. Remove from remote.origin.fetch: %s. Then run %s\n",
				branch, excluded,
				shell.Named("git branch --set-upstream-to="+shell.Quote("origin/"+branch)+" "+shell.Quote(branch)))
			args = []string{"-b", branch, path, start}
		} else if !trackable(repo, branch) {
			// Widening sets no upstream on the branch just made, and git refuses
			// to set one by hand until it is widened: both, in this order.
			fmt.Fprintf(progress, "holt: no upstream for %s: origin's fetch refspec does not cover it, which is what a clone made with --single-branch or --depth looks like. Run %q, fetch, then %s; branches made after that get one on their own\n",
				branch,
				`git config --add remote.origin.fetch '+refs/heads/*:refs/remotes/origin/*'`,
				shell.Named("git branch --set-upstream-to="+shell.Quote("origin/"+branch)+" "+shell.Quote(branch)))
			args = []string{"-b", branch, path, start}
		}
		if err := addNewBranch(repo, branch, path, progress, args...); err != nil {
			return "", err
		}
		return path, nil
	}

	// Offline, the branch recorded at clone time has to do.
	defaultBranch, err := resolveDefaultBranch(repo, options.SkipFetch, progress)
	if err != nil {
		return "", err
	}

	// --no-track, or branch.autoSetupMerge tracks origin/<default>: a bare
	// "git pull" would then merge the default branch into the work.
	if err := addNewBranch(repo, branch, path, progress, "--no-track", "-b", branch, path, "refs/remotes/origin/"+defaultBranch); err != nil {
		return "", err
	}
	return path, nil
}

// git creates the branch before finding out whether the directory can be made,
// so a failure leaves one behind and the retry takes the existing-branch path.
func addNewBranch(repo *git.Repo, branch, path string, progress io.Writer, args ...string) error {
	var said bytes.Buffer
	err := repo.Run(io.MultiWriter(progress, &said), append([]string{"worktree", "add"}, args...)...)
	if err == nil {
		return nil
	}

	// A foreign post-checkout hook exiting non-zero, by when branch, entry and checkout
	// all stand: calling it a failure would print no path and leave a worktree the retry
	// refuses to touch.
	if registered(repo, branch, path) {
		return nil
	}

	// Something else may have made the branch since Create looked, and one holt
	// did not create is not holt's to take back.
	// The branch-specific wording, not a bare "already exists": git says that of
	// an occupied path too, after having created the branch.
	if strings.Contains(said.String(), fmt.Sprintf("a branch named '%s' already exists", branch)) {
		return err
	}
	if localBranchExists(repo, branch) {
		_, _ = repo.Output("branch", "--delete", "--force", branch)
	}
	return err
}

// registered reports whether git made the worktree, not merely recorded it: a
// directory deleted by hand leaves the entry behind, and answering off that entry
// hands the shell a path it cannot enter.
func registered(repo *git.Repo, branch, path string) bool {
	made, err := os.Stat(path)
	if err != nil {
		return false
	}
	worktrees, err := List(repo)
	if err != nil {
		return false
	}
	// Asked of the filesystem: git resolves symlinks in the path it records, so
	// a worktrees root reached through one never matches as text.
	return slices.ContainsFunc(worktrees, func(w Worktree) bool {
		if w.Branch != branch {
			return false
		}
		listed, err := os.Stat(w.Path)
		return err == nil && os.SameFile(made, listed)
	})
}

// The refs here say nothing of a branch pushed since the last fetch, so "holt new"
// on a colleague's would cut a second one off the default: their commits missing,
// no upstream, and a refused push.
func originHasBranch(repo *git.Repo, branch string) bool {
	commit, answered := originTip(repo, branch)
	return answered && commit != ""
}

// The commit origin has the branch at, and whether origin could be asked: an empty
// commit from an answered ask means no such branch, and offline is not an answer.
func originTip(repo *git.Repo, branch string) (string, bool) {
	// ls-remote reads its argument as a pattern and git refuses these in a branch
	// name, so asking would take whatever it matches and fetch a ref nobody named.
	if strings.ContainsAny(branch, "*?[\\") {
		return "", true
	}
	ref := "refs/heads/" + branch
	out, err := repo.Output("ls-remote", "--exit-code", "origin", ref)
	if err != nil {
		// git answers 2 for a ref origin lacks and 128 for a question it could not put.
		var exit *git.ExitError
		if errors.As(err, &exit) && exit.Code == 2 {
			return "", true
		}
		return "", false
	}
	// ls-remote matches a name's tail at a slash, so foo/refs/heads/bar answers for
	// refs/heads/bar; a tail match carries more components, which tells them apart
	// without comparing text macOS may have precomposed.
	for line := range strings.SplitSeq(out, "\n") {
		commit, name, found := strings.Cut(line, "\t")
		if found && strings.Count(name, "/") == strings.Count(ref, "/") {
			return commit, true
		}
	}
	// A tail match of some other name, so origin answered and lacks this branch.
	return "", true
}

// A refspec beginning with ^ excludes and has no destination, so trackable passes
// over it, takes the wildcard beside it, and hands git a --track that kills the add.
func excludedFromFetch(repo *git.Repo, branch string) string {
	refspecs, err := repo.ConfigAll("remote.origin.fetch")
	if err != nil {
		return ""
	}
	return excludedBy(refspecs, branch)
}

// Split out so a caller holding the refspecs already does not read them twice.
func excludedBy(refspecs []string, branch string) string {
	// Every covering line: removing one of two leaves the branch excluded still.
	var excluding []string
	for _, refspec := range refspecs {
		if pattern, negative := strings.CutPrefix(refspec, "^"); negative &&
			patternCovers(pattern, "refs/heads/"+branch) {
			excluding = append(excluding, strconv.Quote(refspec))
		}
	}
	return strings.Join(excluding, " and ")
}

// git calls a start point a branch when a refspec produces it, so ask the side refs
// are written to; dropping --track costs the upstream, keeping it costs the add.
func trackable(repo *git.Repo, branch string) bool {
	refspecs, err := repo.ConfigAll("remote.origin.fetch")
	if err != nil {
		return true // holt cannot tell, so let git answer
	}
	for _, refspec := range refspecs {
		_, destination, found := strings.Cut(strings.TrimPrefix(refspec, "+"), ":")
		if found && patternCovers(destination, "refs/remotes/origin/"+branch) {
			return true
		}
	}
	return false
}

func resolveDefaultBranch(repo *git.Repo, skipFetch bool, progress io.Writer) (string, error) {
	if skipFetch {
		return DefaultBranch(repo)
	}
	branch, err := FetchDefaultBranch(repo, progress)
	if err == nil {
		return branch, nil
	}
	// Asking origin is the only part needing the network, which holt can skip, and
	// named only where it works: without a recorded branch it fails a second time.
	if _, offline := DefaultBranch(repo); offline == nil {
		fmt.Fprintf(progress, "holt: %q makes the branch from the refs already here, without asking origin\n", "holt new --no-fetch")
	}
	return "", err
}

// Nothing pruned origin/<branch> after it was merged and deleted, so holt goes
// to track it and git objects. Pruning would drop every other stale ref too.
func staleRemoteBranch(err error, branch string) error {
	var exit *git.ExitError
	if !errors.As(err, &exit) || !strings.Contains(exit.Stderr, "couldn't find remote ref") {
		return err
	}
	return fmt.Errorf("origin has no branch %s any more, but this repository still remembers one. Run %q to forget the branches origin has dropped, then try again",
		branch, "git fetch --prune")
}

// occupant is the path as its directory spells it: where case folds, "feat"
// answers the stat for "FEAT", and naming the latter sends the user hunting.
func occupant(path string) string {
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		return path
	}
	name := filepath.Base(path)
	for _, entry := range entries {
		if entry.Name() != name && strings.EqualFold(entry.Name(), name) {
			return filepath.Join(filepath.Dir(path), entry.Name())
		}
	}
	return path
}

// RelocatedPath is where holt's layout puts this worktree under the main checkout
// as it stands, which a moved project leaves; empty means it really is gone.
//
// The two must be told apart: a prune drops the registration for good, and a
// repair leaves holt's absolute symlinks naming the old main checkout.
func RelocatedPath(repo *git.Repo, mainCheckout string, w Worktree) string {
	if w.Branch == "" {
		return ""
	}
	path := worktreePath(mainCheckout, w.Branch)
	if path == w.Path || !registeredAs(repo, path, w.Path) {
		return ""
	}
	return path
}

// registeredAs reports whether the directory holds the .git file of the worktree git
// records at recorded; anything else is the user's, and mistaking it costs them a prune.
//
// The entry is followed rather than guessed: git appends a counter when the last
// segment is taken, and only the entry holds the path back after a move.
func registeredAs(repo *git.Repo, path, recorded string) bool {
	entry, ok := worktreeEntry(path)
	if !ok {
		return false
	}
	commonDir, err := repo.CommonDir()
	if err != nil {
		return false
	}
	back, err := os.ReadFile(filepath.Join(commonDir, "worktrees", entry, "gitdir"))
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(back)) == filepath.Join(recorded, ".git")
}

// worktreeEntry is the name git files a worktree under, read out of its .git
// file; a repository of its own has a .git directory and names no entry.
func worktreeEntry(path string) (string, bool) {
	gitFile := filepath.Join(path, ".git")
	info, err := os.Lstat(gitFile)
	if err != nil || !info.Mode().IsRegular() {
		return "", false
	}
	content, err := os.ReadFile(gitFile)
	if err != nil {
		return "", false
	}
	gitDir, found := strings.CutPrefix(strings.TrimSpace(string(content)), "gitdir:")
	if !found {
		return "", false
	}
	// The name is joined onto the entries directory: Base leaves no separator, so a
	// hand-written .git file cannot send holt below it, and "..", the one name that
	// climbs out, lands where git writes no gitdir.
	return filepath.Base(strings.TrimSpace(gitDir)), true
}

func worktreePath(mainCheckout, branch string) string {
	return filepath.Join(worktreesRoot(mainCheckout), branch)
}

func worktreesRoot(mainCheckout string) string {
	parent := filepath.Dir(mainCheckout)
	return filepath.Join(parent, filepath.Base(mainCheckout)+worktreeDirSuffix)
}

func localBranchExists(repo *git.Repo, branch string) bool {
	_, err := repo.Output("rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	return err == nil
}
