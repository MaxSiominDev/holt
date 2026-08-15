// Package worktree creates, inspects and removes the worktrees of a repository.
package worktree

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/MaxSiominDev/holt/internal/git"
)

var ErrNoDefaultBranch = errors.New("cannot determine the default branch")

// Worktree is one entry of "git worktree list".
type Worktree struct {
	Path     string
	Branch   string // short name, empty when detached or bare
	Detached bool
	Bare     bool
	Main     bool // the checkout that owns the shared repository directory
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
	return worktrees, nil
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
		if w.Branch == branch {
			return w, nil
		}
	}
	return Worktree{}, fmt.Errorf("no worktree checked out on branch %q", branch)
}

// FetchDefaultBranch asks origin rather than trusting origin/HEAD, which git
// writes at clone time and never again: a branch renamed on the server leaves a
// stale name behind that still resolves.
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

// Both sides are named because a --single-branch clone's own refspec covers one
// branch, and a fetch by name would then update FETCH_HEAD and nothing else.
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
	previous, err := repo.Output("symbolic-ref", "--short", "refs/remotes/origin/HEAD")
	recorded := strings.TrimPrefix(previous, "origin/")
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
	// symbolic-ref reports a dead name for a branch deleted on the remote.
	if head, err := repo.Output("symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil {
		branch := strings.TrimPrefix(head, "origin/")
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
	if branch == "" {
		return false
	}
	_, err := repo.Output("rev-parse", "--verify", "--quiet", "refs/remotes/origin/"+branch)
	return err == nil
}

// The remedy differs: "remote set-head" fails outright with no origin to ask.
func undeterminedDefaultBranch(repo *git.Repo) error {
	if _, err := repo.Output("remote", "get-url", "origin"); err != nil {
		return fmt.Errorf("%w: this repository has no origin remote, and holt takes the default branch from origin", ErrNoDefaultBranch)
	}
	return fmt.Errorf("%w: origin/HEAD is missing here, restore it with %q", ErrNoDefaultBranch,
		"git remote set-head origin --auto")
}

// A "worktree <path>" field opens a record and later fields add to it. The
// output is NUL-terminated because a worktree path may contain a newline.
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
		}
	}
	return worktrees
}
