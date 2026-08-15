package cmd

import (
	"errors"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/MaxSiominDev/holt/internal/git"
	"github.com/MaxSiominDev/holt/internal/mirror"
	"github.com/MaxSiominDev/holt/internal/worktree"
	"github.com/spf13/cobra"
)

func newMirrorCommand() *cobra.Command {
	command := &cobra.Command{
		Use:     "mirror",
		Short:   "Mirror your personal files into every worktree",
		GroupID: groupSetup,
		Long: "Personal files git does not track, such as CLAUDE.local.md, are absent from\n" +
			"every worktree git creates. holt keeps a list of them and symlinks each one\n" +
			"back to the main checkout, in the worktrees that exist now and in every one\n" +
			"created later.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	command.AddCommand(
		newMirrorAddCommand(),
		newMirrorListCommand(),
		newMirrorRemoveCommand(),
		newMirrorSyncCommand(),
		newMirrorHookCommand(),
	)
	return command
}

func newMirrorAddCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "add <path>",
		Short: "Mirror a repo-relative path into every worktree",
		Long: "The path is relative to the repository root and may be a glob, expanded\n" +
			"against the main checkout. Adding a path also installs the post-checkout\n" +
			"hook and links the path into the worktrees that already exist.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			repo, err := openRepo()
			if err != nil {
				return err
			}
			path, err := mirror.CleanPath(args[0])
			if err != nil {
				return err
			}
			main, err := worktree.Main(repo)
			if err != nil {
				return err
			}
			if err := mirror.CheckAmbiguous(main.Path, path); err != nil {
				return err
			}
			list, err := mirror.LoadList(repo)
			if err != nil {
				return err
			}
			added, err := list.Add(path)
			if err != nil {
				return err
			}
			if err := list.Save(); err != nil {
				return err
			}
			if err := mirror.WriteExcludes(repo, list.Paths()); err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			if added {
				fmt.Fprintf(out, "added %s\n", path)
			} else {
				fmt.Fprintf(out, "%s is already mirrored\n", path)
			}
			reportHookInstall(out, repo)
			return syncAll(out, repo, list.Paths())
		},
	}
}

func newMirrorListCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "ls",
		Short:   "Show the mirrored paths",
		Args:    cobra.NoArgs,
		Aliases: []string{"list"},
		RunE: func(cmd *cobra.Command, _ []string) error {
			repo, err := openRepo()
			if err != nil {
				return err
			}
			list, err := mirror.LoadList(repo)
			if err != nil {
				return err
			}
			paths := list.Paths()
			if len(paths) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "nothing is mirrored yet")
				return nil
			}
			mainCheckout, err := worktree.Main(repo)
			if err != nil {
				return err
			}
			absent, err := mirror.MissingPatterns(mainCheckout.Path, paths)
			if err != nil {
				return err
			}

			missing := make(map[string]bool, len(absent))
			for _, pattern := range absent {
				missing[pattern] = true
			}
			table := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 3, ' ', 0)
			for _, path := range paths {
				status := "present"
				if missing[path] {
					status = "not found in the main checkout"
				}
				fmt.Fprintf(table, "%s\t%s\n", path, status)
			}
			return table.Flush()
		},
	}
}

func newMirrorRemoveCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "rm <path>",
		Short:   "Stop mirroring a path and remove its symlinks",
		Args:    cobra.ExactArgs(1),
		Aliases: []string{"remove"},
		RunE: func(cmd *cobra.Command, args []string) error {
			repo, err := openRepo()
			if err != nil {
				return err
			}
			path, err := mirror.CleanPath(args[0])
			if err != nil {
				return err
			}
			list, err := mirror.LoadList(repo)
			if err != nil {
				return err
			}
			removed, err := list.Remove(path)
			if err != nil {
				return err
			}
			if !removed {
				return fmt.Errorf("%s is not mirrored", path)
			}
			if err := list.Save(); err != nil {
				return err
			}
			if err := mirror.WriteExcludes(repo, list.Paths()); err != nil {
				return err
			}

			links, cleared, err := mirror.Unsync(repo, []string{path})
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "removed %s, unlinked %s from %s\n",
				path, plural(links, "symlink"), plural(cleared, "worktree"))
			return nil
		},
	}
}

