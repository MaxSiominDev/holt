package cmd

import (
	"github.com/MaxSiominDev/holt/internal/worktree"
	"github.com/spf13/cobra"
)

func newHomeCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "home",
		Short:   "Go back to the main checkout",
		GroupID: groupWorktrees,
		Long: "Prints the directory and nothing else, for the shell function to enter it\n" +
			"from anywhere in the repository.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			repo, err := openRepo()
			if err != nil {
				return err
			}
			main, err := worktree.Main(repo)
			if err != nil {
				return err
			}
			return printPath(cmd, main.Path)
		},
	}
}
