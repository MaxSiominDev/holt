package cmd

import (
	"github.com/MaxSiominDev/holt/internal/worktree"
	"github.com/spf13/cobra"
)

func newUncommitCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "uncommit",
		Short:   "Take the last commit off this branch, keeping its changes staged",
		GroupID: groupBranch,
		Long: "This is \"git reset --soft HEAD~1\": the branch goes back one commit and\n" +
			"nothing else moves. Neither the index nor the working tree is touched, so\n" +
			"what the commit held is staged again, work that was never committed stays\n" +
			"where it is, and a file staged afresh since keeps the newer version. One\n" +
			"commit, the last one, and there is no way to ask for more; for that, run git\n" +
			"yourself.\n\n" +
			"It refuses where the reset would quietly do something else: in a worktree on\n" +
			"no branch, where it would move HEAD alone and take the commit off nothing; in\n" +
			"a bare repository, which has no index for the changes to land in; where git\n" +
			"has an operation of its own left unfinished, a cherry-pick or revert among\n" +
			"them, which the reset would take down with it; and where there is nothing to\n" +
			"go back to, on a branch without a commit or on a commit without a parent.\n\n" +
			"The commit is off the branch, not gone: \"git reflog\" still names it. holt does\n" +
			"not ask origin whether it has the commit too, and a push after this one may\n" +
			"need \"holt push -f\".",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			repo, err := openRepo()
			if err != nil {
				return err
			}
			return worktree.Uncommit(repo, cmd.ErrOrStderr())
		},
	}
}
