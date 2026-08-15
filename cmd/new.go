package cmd

import (
	"github.com/MaxSiominDev/holt/internal/worktree"
	"github.com/spf13/cobra"
)

func newNewCommand() *cobra.Command {
	var skipFetch bool
	command := &cobra.Command{
		Use:     "new <branch>",
		Short:   "Create a worktree for a new branch and go there",
		GroupID: groupWorktrees,
		Long: "Adds a worktree in <repo>-worktrees/<branch>. The main checkout keeps the\n" +
			"branch it had open and does not have to be clean.\n\n" +
			"What the branch is made from depends on what already exists:\n" +
			"  a local branch of that name    it is checked out as it stands, no network\n" +
			"  a known origin/<branch>        the branch is fetched and tracked, not shadowed\n" +
			"  neither                        the default branch is fetched and branched off\n\n" +
			"Prints the new directory, for the shell function to enter.",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: cobra.NoFileCompletions,
		RunE: func(cmd *cobra.Command, args []string) error {
			repo, err := openRepo()
			if err != nil {
				return err
			}
			// git writes its progress to stderr; holt keeps stdout for the path.
			path, err := worktree.Create(repo, args[0], worktree.CreateOptions{SkipFetch: skipFetch}, cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			return printPath(cmd, path)
		},
	}
	command.Flags().BoolVar(&skipFetch, "no-fetch", false,
		"work offline: use the origin/... already in this repository, fetching nothing")
	return command
}
