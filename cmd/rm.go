package cmd

import (
	"fmt"

	"github.com/MaxSiominDev/holt/internal/worktree"
	"github.com/spf13/cobra"
)

// Build output alone can run to hundreds of files.
const ignoredInMessage = 5

func newRemoveCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "rm <branch>",
		Short:   "Remove a worktree, and its branch if it is merged",
		GroupID: groupWorktrees,
		Long: "Removes one named worktree, and the branch too if origin's default branch\n" +
			"already has its commits. A branch still holding work of its own is kept and\n" +
			"reported, whether or not it has been pushed.\n\n" +
			"--force is never passed to \"git worktree remove\", so git's refusal over\n" +
			"uncommitted changes stands. That does not cover ignored files, which git\n" +
			"deletes with the worktree, so those are listed first: a .env sits among the\n" +
			"build output.",
		Args:              cobra.ExactArgs(1),
		Aliases:           []string{"remove"},
		ValidArgsFunction: completeWorktreeBranches,
		RunE: func(cmd *cobra.Command, args []string) error {
			repo, err := openRepo()
			if err != nil {
				return err
			}
			branch := args[0]
			found, err := worktree.Find(repo, branch)
			if err != nil {
				return err
			}
			if found.Main {
				return fmt.Errorf("%s is the main checkout, which holt does not remove", found.Path)
			}

			out := cmd.OutOrStdout()
			ignored, err := worktree.IgnoredFiles(repo, found.Path)
			if err != nil {
				return err
			}
			if len(ignored) > 0 {
				fmt.Fprintf(out, "taking %s with it, which git ignores: %s\n",
					plural(len(ignored), "file"), listSome(ignored, ignoredInMessage))
			}

			if err := worktree.Remove(repo, found.Path, cmd.ErrOrStderr()); err != nil {
				return err
			}
			fmt.Fprintf(out, "removed %s\n", found.Path)

			outcome, err := worktree.DeleteMergedBranch(repo, branch)
			if err != nil {
				return err
			}
			switch outcome {
			case worktree.BranchDeleted:
				fmt.Fprintf(out, "deleted branch %s, the default branch already has its commits\n", branch)
			case worktree.BranchKeptUnmerged:
				fmt.Fprintf(out, "kept branch %s, it holds commits the default branch does not\n", branch)
			default:
				fmt.Fprintf(out, "kept branch %s, there is no default branch here to check it against\n", branch)
			}
			return nil
		},
	}
}
