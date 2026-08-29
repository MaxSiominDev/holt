// Package worktree creates, inspects and removes the worktrees of a repository.
package worktree

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/MaxSiominDev/holt/internal/git"
)

var ErrNoDefaultBranch = errors.New("cannot determine the default branch")

// Worktree is one entry of "git worktree list".
type Worktree struct {
	Path     string
	Branch   string // short name, empty when bare or detached, unless a rebase names one
	Detached bool
	Bare     bool
	Main     bool // the checkout that owns the shared repository directory
	Rebasing bool // detached only because a rebase is under way
	Locked   bool // "git worktree prune" leaves the entry alone on purpose
}

func List(repo *git.Repo) ([]Worktree, error) {
	out, err := repo.Output("worktree", "list", "--porcelain", "-z")
	if err != nil {
		return nil, err
	}
	worktrees := parseList(out)
	if len(worktrees) == 0 {
		return nil, errors.New("git listed no worktrees")
	}
	worktrees[0].Main = true // git lists the main worktree first
	repairMainPath(repo, worktrees)
	for index := range worktrees {
		nameRebasingBranch(repo, &worktrees[index])
	}
	return worktrees, nil
}

// git parks HEAD on a commit through a rebase, so its listing carries no branch for a
// worktree stopped in one, and without the name "holt cd" and "holt rm" cannot reach it.
func nameRebasingBranch(repo *git.Repo, w *Worktree) {
	if !w.Detached {
		return
	}
	// The state is read out of the directory, and a path no longer this repository's
	// answers for whatever is there or above, impostor branch and all.
	if !SameRepository(repo, w.Path) {
		return
	}
	gitDir, err := repo.At(w.Path).GitDir()
	if err != nil {
		return
	}
	for _, dir := range []string{"rebase-merge", "rebase-apply"} {
		recorded, err := os.ReadFile(filepath.Join(gitDir, dir, "head-name"))
		if err != nil {
			continue
		}
		// Begun detached, git writes two words here rather than a ref, which
		// would reach completion as a branch git would never allow.
		w.Rebasing = true
		if name, ok := strings.CutPrefix(strings.TrimSpace(string(recorded)), "refs/heads/"); ok && name != "" {
			w.Branch = name
		}
		return
	}
}

// A repository directory not named .git, which "clone --separate-git-dir" and an
// absorbed submodule leave, is named by git as the main worktree.
//
// --show-toplevel answers for the checkout holt was called in, so from a linked
// worktree what is left is core.worktree, which only a submodule carries.
func repairMainPath(repo *git.Repo, worktrees []Worktree) {
	if worktrees[0].Bare {
		return
	}
	gitDir, err := repo.GitDir()
	if err != nil {
		return
	}
	commonDir, err := repo.CommonDir()
	if err != nil {
		return
	}
	if gitDir == commonDir {
		toplevel, err := repo.Toplevel()
		if err != nil {
			return
		}
		worktrees[0].Path = toplevel
		return
	}
	// Only where git fell back to naming the repository directory: a checkout it
	// already found is one holt has nothing to correct.
	if worktrees[0].Path != commonDir {
		return
	}
	recorded, ok, err := repo.Config("core.worktree")
	if err != nil || !ok || recorded == "" {
		return
	}
	if !filepath.IsAbs(recorded) {
		recorded = filepath.Join(commonDir, recorded)
	}
	worktrees[0].Path = filepath.Clean(recorded)
}

func Main(repo *git.Repo) (Worktree, error) {
	worktrees, err := List(repo)
	if err != nil {
		return Worktree{}, err
	}
	return worktrees[0], nil
}

func Find(repo *git.Repo, branch string) (Worktree, error) {
	worktrees, err := List(repo)
	if err != nil {
		return Worktree{}, err
	}
	for _, w := range worktrees {
		// Bare and detached entries carry no branch, so an empty name would match
		// one the caller never asked for: holt rm "$branch" with it unset.
		if w.Branch != "" && w.Branch == branch {
			return w, nil
		}
	}

	// Nothing matched as typed, and git precomposes what it stores, so a combining
	// accent survives "holt new" and then fails to match.
	if spelled := gitSpelling(repo, branch); spelled != "" && spelled != branch {
		for _, w := range worktrees {
			if w.Branch == spelled {
				return w, nil
			}
		}
	}
	return Worktree{}, fmt.Errorf("no worktree checked out on branch %q", branch)
}

