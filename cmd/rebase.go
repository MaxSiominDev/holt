package cmd

import (
	"github.com/MaxSiominDev/holt/internal/worktree"
	"github.com/spf13/cobra"
)

func newRebaseCommand() *cobra.Command {
	var noAbort bool
	command := &cobra.Command{
		Use:     "rebase",
		Short:   "Rebase this branch onto the default branch",
		GroupID: groupBranch,
		Long: "Refuses unless the rebase is plainly safe: a branch of its own, no\n" +
			"uncommitted changes, and nothing of git's own left half-finished in the\n" +
			"worktree, a rebase, merge, cherry-pick, revert, am or bisect among them.\n\n" +
			"On a conflict it undoes the rebase, names the files that disagreed and\n" +
			"leaves the branch where it was. --no-abort stops where git stopped instead,\n" +
			"for resolving by hand.\n\n" +
			"It never pushes: force-pushing rewritten history is yours to decide.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			repo, err := openRepo()
			if err != nil {
				return err
			}
			return worktree.Rebase(repo, !noAbort, cmd.ErrOrStderr())
		},
	}
	command.Flags().BoolVar(&noAbort, "no-abort", false,
		"on a conflict stop where git stopped instead of putting the branch back")
	return command
}
