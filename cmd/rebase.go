package cmd

import (
	"fmt"

	"github.com/MaxSiominDev/holt/internal/config"
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
			"A conflict in a markdown file named in the merge list, which is\n" +
			"~/.config/holt/merge.list by default, is settled here instead, and only\n" +
			"where both sides did nothing but add lines, this branch's own first.\n\n" +
			"It never pushes: force-pushing rewritten history is yours to decide.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			repo, err := openRepo()
			if err != nil {
				return err
			}
			list, err := config.LoadMergeList()
			if err != nil {
				return err
			}
			// A line holt cannot read is a file it will not settle, and the rebase
			// that then puts the branch back would be the only news of the typo.
			for _, rejected := range list.Rejected() {
				fmt.Fprintf(cmd.ErrOrStderr(), "holt: %s: %s\n", list.Path(), rejected)
			}
			return worktree.Rebase(repo, !noAbort, list, cmd.ErrOrStderr())
		},
	}
	command.Flags().BoolVar(&noAbort, "no-abort", false,
		"on a conflict stop where git stopped instead of putting the branch back")
	return command
}