// The name git stores for a branch, empty when it has none by that name.
//
// The full refname, not --short: shortening disambiguates against tags, so a tag of
// the same name turns feature into heads/feature. "branch --list", not "for-each-ref",
// which reads refs/heads/feat as a prefix and answers feat/inner.
func gitSpelling(repo *git.Repo, branch string) string {
	// "branch --list" globs and git refuses these in a name, so an argument
	// carrying one names nothing and must not stand for what it matches.
	if strings.ContainsAny(branch, "*?[\\") {
		return ""
	}
	// Behind "--": read as a flag, "-a" would list every branch and answer with one.
	out, err := repo.Output("branch", "--list", "--format=%(refname)", "--", branch)
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(strings.TrimSpace(out), "refs/heads/")
}

// FetchDefaultBranch asks origin rather than trusting origin/HEAD, which a
// rename on the server leaves stale while the old name still resolves.
func FetchDefaultBranch(repo *git.Repo, progress io.Writer) (string, error) {
	branch, err := remoteDefaultBranch(repo)
	if err != nil {
		return "", err
	}
	if err := repo.Run(progress, "fetch", "origin", fetchRefspec(branch)); err != nil {
		return "", err
	}
	recordDefaultBranch(repo, branch, progress)
	return branch, nil
}

// Both sides named: a --single-branch refspec covers one branch, and a fetch by
// name would then update FETCH_HEAD and nothing else.
func fetchRefspec(branch string) string {
	return "+refs/heads/" + branch + ":refs/remotes/origin/" + branch
}

func remoteDefaultBranch(repo *git.Repo) (string, error) {
	// Without this, ls-remote's wording reads as though holt lost the repository.
	if _, err := repo.Output("remote", "get-url", "origin"); err != nil {
		return "", undeterminedDefaultBranch(repo)
	}

	out, err := repo.Output("ls-remote", "--symref", "origin", "HEAD")
	if err != nil {
		return "", err
	}
	// The first line is "ref: refs/heads/<branch>\tHEAD", the second the commit.
	for line := range strings.SplitSeq(out, "\n") {
		reference, _, found := strings.Cut(strings.TrimPrefix(line, "ref: "), "\t")
		if found && strings.HasPrefix(reference, "refs/heads/") {
			return strings.TrimPrefix(reference, "refs/heads/"), nil
		}
	}
	return "", fmt.Errorf("%w: origin did not say which branch its HEAD is on", ErrNoDefaultBranch)
}

// recordDefaultBranch updates origin/HEAD; a failure is ignored.
func recordDefaultBranch(repo *git.Repo, branch string, progress io.Writer) {
	previous, err := repo.Output("symbolic-ref", "refs/remotes/origin/HEAD")
	recorded := strings.TrimPrefix(previous, "refs/remotes/origin/")
	if err == nil && recorded == branch {
		return
	}
	if _, err := repo.Output("remote", "set-head", "origin", branch); err != nil {
		return
	}
	if recorded != "" {
		fmt.Fprintf(progress, "holt: origin's default branch is %s now, not %s\n", branch, recorded)
	}
}

// DefaultBranch answers offline, so it cannot see that origin/HEAD went stale
// after a rename on the server. FetchDefaultBranch is what corrects that.
func DefaultBranch(repo *git.Repo) (string, error) {
	// symbolic-ref reports a dead name for a branch deleted on the remote, which the check
	// below is for. The full ref, since --short answers remotes/origin/x once anything
	// else is named origin/x.
	if head, err := repo.Output("symbolic-ref", "refs/remotes/origin/HEAD"); err == nil {
		branch := strings.TrimPrefix(head, "refs/remotes/origin/")
		if remoteBranchExists(repo, branch) {
			return branch, nil
		}
	}

	for _, candidate := range []string{"main", "master"} {
		if remoteBranchExists(repo, candidate) {
			return candidate, nil
		}
	}

	return "", undeterminedDefaultBranch(repo)
}

// SameRepository reports whether a directory is still a worktree of this repository:
// it may have lost its .git file, leaving discovery to walk up, or been reused.
func SameRepository(repo *git.Repo, path string) bool {
	here, err := commonDirInfo(repo)
	if err != nil {
		return false
	}
	return sameRepositoryAs(repo, here, path)
}

