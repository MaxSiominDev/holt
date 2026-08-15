package cmd

import (
	"github.com/MaxSiominDev/holt/internal/worktree"
	"github.com/spf13/cobra"
)

func newPushCommand() *cobra.Command {
	var force bool
	command := &cobra.Command{
		Use:     "push",
		Short:   "Push this branch to origin",
		GroupID: groupBranch,
		Long: "On branch feature this runs \"git push origin feature\". The upstream is left\n" +
			"alone.\n\n" +
			"--force is what a rebase leaves you needing. It refuses if the remote branch\n" +
			"has moved since your last fetch, and refuses again if your branch does not\n" +
			"contain what that fetch brought in.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			repo, err := openRepo()
			if err != nil {
				return err
			}
			return worktree.Push(repo, force, cmd.ErrOrStderr())
		},
	}
	command.Flags().BoolVarP(&force, "force", "f", false,
		"rewrite the remote branch, refusing if that would overwrite work you have not seen")
	return command
}
