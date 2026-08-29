package cmd

import (
	"fmt"
	"io"

	"github.com/MaxSiominDev/holt/internal/forge"
	"github.com/MaxSiominDev/holt/internal/git"
	"github.com/MaxSiominDev/holt/internal/worktree"
	"github.com/spf13/cobra"
)

func newOpenCommand() *cobra.Command {
	var printOnly bool
	command := &cobra.Command{
		Use:     "open",
		Short:   "Open the request for this branch, or the page for raising one",
		GroupID: groupBranch,
		Long: "Asks the forge's own tool whether a request is already open from this\n" +
			"branch, and opens that one: gh on GitHub, glab on GitLab. Either holds the\n" +
			"authentication, so holt needs no token of its own.\n\n" +
			"Without the tool, or without an answer from it, the page for raising a\n" +
			"request is opened instead. That page needs nothing installed.\n\n" +
			"The forge is recognised by its host name. Set " + forge.KindKey + " to github or\n" +
			"gitlab for a host that is named after neither, or after the wrong one, and\n" +
			forge.HostKey + " where the remote carries a name only ssh understands, such as\n" +
			"a Host alias from ~/.ssh/config.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			repo, err := openRepo()
			if err != nil {
				return err
			}
			target, err := changeRequestURL(repo, cmd.ErrOrStderr())
			if err != nil {
				return err
			}

			if printOnly {
				_, err := fmt.Fprintln(cmd.OutOrStdout(), target)
				return err
			}
			fmt.Fprintln(cmd.ErrOrStderr(), target)
			return forge.Open(target)
		},
	}
	command.Flags().BoolVar(&printOnly, "print", false, "print the address instead of opening it")
	return command
}

func changeRequestURL(repo *git.Repo, notes io.Writer) (string, error) {
	branch, err := worktree.CurrentBranch(repo)
	if err != nil {
		return "", err
	}
	defaultBranch, err := worktree.DefaultBranch(repo)
	if err != nil {
		return "", err
	}
	if branch == defaultBranch {
		return "", fmt.Errorf("this worktree is on %s, which is what a request would be raised against", defaultBranch)
	}
	return forge.ChangeRequestURL(repo, branch, defaultBranch, notes)
}
