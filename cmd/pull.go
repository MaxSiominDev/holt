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
		Long: "On branch feature this pulls refs/heads/feature from origin, spelled out so\n" +
			"that a tag of the same name cannot come instead. Naming the branch at all\n" +
			"means it works whether or not an upstream was ever configured, and holt does\n" +
			"not configure one.\n\n" +
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
