package worktree

import (
	"fmt"
	"io"

	"github.com/MaxSiominDev/holt/internal/git"
)

// SwitchToDefault checks out the branch origin calls its default, answering
// from what the repository already knows.
//
// Only the main checkout is switched. git allows it in a linked worktree
// whenever the default branch is free, but a worktree is there to hold one
// branch of its own, so holt refuses instead.
func SwitchToDefault(repo *git.Repo, progress io.Writer) error {
	main, err := Main(repo)
	if err != nil {
		return err
	}
	here, err := repo.Toplevel()
	if err != nil {
		return err
	}
	if here != main.Path {
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
	// The start point is named outright: git's own guess is off under
	// checkout.guess=false and gives up when a second remote carries the same
	// branch name.
	return repo.Run(progress, "switch", "--track", "-c", branch, "origin/"+branch)
}
