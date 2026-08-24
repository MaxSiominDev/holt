package cmd

import (
	"github.com/MaxSiominDev/holt/internal/worktree"
	"github.com/spf13/cobra"
)

func newMainCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "main",
		Short:   "Switch the main checkout to the default branch",
		GroupID: groupWorktrees,
		Long: "Runs \"git switch\" on whatever branch origin calls its default, without\n" +
			"touching the network.\n\n" +
			"A linked worktree holds one branch of its own, so holt refuses there and\n" +
			"points at \"holt home\" instead.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			repo, err := openRepo()
			if err != nil {
				return err
			}
			return worktree.SwitchToDefault(repo, cmd.ErrOrStderr())
		},
	}
}
