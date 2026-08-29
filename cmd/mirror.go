package cmd

import (
	"errors"
	"fmt"
	"io"
	"slices"
	"text/tabwriter"

	"github.com/MaxSiominDev/holt/internal/git"
	"github.com/MaxSiominDev/holt/internal/mirror"
	"github.com/MaxSiominDev/holt/internal/shell"
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
			main, err := mirror.MainCheckout(repo)
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
			reportRejected(out, list)
			if added {
				fmt.Fprintf(out, "added %s\n", path)
			} else {
				fmt.Fprintf(out, "%s is already mirrored\n", path)
			}
			reportTracked(out, repo, main.Path, path)
			reportHookInstall(out, repo)
			return syncAll(out, repo, list.Paths())
		},
	}
}

// Otherwise the report blames a stranger for a tracked file forever. Said rather
// than refused, since a glob can reach one among the untracked files it is for.
func reportTracked(out io.Writer, repo *git.Repo, mainCheckout, pattern string) {
	tracked, err := mirror.TrackedMatches(repo, mainCheckout, pattern)
	if err != nil || len(tracked) == 0 {
		return
	}
	// The path quoted: it may hold a space, and the line is there to be typed.
	fmt.Fprintf(out, "holt: git tracks %s, and writes it into every worktree itself, so there is nowhere for the link to go. Run %s and commit that, or mirror a path git does not carry\n",
		listSome(tracked, itemsInMessage), "\"git rm --cached "+mirror.ShellQuote(tracked[0])+"\"")
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
			out := cmd.OutOrStdout()
			reportRejected(out, list)
			paths := list.Paths()
			if len(paths) == 0 {
				fmt.Fprintln(out, "nothing is mirrored yet")
				return nil
			}
			mainCheckout, err := mirror.MainCheckout(repo)
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

			links, cleared, unsyncErr := mirror.Unsync(repo, []string{path})
			// Reported after the counts, not instead: the list is already saved,
			// so a later "holt mirror rm" can no longer name what did come away.
			out := cmd.OutOrStdout()
			reportRejected(out, list)
			fmt.Fprintf(out, "removed %s, unlinked %s from %s\n",
				path, plural(links, "symlink"), plural(cleared, "worktree"))
			// Two patterns can share one symlink, so unlinking takes a link the survivor
			// owns; linking again restores what is listed, and runs before the failure
			// above is returned, since one worktree must not cost the rest their repair.
			relinkErr := relink(out, repo, list.Paths())
			if unsyncErr != nil {
				// Joined failures carry a newline apiece, and a continuation line
				// under "Error:" reads as a second message.
				return errors.New(oneLine(unsyncErr))
			}
			return relinkErr
		},
	}
}

