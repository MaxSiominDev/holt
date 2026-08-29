// Package cmd assembles the holt command tree.
package cmd

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/MaxSiominDev/holt/internal/git"
	"github.com/spf13/cobra"
)

// Without a group, a command lands in cobra's "Additional Commands".
const (
	groupWorktrees = "worktrees"
	groupBranch    = "branch"
	groupSetup     = "setup"
)

// The error returned has already been reported to the user by cobra.
func Execute(version string) error {
	return newRootCommand(version).Execute()
}

func newRootCommand(version string) *cobra.Command {
	root := &cobra.Command{
		Use:   "holt",
		Short: "Work out of many git worktrees, with your own files in every one of them",
		Long: "A directory per branch, so changing task is changing directory. Nothing to\n" +
			"stash, and every branch stays as you left it.\n" +
			"\n" +
			"holt also carries in the files git will not. Anything untracked lives only\n" +
			"in the main checkout, so a new worktree comes up without your\n" +
			"CLAUDE.local.md and nothing says so. Name those files once and every\n" +
			"worktree gets them, later ones included.\n" +
			"\n" +
			"Once, in your shell config: eval \"$(holt shell-init zsh)\", or bash. That is\n" +
			"what lets cd, home and new actually move you. \"holt doctor\" says what is\n" +
			"missing.",
		Example: "  holt new PROJ-1234-fix    branch off the fresh default branch and go there\n" +
			"  holt ls                   what exists, how far each has drifted, which hold work\n" +
			"  holt cd <TAB>             go to another worktree, completing the branch name\n" +
			"  holt rebase               replant this branch on the default branch\n" +
			"  holt push -f              publish it without overwriting anyone\n" +
			"  holt open                 raise the request for review\n" +
			"  holt home                 back to the main checkout\n" +
			"  holt rm PROJ-1234-fix     take the worktree down once it is merged",
		Version: version,
		Args:    cobra.NoArgs,
		// cobra validates Args only for a runnable command.
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
		// The usage block would bury the error of a command that merely failed.
		SilenceUsage: true,
	}
	root.SetVersionTemplate("holt {{.Version}}\n")
	// Declaring it here stops cobra adding a "-v" shorthand, which means verbose.
	root.Flags().Bool("version", false, "version for holt")

	root.AddGroup(
		&cobra.Group{ID: groupWorktrees, Title: "Moving between worktrees:"},
		&cobra.Group{ID: groupBranch, Title: "Working on the branch you are in:"},
		&cobra.Group{ID: groupSetup, Title: "Setting holt up:"},
	)
	root.AddCommand(
		newNewCommand(),
		newCdCommand(),
		newHomeCommand(),
		newMainCommand(),
		newListCommand(),
		newRemoveCommand(),
		newRebaseCommand(),
		newStatusCommand(),
		newPullCommand(),
		newPushCommand(),
		newOpenCommand(),
		newMirrorCommand(),
		newShellInitCommand(),
		newDoctorCommand(),
	)
	return root
}

// Nothing but the directory goes to stdout: the shell function captures it.
func printPath(cmd *cobra.Command, path string) error {
	_, err := fmt.Fprintln(cmd.OutOrStdout(), path)
	return err
}

func plural(count int, noun string) string {
	if count == 1 {
		return fmt.Sprintf("%d %s", count, noun)
	}
	return fmt.Sprintf("%d %ss", count, noun)
}

// oneLine flattens an error built from several, so a report of one line per
// worktree stays one line per worktree.
func oneLine(err error) string {
	return strings.ReplaceAll(err.Error(), "\n", "; ")
}

// A listing stops being readable long before it stops being complete.
func listSome(values []string, limit int) string {
	if len(values) <= limit {
		return strings.Join(values, ", ")
	}
	return fmt.Sprintf("%s, and %d more", strings.Join(values[:limit], ", "), len(values)-limit)
}

func openRepo() (*git.Repo, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	repo, err := git.Open(cwd)
	if errors.Is(err, git.ErrNotARepository) {
		// git's wording names a path the user did not, which reads as a holt bug.
		return nil, fmt.Errorf("holt has to run inside a git repository, and %s is not one", cwd)
	}
	return repo, err
}
