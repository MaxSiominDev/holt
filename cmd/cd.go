package cmd

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/MaxSiominDev/holt/internal/git"
	"github.com/MaxSiominDev/holt/internal/worktree"
	"github.com/spf13/cobra"
)

// A repository can hold a hundred worktrees.
const branchesInErrorMessage = 10

func newCdCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "cd [<branch>]",
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
			// git lists a worktree after its directory is deleted; an unreadable one
			// gets its own words, as "holt ls" and doctor also tell the two apart.
			if _, err := os.Stat(found.Path); errors.Is(err, fs.ErrNotExist) {
				return fmt.Errorf("%s is registered at %s, but that directory is gone", args[0], found.Path)
			} else if err != nil {
				return fmt.Errorf("%s is registered at %s, which holt cannot read: %w", args[0], found.Path, err)
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
		// A --detach worktree carries no branch, and holt reaches them by name;
		// saying there are none would contradict the "holt ls" just read.
		worktrees, err := worktree.List(repo)
		if err != nil {
			return err
		}
		if len(worktrees) > 1 {
			return errors.New("no linked worktree here is on a branch, and that is the name holt goes by")
		}
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
