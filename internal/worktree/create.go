package worktree

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/MaxSiominDev/holt/internal/git"
)

// /code/project keeps its worktrees in /code/project-worktrees/<branch>.
const worktreeDirSuffix = "-worktrees"

type CreateOptions struct {
	// SkipFetch branches off the origin/<default> already here, with no network.
	SkipFetch bool
}

// Branching off the remote-tracking ref reaches the same commit as the default
// branch without moving the main checkout off what it had open.
func Create(repo *git.Repo, branch string, options CreateOptions, progress io.Writer) (string, error) {
	main, err := Main(repo)
	if err != nil {
		return "", err
	}
	path := worktreePath(main.Path, branch)

	// git would create the branch first and only then notice the directory.
	if _, err := os.Lstat(path); err == nil {
		return "", fmt.Errorf("%s already exists, so there is nowhere to put the worktree", path)
	}

	// An existing branch carries its own history: no default branch, no network.
	if localBranchExists(repo, branch) {
		if err := repo.Run(progress, "worktree", "add", path, branch); err != nil {
			return "", err
		}
		return path, nil
	}

	// The name is taken on origin. Tracking it is what plain "git worktree add"
	// does; branching off the default would shadow it.
	if remoteBranchExists(repo, branch) {
		if !options.SkipFetch {
			// Output rather than Run: staleRemoteBranch reads git's message.
			if _, err := repo.Output("fetch", "origin", fetchRefspec(branch)); err != nil {
				return "", staleRemoteBranch(err, branch)
			}
		}
		if err := addNewBranch(repo, branch, progress, "--track", "-b", branch, path, "origin/"+branch); err != nil {
			return "", err
		}
		return path, nil
	}

	// Offline, the branch recorded at clone time has to do.
	defaultBranch, err := resolveDefaultBranch(repo, options.SkipFetch, progress)
	if err != nil {
		return "", err
	}

	if err := addNewBranch(repo, branch, progress, "-b", branch, path, "origin/"+defaultBranch); err != nil {
		return "", err
	}
	return path, nil
}

// git creates the branch before it finds out whether the directory can be made,
// so a failure otherwise leaves one behind and the retry takes Create's
// existing-branch path. Create has established there was no such branch before.
func addNewBranch(repo *git.Repo, branch string, progress io.Writer, args ...string) error {
	err := repo.Run(progress, append([]string{"worktree", "add"}, args...)...)
	if err != nil && localBranchExists(repo, branch) {
		_, _ = repo.Output("branch", "--delete", "--force", branch)
	}
	return err
}

func resolveDefaultBranch(repo *git.Repo, skipFetch bool, progress io.Writer) (string, error) {
	if skipFetch {
		return DefaultBranch(repo)
	}
	return FetchDefaultBranch(repo, progress)
}

// After a merged and deleted branch, nothing pruned origin/<branch>, so holt
// goes to track it and only then does git object. Pruning would drop every
// other stale ref too, so holt asks rather than choosing.
func staleRemoteBranch(err error, branch string) error {
	var exit *git.ExitError
	if !errors.As(err, &exit) || !strings.Contains(exit.Stderr, "couldn't find remote ref") {
		return err
	}
	return fmt.Errorf("origin has no branch %s any more, but this repository still remembers one. Run %q to forget the branches origin has dropped, then try again",
		branch, "git fetch --prune")
}

func worktreePath(mainCheckout, branch string) string {
	parent := filepath.Dir(mainCheckout)
	return filepath.Join(parent, filepath.Base(mainCheckout)+worktreeDirSuffix, branch)
}

func localBranchExists(repo *git.Repo, branch string) bool {
	_, err := repo.Output("rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	return err == nil
}
