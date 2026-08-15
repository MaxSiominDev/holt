package worktree

import (
	"io"

	"github.com/MaxSiominDev/holt/internal/git"
)

// Pull runs "git pull origin <branch>" for the branch checked out here. No
// upstream is needed, and none is set.
func Pull(repo *git.Repo, progress io.Writer) error {
	branch, err := CurrentBranch(repo)
	if err != nil {
		return err
	}
	// Named in full for the reason Push gives: a bare name can read as a flag.
	return repo.Run(progress, "pull", "origin", "refs/heads/"+branch)
}
