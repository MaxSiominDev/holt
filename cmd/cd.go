package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/MaxSiominDev/holt/internal/git"
	"github.com/MaxSiominDev/holt/internal/worktree"
	"github.com/spf13/cobra"
)

// A repository can hold a hundred worktrees.
const branchesInErrorMessage = 10

func newCdCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "cd <branch>",
		Short:   "Go to the worktree for a branch",
		GroupID: groupWorktrees,
		Long: "Prints the directory and nothing else, for the shell function to enter.\n" +
			"Press TAB to complete the branch name.",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeWorktreeBranches,
		RunE: func(cmd *cobra.Command, args []string) error {
			repo, err := openRepo()
			if err != nil {
				return err
			}
			if len(args) == 0 {
				return noBranchNamed(repo)
			}

			found, err := worktree.Find(repo, args[0])
			if err != nil {
				return err
			}
			// git keeps listing a worktree after its directory is deleted.
			if _, err := os.Stat(found.Path); err != nil {
				return fmt.Errorf("%s is registered at %s, but that directory is gone", args[0], found.Path)
			}
			return printPath(cmd, found.Path)
		},
	}
}

func noBranchNamed(repo *git.Repo) error {
	branches, err := worktree.LinkedBranches(repo, "")
	if err != nil {
		return err
	}
	if len(branches) == 0 {
		return errors.New("this repository has no linked worktrees yet")
	}

	return fmt.Errorf("name the worktree to enter, or press TAB to complete one of: %s",
		listSome(branches, branchesInErrorMessage))
}

func completeWorktreeBranches(_ *cobra.Command, _ []string, prefix string) ([]string, cobra.ShellCompDirective) {
	repo, err := openRepo()
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}
	branches, err := worktree.LinkedBranches(repo, prefix)
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}
	return branches, cobra.ShellCompDirectiveNoFileComp
}
