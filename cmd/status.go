package cmd

import "github.com/spf13/cobra"

func newStatusCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "status",
		Aliases: []string{"st"},
		Short:   "Show git status for this worktree",
		GroupID: groupBranch,
		Long: "Runs git status and shows what it says. holt adds nothing and takes\n" +
			"nothing: for anything beyond the plain listing, run git yourself.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			repo, err := openRepo()
			if err != nil {
				return err
			}
			return repo.Passthrough(cmd.OutOrStdout(), cmd.ErrOrStderr(), "status")
		},
	}
}
