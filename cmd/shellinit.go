package cmd

import (
	"fmt"
	"slices"
	"strings"

	"github.com/MaxSiominDev/holt/internal/shell"
	"github.com/spf13/cobra"
)

func newShellInitCommand() *cobra.Command {
	var install bool
	command := &cobra.Command{
		Use:     "shell-init <bash|zsh>",
		Short:   "Print the shell function that lets holt change directory",
		GroupID: groupSetup,
		Long: "A program cannot change the working directory of the shell that started it,\n" +
			"so holt new, holt cd and holt home only print the directory they resolved.\n" +
			"This function calls the binary and performs the cd in your own shell.\n\n" +
			"Add it to your shell startup file:\n\n" +
			`    eval "$(holt shell-init zsh)"` + "\n\n" +
			"or let holt add that line for you:\n\n" +
			"    holt shell-init zsh --install\n\n" +
			"Either way it is the line that gets stored, not the function, so upgrading\n" +
			"holt upgrades the function with it. Open a new shell afterwards.",
		Args:      cobra.ExactArgs(1),
		ValidArgs: shell.Supported,
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if !slices.Contains(shell.Supported, name) {
				return fmt.Errorf("holt has no shell function for %s, only %s",
					name, strings.Join(shell.Supported, " and "))
			}
			if !install {
				fmt.Fprint(cmd.OutOrStdout(), shell.Snippet)
				return nil
			}
			return installSnippet(cmd, name)
		},
	}
	command.Flags().BoolVar(&install, "install", false,
		"add the line that loads this function to your shell startup file")
	return command
}

func installSnippet(cmd *cobra.Command, name string) error {
	path, err := shell.ConfigFile(name)
	if err != nil {
		return err
	}
	changed, err := shell.Install(name, path)
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	if changed {
		fmt.Fprintf(out, "added the line that loads holt to %s\n", path)
	} else {
		fmt.Fprintf(out, "%s already loads holt\n", path)
	}
	fmt.Fprintln(out, "open a new shell for it to take effect")
	return nil
}
