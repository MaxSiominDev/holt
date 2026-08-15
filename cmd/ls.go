package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/MaxSiominDev/holt/internal/git"
	"github.com/MaxSiominDev/holt/internal/worktree"
	"github.com/spf13/cobra"
)

func newListCommand() *cobra.Command {
	var porcelain bool
	command := &cobra.Command{
		Use:     "ls",
		Short:   "List the worktrees and the state each is in",
		GroupID: groupWorktrees,
		Long: "Shows each worktree with how far its branch has drifted from the default\n" +
			"branch and whether it holds uncommitted work. The drift columns stay empty\n" +
			"when there is no remote default branch to compare against.",
		Args:    cobra.NoArgs,
		Aliases: []string{"list"},
		RunE: func(cmd *cobra.Command, _ []string) error {
			repo, err := openRepo()
			if err != nil {
				return err
			}
			statuses, err := worktree.Statuses(repo)
			if err != nil {
				return err
			}
			// The note goes to stderr so the table on stdout stays script-readable.
			noteMissingDrift(cmd.ErrOrStderr(), repo, statuses)

			if porcelain {
				return writeWorktreeRecords(cmd.OutOrStdout(), statuses)
			}
			return writeWorktreeTable(cmd.OutOrStdout(), statuses)
		},
	}
	command.Flags().BoolVar(&porcelain, "porcelain", false,
		"one tab-separated record per worktree: branch, ahead, behind, state, path "+
			"(an empty branch means a detached HEAD or a bare repository)")
	return command
}

// noteMissingDrift says why the ahead and behind columns are empty, without failing.
func noteMissingDrift(out io.Writer, repo *git.Repo, statuses []worktree.Status) {
	named := slices.ContainsFunc(statuses, func(status worktree.Status) bool { return status.Branch != "" })
	compared := slices.ContainsFunc(statuses, func(status worktree.Status) bool { return status.Compared })
	if compared || !named {
		return
	}

	if _, err := worktree.DefaultBranch(repo); err != nil {
		fmt.Fprintf(out, "holt: no ahead/behind numbers, %v\n", err)
		return
	}
	if !worktree.SupportsDrift(repo) {
		fmt.Fprintln(out, "holt: no ahead/behind numbers, this git cannot compare branches in one call (git 2.41 or newer can)")
		return
	}
	fmt.Fprintln(out, "holt: no ahead/behind numbers, origin has no copy of the default branch yet")
}

func writeWorktreeTable(out io.Writer, statuses []worktree.Status) error {
	home, _ := os.UserHomeDir() // "" when it cannot tell, which shortenHome handles

	table := tabwriter.NewWriter(out, 0, 0, 3, ' ', 0)
	fmt.Fprintln(table, "BRANCH\tAHEAD/BEHIND\tSTATE\tPATH")
	for _, status := range statuses {
		fmt.Fprintf(table, "%s\t%s\t%s\t%s\n",
			branchLabel(status), driftLabel(status), stateLabel(status), shortenHome(status.Path, home))
	}
	return table.Flush()
}

func writeWorktreeRecords(out io.Writer, statuses []worktree.Status) error {
	for _, status := range statuses {
		ahead, behind := "", ""
		if status.Compared {
			ahead, behind = strconv.Itoa(status.Ahead), strconv.Itoa(status.Behind)
		}
		if _, err := fmt.Fprintf(out, "%s\t%s\t%s\t%s\t%s\n",
			status.Branch, ahead, behind, status.State, status.Path); err != nil {
			return err
		}
	}
	return nil
}

func branchLabel(status worktree.Status) string {
	switch {
	case status.Bare:
		return "(bare)"
	case status.Detached:
		return "(detached)"
	default:
		return status.Branch
	}
}

// driftLabel renders git's two counts: ahead of the default branch, then behind.
func driftLabel(status worktree.Status) string {
	if !status.Compared {
		return ""
	}
	ahead, behind := "0", "0"
	if status.Ahead > 0 {
		ahead = "+" + strconv.Itoa(status.Ahead)
	}
	if status.Behind > 0 {
		behind = "-" + strconv.Itoa(status.Behind)
	}
	return ahead + " / " + behind
}

func stateLabel(status worktree.Status) string {
	if status.State == worktree.StateDirty {
		return "*"
	}
	return string(status.State)
}

// shortenHome writes the home directory the way the shell prompt does.
func shortenHome(path, home string) string {
	if home == "" {
		return path
	}
	if path == home {
		return "~"
	}
	if rest, found := strings.CutPrefix(path, home+string(filepath.Separator)); found {
		return "~" + string(filepath.Separator) + rest
	}
	return path
}