func newMirrorSyncCommand() *cobra.Command {
	var worktreePath string
	command := &cobra.Command{
		Use:   "sync",
		Short: "Re-apply the mirror list to existing worktrees",
		Long: "Repairs symlinks that were deleted or went stale, backfills worktrees\n" +
			"created before a path was added, and rewrites holt's block in info/exclude.\n" +
			"Running it repeatedly is safe.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			repo, err := openRepo()
			if err != nil {
				return err
			}
			list, err := mirror.LoadList(repo)
			if err != nil {
				return err
			}
			paths := list.Paths()

			if worktreePath != "" {
				// The hook runs this on every checkout, so it says nothing
				// unless a mirrored path is shadowed.
				if len(paths) == 0 {
					return nil
				}
				result, err := mirror.SyncOne(repo, paths, worktreePath)
				if err != nil {
					return err
				}
				for _, path := range result.Blocked {
					fmt.Fprintf(cmd.OutOrStdout(), "holt: a real file is in the way of the mirrored %s\n", path)
				}
				return nil
			}

			if len(paths) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "nothing is mirrored yet")
				return nil
			}
			// sync is the repair command, so it restores the exclude block too.
			if err := mirror.WriteExcludes(repo, paths); err != nil {
				return err
			}
			return syncAll(cmd.OutOrStdout(), repo, paths)
		},
	}
	command.Flags().StringVar(&worktreePath, "worktree", "", "mirror into this worktree only (used by the post-checkout hook)")
	return command
}

func newMirrorHookCommand() *cobra.Command {
	var replace bool
	command := &cobra.Command{
		Use:   "hook",
		Short: "Install the post-checkout hook that mirrors into new worktrees",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			repo, err := openRepo()
			if err != nil {
				return err
			}
			path, err := mirror.InstallHook(repo, mirror.HookOptions{Replace: replace})
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "installed %s\n", path)
			return nil
		},
	}
	command.Flags().BoolVar(&replace, "replace", false, "overwrite a post-checkout hook holt did not write")
	return command
}

// reportHookInstall keeps a refused hook from failing the command that asked
// for it: the mirror list is already saved and useful on its own.
func reportHookInstall(out io.Writer, repo *git.Repo) {
	if _, err := mirror.InstallHook(repo, mirror.HookOptions{}); err != nil {
		fmt.Fprintf(out, "hook not installed: %v\n", err)
		if errors.Is(err, mirror.ErrForeignHook) {
			fmt.Fprintln(out, `run "holt mirror hook --replace" to take it over, or mirror by hand with "holt mirror sync"`)
			return
		}
		fmt.Fprintln(out, `mirror by hand with "holt mirror sync"`)
	}
}

func syncAll(out io.Writer, repo *git.Repo, patterns []string) error {
	results, err := mirror.Sync(repo, patterns)
	if err != nil {
		return err
	}
	return reportSync(out, results)
}

// reportSync summarises across worktrees rather than printing a line each: a
// repository can hold a hundred, and only the exceptions need naming.
func reportSync(out io.Writer, results []mirror.WorktreeResult) error {
	touched, gone := 0, 0
	var missing []string
	seenMissing := make(map[string]bool)
	var blocked, failed []mirror.WorktreeResult

	for _, result := range results {
		switch {
		case result.Gone:
			gone++
			continue
		case result.Err != nil:
			failed = append(failed, result)
			continue
		}
		if len(result.Result.Linked) > 0 {
			touched++
		}
		for _, pattern := range result.Result.Unmatched {
			if !seenMissing[pattern] {
				seenMissing[pattern] = true
				missing = append(missing, pattern)
			}
		}
		if len(result.Result.Blocked) > 0 {
			blocked = append(blocked, result)
		}
	}

	fmt.Fprintf(out, "linked into %d of %s\n", touched, plural(len(results)-gone-len(failed), "worktree"))
	if gone > 0 {
		fmt.Fprintf(out, "skipped %s git still lists but whose directory is gone (\"git worktree prune\")\n",
			plural(gone, "worktree"))
	}
	for _, pattern := range missing {
		fmt.Fprintf(out, "not found in the main checkout: %s\n", pattern)
	}
	for _, result := range failed {
		fmt.Fprintf(out, "could not mirror into %s: %v\n", result.Worktree.Path, result.Err)
	}

	if len(blocked) > 0 {
		fmt.Fprintln(out, "left alone, a real file is in the way:")
		table := tabwriter.NewWriter(out, 0, 0, 3, ' ', 0)
		for _, result := range blocked {
			for _, path := range result.Result.Blocked {
				fmt.Fprintf(table, "  %s\t%s\n", result.Worktree.Path, path)
			}
		}
		if err := table.Flush(); err != nil {
			return err
		}
	}

	// Everything that could be repaired has been; the exit status still reports
	// that something could not.
	if len(failed) > 0 {
		return fmt.Errorf("could not mirror into %s", plural(len(failed), "worktree"))
	}
	return nil
}
