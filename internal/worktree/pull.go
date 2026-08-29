package worktree

import (
	"io"

	"github.com/MaxSiominDev/holt/internal/git"
)

// Pull brings origin's copy of the branch checked out here into it, naming
// refs/heads/<branch>. No upstream is needed, and none is set.
func Pull(repo *git.Repo, progress io.Writer) error {
	branch, err := CurrentBranch(repo)
	if err != nil {
		return err
	}
	// Named in full: a bare name reads as a flag for a branch called -f, and git
	// resolves a tag first, so a same-named tag would be merged without a word.
	return repo.Run(progress, "pull", "origin", "refs/heads/"+branch)
}