func newMirrorSyncCommand() *cobra.Command {
	var worktreePath string
	command := &cobra.Command{
		Use:   "sync",
		Short: "Re-apply the mirror list to existing worktrees",
		Long: "Repairs symlinks that were deleted or left pointing at a main checkout\n" +
			"that has moved, takes away the ones whose file the main checkout no\n" +
			"longer has, backfills worktrees created before a path was added, and\n" +
			"rewrites holt's block in info/exclude. Running it repeatedly is safe.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Given but empty, the flag would read as absent and turn a hook
			// meant for one worktree into a sweep of the repository.
			if cmd.Flags().Changed("worktree") && worktreePath == "" {
				return errors.New("--worktree needs the path of a worktree")
			}

			repo, err := openRepo()
			if err != nil {
				return err
			}
			list, err := mirror.LoadList(repo)
			if err != nil {
				// On the hook's path every checkout would print the raw error.
				if worktreePath != "" {
					fmt.Fprintf(cmd.OutOrStdout(), "holt: cannot read its own mirror list, so nothing was mirrored here (\"holt doctor\"): %v\n", err)
					return nil
				}
				return err
			}
			paths := list.Paths()

			if worktreePath != "" {
				// Nothing to link, and this runs on every checkout: not worth listing
				// the worktrees to find that out.
				if len(paths) == 0 {
					return nil
				}
				result, err := mirror.SyncOne(repo, paths, worktreePath)
				if err != nil {
					// As above: this runs inside a checkout nobody aimed at holt, and
					// cobra's bare "Error:" would not say whose trouble it is.
					fmt.Fprintf(cmd.OutOrStdout(), "holt: nothing was mirrored here (\"holt mirror sync\"): %v\n", oneLine(err))
					return nil
				}
				for _, path := range result.Blocked {
					fmt.Fprintf(cmd.OutOrStdout(), "holt: something holt did not put there is in the way of the mirrored %s\n", path)
				}
				return nil
			}

			// Not on the hook's path: somebody switching branches did not ask
			// about the state of the list.
			reportRejected(cmd.OutOrStdout(), list)
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

// reportRejected names the unreadable lines of a hand-edited list, worded to
// hold whether the caller has already rewritten the file or only read it.
func reportRejected(out io.Writer, list *mirror.List) {
	for _, bad := range list.Rejected() {
		fmt.Fprintf(out, "holt: %s, and holt does not keep that line when it rewrites the list\n", bad)
	}
}

// reportHookInstall keeps a refused hook from failing its command: the list is
// already saved and useful on its own.
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

func relink(out io.Writer, repo *git.Repo, patterns []string) error {
	if len(patterns) == 0 {
		return nil
	}
	results, err := mirror.Sync(repo, patterns)
	if err != nil {
		return err
	}
	return reportRelink(out, results)
}

func syncAll(out io.Writer, repo *git.Repo, patterns []string) error {
	results, err := mirror.Sync(repo, patterns)
	if err != nil {
		return err
	}
	// reportSync learns the patterns matching nothing from the linked worktrees, so with
	// none reachable a mistyped path would be added in silence. Printed before the failure
	// is returned, an unreadable worktree being one way to get here.
	reportErr := reportSync(out, results)
	inspected := slices.ContainsFunc(results, func(result mirror.WorktreeResult) bool {
		return !result.Gone && result.Err == nil
	})
	if !inspected {
		reportMissing(out, repo, patterns)
	}
	return reportErr
}

// Not worth failing the command over: the list is saved, and "holt mirror ls"
// says the same whenever asked.
func reportMissing(out io.Writer, repo *git.Repo, patterns []string) {
	mainCheckout, err := mirror.MainCheckout(repo)
	if err != nil {
		return
	}
	absent, err := mirror.MissingPatterns(mainCheckout.Path, patterns)
	if err != nil {
		return
	}
	for _, pattern := range absent {
		fmt.Fprintf(out, "not found in the main checkout: %s\n", pattern)
	}
}

// Only the exceptions: the command's own line has said what it did, and a
// "linked into 1 of 1 worktree" after it would read as if removal linked something.
func reportRelink(out io.Writer, results []mirror.WorktreeResult) error {
	return report(out, results, false)
}

// reportSync summarises across worktrees rather than printing a line each.
// Only the exceptions are worth naming.
func reportSync(out io.Writer, results []mirror.WorktreeResult) error {
	return report(out, results, true)
}

func report(out io.Writer, results []mirror.WorktreeResult, summary bool) error {
	touched, cleared, gone, lockedGone := 0, 0, 0, 0
	var moved []string
	var missing []string
	seenMissing := make(map[string]bool)
	var blocked, failed []mirror.WorktreeResult

	for _, result := range results {
		switch {
		case result.Gone:
			// A locked entry outliving its directory is what lock is for, and
			// prune spares it, so it is counted apart from what prune clears.
			if result.Worktree.Locked {
				lockedGone++
				continue
			}
			gone++
			// The project directory moved, and the prune below would drop the
			// registration for good and strand the files.
			if result.Moved != "" {
				moved = append(moved, result.Moved)
			}
			continue
		case result.Err != nil:
			// A worktree can fail on one pattern and still have found a path in the
			// way on another, which is the user's to know about.
			if len(result.Result.Blocked) > 0 {
				blocked = append(blocked, result)
			}
			failed = append(failed, result)
			continue
		}
		if len(result.Result.Linked) > 0 {
			touched++
		}
		cleared += len(result.Result.Cleared)
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

	if summary {
		fmt.Fprintf(out, "linked into %d of %s\n", touched, plural(len(results)-gone-lockedGone-len(failed), "worktree"))
	}
	if cleared > 0 {
		// The file behind them left the main checkout, perhaps unmeant, so the
		// links going is worth a line.
		fmt.Fprintf(out, "took away %s leading to a file the main checkout no longer has\n",
			plural(cleared, "symlink"))
	}
	if len(moved) > 0 {
		// The sync too: this run could not reach the worktree, whose symlinks
		// still name the main checkout as it was before the move.
		fmt.Fprintf(out, "skipped %s git records elsewhere, sitting where holt keeps them (%s, then this again)\n",
			plural(len(moved), "worktree"), shell.Named("git worktree repair "+shell.Quote(moved[0])))
	}
	if gone-len(moved) > 0 {
		fmt.Fprintf(out, "skipped %s git still lists but whose directory is gone (\"git worktree prune\")\n",
			plural(gone-len(moved), "worktree"))
	}
	if lockedGone > 0 {
		fmt.Fprintf(out, "skipped %s locked with the directory not there\n", plural(lockedGone, "worktree"))
	}
	for _, pattern := range missing {
		fmt.Fprintf(out, "not found in the main checkout: %s\n", pattern)
	}
	for _, result := range failed {
		fmt.Fprintf(out, "could not mirror into %s: %s\n", result.Worktree.Path, oneLine(result.Err))
	}

	if len(blocked) > 0 {
		fmt.Fprintln(out, "left alone, with something holt did not put there in the way:")
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

	// Everything repairable is repaired; the exit status still says something was not.
	if len(failed) > 0 {
		return fmt.Errorf("could not mirror into %s", plural(len(failed), "worktree"))
	}
	return nil
}
