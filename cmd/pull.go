package cmd

import (
	"github.com/MaxSiominDev/holt/internal/worktree"
	"github.com/spf13/cobra"
)

func newPullCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "pull",
		Short:   "Pull this branch from origin",
		GroupID: groupBranch,
		Long: "On branch feature this runs \"git pull origin feature\". Naming the branch\n" +
			"outright means it works whether or not an upstream was ever configured, and\n" +
			"holt does not configure one.\n\n" +
			"For anything else git pull can do, run git yourself. \"holt rebase\" replants\n" +
			"this branch on the default branch.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			repo, err := openRepo()
			if err != nil {
				return err
			}
			return worktree.Pull(repo, cmd.ErrOrStderr())
		},
	}
}
