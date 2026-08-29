package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/MaxSiominDev/holt/internal/worktree"
	"github.com/spf13/cobra"
)

// lostWithWorktree keeps the entries whose content goes with the worktree: a
// symlink out of it leaves its file, and every mirrored path lands in info/exclude,
// so they would drown the .env the warning is for. Matching by pattern would hide
// a real file sharing a glob's name.
func lostWithWorktree(worktreePath string, ignored []string) []string {
	return slices.DeleteFunc(ignored, func(entry string) bool {
		return survivesRemoval(worktreePath, filepath.Join(worktreePath, strings.TrimSuffix(entry, "/")))
	})
}

// survivesRemoval reports whether what is behind path outlives the worktree: a symlink
// out of it does, and so does a directory holding nothing else, which is what an
// ignored ".claude/" of mirrored files arrives as.
func survivesRemoval(worktreePath, path string) bool {
	if target, err := os.Readlink(path); err == nil {
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(path), target)
		}
		rel, err := filepath.Rel(worktreePath, target)
		return err != nil || !filepath.IsLocal(rel)
	}

	// An empty directory takes nothing with it; an unreadable one stays in the
	// warning, since holt cannot tell what is inside.
	entries, err := os.ReadDir(path)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if !survivesRemoval(worktreePath, filepath.Join(path, entry.Name())) {
			return false
		}
	}
	return true
}

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
			"deletes with the worktree, so those are named once it is gone: a .env sits\n" +
			"among the build output.",
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
			// git takes a half-finished operation down with the worktree and refuses only
			// over a conflicted tree, which a rebase stopped on a "break" does not leave.
			//
			// A directory that is gone cannot be asked, and one without its .git is worse:
			// discovery walks up and an outer repository answers with its own markers.
			operation := ""
			if worktree.SameRepository(repo, found.Path) {
				operation, err = worktree.OperationInProgress(repo.At(found.Path))
				if err != nil {
					return err
				}
			}
			switch {
			case operation == "bisect":
				// The one without a --continue or an --abort of its own.
				return fmt.Errorf("%s has an unfinished bisect; end it with %q, then remove the worktree",
					found.Path, "git bisect reset")
			case operation != "":
				// Only the abort is named: finishing depends on where it stopped, and
				// "cherry-pick --continue" refuses outright on a commit gone empty.
				return fmt.Errorf("%s has an unfinished %s; %q there says how to finish it, or undo it with %q, then remove the worktree",
					found.Path, operation, "git status", "git "+operation+" --abort")
			}

			out := cmd.OutOrStdout()
			// Read before the removal, reported after: with no prompt in between,
			// warning first would claim a loss git's own refusal then prevents.
			ignored, err := worktree.IgnoredFiles(repo, found.Path)
			if err != nil {
				return err
			}
			ignored = lostWithWorktree(found.Path, ignored)

			if err := worktree.Remove(repo, found.Path, cmd.ErrOrStderr()); err != nil {
				return err
			}
			fmt.Fprintf(out, "removed %s\n", found.Path)
			if len(ignored) > 0 {
				fmt.Fprintf(out, "took %s with it, which git ignores: %s\n",
					plural(len(ignored), "path"), listSome(ignored, ignoredInMessage))
			}

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
