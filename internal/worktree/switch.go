package worktree

import (
	"fmt"
	"io"

	"github.com/MaxSiominDev/holt/internal/git"
)

// SwitchToDefault checks out the branch origin calls its default, answering
// from what the repository already knows.
//
// Only the main checkout is switched: git allows it in a linked worktree whenever the
// default branch is free, but a worktree is there to hold a branch of its own, so holt
// refuses instead.
func SwitchToDefault(repo *git.Repo, progress io.Writer) error {
	main, err := Main(repo)
	if err != nil {
		return err
	}
	// A bare repository has no working tree to switch in, and git's own words
	// for that are about an operation the user never named.
	here, err := repo.Toplevel()
	if err != nil {
		if main.Bare {
			return fmt.Errorf("%s is a bare repository, which has no checkout to switch; make a worktree with \"holt new\" instead", main.Path)
		}
		return err
	}
	if here != main.Path {
		if main.Bare {
			return fmt.Errorf("%s is a bare repository, which has no checkout to switch; stay in this worktree or make another with \"holt new\"", main.Path)
		}
		return fmt.Errorf("this is a linked worktree, which holds one branch of its own; "+
			`go to the main checkout at %s first with "holt home"`, main.Path)
	}

	branch, err := DefaultBranch(repo)
	if err != nil {
		return err
	}
	// No --discard-changes, so git's refusal over work a checkout would
	// overwrite stands.
	if localBranchExists(repo, branch) {
		return repo.Run(progress, "switch", branch)
	}
	// The start point is named outright, since git's own guess is off under
	// checkout.guess=false and gives up when a second remote carries the same name. In
	// full, since a tag or local branch called origin/<name> makes the short form ambiguous.
	return repo.Run(progress, "switch", "--track", "-c", branch, "refs/remotes/origin/"+branch)
}
