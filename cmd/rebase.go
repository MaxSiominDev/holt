package cmd

import (
	"github.com/MaxSiominDev/holt/internal/worktree"
	"github.com/spf13/cobra"
)

func newRebaseCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "rebase",
		Short:   "Rebase this branch onto the default branch",
		GroupID: groupBranch,
		Long: "Refuses unless the rebase is plainly safe: a branch of its own, no\n" +
			"uncommitted changes, and nothing of git's own left half-finished in the\n" +
			"worktree, a rebase, merge, cherry-pick, revert, am or bisect among them.\n\n" +
			"On a conflict it stops where git stopped and says how to continue or abort.\n" +
			"It never pushes: force-pushing rewritten history is yours to decide.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			repo, err := openRepo()
			if err != nil {
				return err
			}
			return worktree.Rebase(repo, cmd.ErrOrStderr())
		},
	}
}
