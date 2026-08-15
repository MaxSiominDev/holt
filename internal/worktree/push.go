package worktree

import (
	"fmt"
	"io"
	"strings"

	"github.com/MaxSiominDev/holt/internal/git"
)

// Push sends the branch checked out here to origin by name: on branch feature
// it runs "git push origin feature".
//
// force takes two guards rather than a plain --force. --force-with-lease
// refuses when the remote branch is not where the last fetch left it.
// --force-if-includes refuses when the local branch does not contain what that
// fetch brought in: without it the lease is satisfied by any fetch at all,
// including a background one from an IDE, and the push goes through over a
// colleague's commit.
func Push(repo *git.Repo, force bool, progress io.Writer) error {
	branch, err := CurrentBranch(repo)
	if err != nil {
		return err
	}
	if force {
		if err := checkLeaseUsable(repo, branch); err != nil {
			return err
		}
	}

	// Full refs on both sides: a bare name is read as a flag when the branch is
	// called something like "-f", and is ambiguous when a tag shares the name.
	refspec := "refs/heads/" + branch + ":refs/heads/" + branch

	args := []string{"push", "origin", refspec}
	if force {
		args = append(args, "--force-with-lease", "--force-if-includes")
	}
	return repo.Run(progress, args...)
}

// A lease is taken against the remote-tracking ref that origin's fetch refspec
// maps the branch to, not against one of the same name. In a --single-branch
// clone the refspec covers one branch, so every other one is rejected as "stale
// info" and no amount of fetching helps.
func checkLeaseUsable(repo *git.Repo, branch string) error {
	refspecs, err := repo.ConfigAll("remote.origin.fetch")
	if err != nil || len(refspecs) == 0 {
		return nil
	}
	for _, refspec := range refspecs {
		if refspecCovers(refspec, "refs/heads/"+branch) {
			return nil
		}
	}
	return fmt.Errorf("origin's fetch refspec does not cover %s, so --force-with-lease has nothing to compare against. This is what a clone made with --single-branch looks like. Run %q and fetch once, then try again",
		branch, `git config --add remote.origin.fetch '+refs/heads/*:refs/remotes/origin/*'`)
}

func refspecCovers(refspec, ref string) bool {
	source, _, found := strings.Cut(strings.TrimPrefix(refspec, "+"), ":")
	if !found {
		return false
	}
	if prefix, wildcard := strings.CutSuffix(source, "*"); wildcard {
		return strings.HasPrefix(ref, prefix)
	}
	return source == ref
}