// The side of the comparison that does not vary, so many worktrees cost it once.
func commonDirInfo(repo *git.Repo) (os.FileInfo, error) {
	here, err := repo.CommonDir()
	if err != nil {
		return nil, err
	}
	return os.Stat(here)
}

func sameRepositoryAs(repo *git.Repo, here os.FileInfo, path string) bool {
	if _, err := os.Stat(path); err != nil {
		return false
	}
	there, err := repo.At(path).CommonDir()
	if err != nil {
		return false
	}
	// By identity: one side is the caller's spelling and the other git's own, and
	// a case-folding filesystem gives the same directory under both.
	thereInfo, err := os.Stat(there)
	return err == nil && os.SameFile(here, thereInfo)
}

// LinkedBranches leaves out the main checkout, which "holt home" reaches.
func LinkedBranches(repo *git.Repo, prefix string) ([]string, error) {
	worktrees, err := List(repo)
	if err != nil {
		return nil, err
	}

	var branches []string
	for _, w := range worktrees[1:] {
		if w.Branch != "" && strings.HasPrefix(w.Branch, prefix) {
			branches = append(branches, w.Branch)
		}
	}
	return branches, nil
}

func remoteBranchExists(repo *git.Repo, branch string) bool {
	// refs/remotes/origin/HEAD names origin's default and is no branch of its own, so
	// taken for one holt fetches it, fails, and blames a pruning that changes nothing.
	if branch == "HEAD" {
		return false
	}
	_, err := repo.Output("rev-parse", "--verify", "--quiet", "refs/remotes/origin/"+branch)
	return err == nil
}

// The remedy differs: "remote set-head" has no origin to ask.
func undeterminedDefaultBranch(repo *git.Repo) error {
	if _, err := repo.Output("remote", "get-url", "origin"); err != nil {
		// git answers 2 for a missing remote and 128 for anything else, an unparsable refspec
		// among them, where git's own words beat blaming a missing remote when one is there.
		var exit *git.ExitError
		if !errors.As(err, &exit) || exit.Code != 2 {
			return fmt.Errorf("%w: %w", ErrNoDefaultBranch, err)
		}
		return fmt.Errorf("%w: this repository has no origin remote, and holt takes the default branch from origin", ErrNoDefaultBranch)
	}
	// set-head writes a symref to a remote-tracking ref no plain fetch brings in while
	// the refspec stays narrow, so widening comes first.
	if !fetchesEveryBranch(repo) {
		return fmt.Errorf("%w: nothing here records a default branch origin still has, and this repository does not fetch every branch, which is what a clone made with --single-branch, --depth or --bare looks like. Run %q, then %q, then %q",
			ErrNoDefaultBranch,
			`git config --add remote.origin.fetch '+refs/heads/*:refs/remotes/origin/*'`,
			"git fetch origin",
			"git remote set-head origin --auto")
	}
	// The fetch first: set-head needs the ref this state lacks, and from git 2.48 a
	// fetch writes origin/HEAD itself.
	return fmt.Errorf("%w: nothing here records a default branch origin still has. Run %q, then %q if that is not enough",
		ErrNoDefaultBranch, "git fetch origin", "git remote set-head origin --auto")
}

// fetchesEveryBranch reports whether origin's refspecs bring in whatever branch it
// calls default, which only an ordinary clone's wildcard does.
func fetchesEveryBranch(repo *git.Repo) bool {
	refspecs, err := repo.ConfigAll("remote.origin.fetch")
	if err != nil {
		return true // holt cannot tell, so the shorter advice stands
	}
	return slices.ContainsFunc(refspecs, func(refspec string) bool {
		source, _, found := strings.Cut(strings.TrimPrefix(refspec, "+"), ":")
		return found && source == "refs/heads/*"
	})
}

// A "worktree <path>" field opens a record; the output is NUL-terminated because
// a worktree path may hold a newline.
func parseList(out string) []Worktree {
	var worktrees []Worktree
	for line := range strings.SplitSeq(out, "\x00") {
		key, value, _ := strings.Cut(line, " ")
		if key == "worktree" {
			worktrees = append(worktrees, Worktree{Path: value})
			continue
		}
		if len(worktrees) == 0 {
			continue
		}
		current := &worktrees[len(worktrees)-1]
		switch key {
		case "branch":
			current.Branch = strings.TrimPrefix(value, "refs/heads/")
		case "detached":
			current.Detached = true
		case "bare":
			current.Bare = true
		case "locked":
			current.Locked = true
		}
	}
	return worktrees
}
