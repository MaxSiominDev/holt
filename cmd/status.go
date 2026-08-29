package cmd

import "github.com/spf13/cobra"

func newStatusCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "status",
		Aliases: []string{"st"},
		Short:   "Show git status for this worktree",
		GroupID: groupBranch,
		Long: "Runs git status in this worktree and prints it back unchanged. holt\n" +
			"forwards no flags and no pathspecs, so for anything beyond the plain\n" +
			"listing, run git yourself.",
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
