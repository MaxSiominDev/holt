package worktree

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/MaxSiominDev/holt/internal/git"
)

// Uncommit takes the last commit off the branch and leaves everything it held
// staged, which is "git reset --soft HEAD~1". The working tree is not touched,
// and the commit itself stays in the reflog.
func Uncommit(repo *git.Repo, progress io.Writer) error {
	// A bare repository has no working tree, so the changes the commit held would
	// go nowhere and survive in the reflog alone.
	bare, err := repo.Output("rev-parse", "--is-bare-repository")
	if err != nil {
		return err
	}
	if bare == "true" {
		return errors.New(`this is a bare repository, which has no index for the changes to go back to; go to a worktree with "holt cd" first`)
	}

	// A cherry-pick or revert whose conflict has been resolved leaves HEAD on the
	// branch, and git takes the unfinished operation down with the reset.
	if err := checkNothingInProgress(repo); err != nil {
		return err
	}
	// Asked for the refusal rather than the name: on a detached HEAD the reset
	// succeeds without a word, having moved HEAD alone and taken the commit off
	// no branch at all.
	if _, err := CurrentBranch(repo); err != nil {
		return err
	}

	commit, hasParent, err := describeHead(repo)
	if err != nil {
		return err
	}
	// Not "the first commit": %P is empty at the boundary of a shallow clone too,
	// where the history goes on and was simply never fetched.
	if !hasParent {
		return errors.New("the commit this branch is on has nothing behind it to go back to")
	}

	// Ended with --, since a file of that name in the worktree makes the argument
	// ambiguous and git refuses rather than choose.
	if _, err := repo.Output("reset", "--soft", "HEAD~1", "--"); err != nil {
		return err
	}
	fmt.Fprintf(progress, "holt: took back %s\n", commit)
	return nil
}

// describeHead names the commit the branch is on, as "a1b2c3d Fix the thing",
// and says whether anything is behind it.
func describeHead(repo *git.Repo) (commit string, hasParent bool, err error) {
	// %P leads because Output trims trailing newlines: last, it would vanish for
	// the parentless commit this is here to recognise. git folds a subject
	// spanning several lines into %s.
	//
	// log.showSignature, which the user may well have set, hands a signed commit
	// to gpg and repeats what gpg says on stdout, ahead of the fields below.
	out, err := repo.Output("-c", "log.showSignature=false", "log", "--max-count=1", "--format=%P%n%h%n%s")
	if err != nil {
		if _, headErr := repo.Output("rev-parse", "--verify", "--quiet", "HEAD"); headErr != nil {
			return "", false, errors.New("this branch has no commit yet, so there is nothing to take back")
		}
		return "", false, err
	}

	lines := strings.SplitN(out, "\n", 3)
	if len(lines) < 2 {
		return "", false, fmt.Errorf("git described the commit as %q, which holt cannot read", out)
	}
	parents, commit := lines[0], lines[1]
	// A third line only when there is a subject: a commit made with
	// --allow-empty-message has nothing but the hash to name it by, and the
	// trailing newline Output cut off was all that stood where the subject goes.
	if len(lines) == 3 {
		commit += " " + lines[2]
	}
	return commit, parents != "", nil
}
