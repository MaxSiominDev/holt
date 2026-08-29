package worktree

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/MaxSiominDev/holt/internal/git"
	"github.com/MaxSiominDev/holt/internal/shell"
)

// Push sends the branch checked out here to origin by name rather than through
// an upstream: on branch feature it pushes refs/heads/feature.
//
// force takes two guards rather than a plain --force: --force-with-lease refuses when
// origin moved since the last fetch, and --force-if-includes when the branch lacks what
// that fetch brought in. Without the second, any background fetch satisfies the lease.
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

// The lease is taken against the ref origin's refspec maps the branch to, not one
// of the same name, which in a --single-branch clone no fetch ever writes.
func checkLeaseUsable(repo *git.Repo, branch string) error {
	refspecs, err := repo.ConfigAll("remote.origin.fetch")
	if err != nil || len(refspecs) == 0 {
		return nil
	}
	// git stops at the first covering line, so a later wildcard does not rescue an
	// earlier line that leads nowhere.
	covering, covered := firstCovering(refspecs, branch)
	if covered {
		tracking := trackedAs(covering, branch)
		if tracking == "" {
			return fmt.Errorf("remote.origin.fetch maps %s to no ref of its own, so --force-with-lease has nothing to compare against. git stops at the first line that covers a branch, so adding another will not help. Remove or complete this one: %s",
				branch, strconv.Quote(covering))
		}
		if excluded := excludedBy(refspecs, branch); excluded != "" {
			return leaseUnderExclusion(repo, branch, tracking, excluded)
		}
		return nil
	}
	excluded := excludedBy(refspecs, branch)
	widen := `git config --add remote.origin.fetch '+refs/heads/*:refs/remotes/origin/*'`
	// The advice stops at the fetch, which is what makes --force-if-includes refuse
	// next, and to that refusal holt has nothing honest to add.
	thenPush := "Run %q and fetch, then push again"

	// Widening alone leaves an excluded branch uncovered, so name the exclusion too.
	if excluded != "" {
		return fmt.Errorf("origin's fetch refspec does not cover %s, and excludes it besides, so --force-with-lease has nothing to compare against. Remove from remote.origin.fetch: %s. "+thenPush,
			branch, excluded, widen)
	}
	return fmt.Errorf("origin's fetch refspec does not cover %s, so --force-with-lease has nothing to compare against. This is what a clone made with --single-branch or --depth looks like. "+thenPush,
		branch, widen)
}

// The first line of remote.origin.fetch covering the branch: git takes it in order, so
// one writing no useful ref is not passed over for a wildcard below. A line with no
// destination side is no match for git, and none here either.
func firstCovering(refspecs []string, branch string) (string, bool) {
	ref := "refs/heads/" + branch
	for _, refspec := range refspecs {
		source, _, hasDestination := strings.Cut(strings.TrimPrefix(refspec, "+"), ":")
		if hasDestination && patternCovers(source, ref) {
			return refspec, true
		}
	}
	return "", false
}

// The remote-tracking ref a covering line maps the branch to, git putting what the
// source star matched into the destination one; empty when the line writes no ref.
func trackedAs(refspec, branch string) string {
	ref := "refs/heads/" + branch
	source, destination, _ := strings.Cut(strings.TrimPrefix(refspec, "+"), ":")
	prefix, suffix, wildcard := strings.Cut(source, "*")
	if !wildcard {
		return destination
	}
	before, after, destinationWildcard := strings.Cut(destination, "*")
	if !destinationWildcard {
		return ""
	}
	return before + ref[len(prefix):len(ref)-len(suffix)] + after
}

// An exclusion keeps every plain fetch off the ref, so once it disagrees with origin
// nothing ordinary repairs it. Usually there is nothing to say: git takes the push.
func leaseUnderExclusion(repo *git.Repo, branch, tracking, excluded string) error {
	here := refValue(repo, tracking)
	tip, answered := originTip(repo, branch)
	if !answered {
		// Origin could not be asked, and holt will not accuse it of dropping a
		// branch on a question that never arrived; git's own error says more.
		return nil
	}
	onOrigin := tip != ""
	switch {
	case !onOrigin && here == "":
		// Nothing either side: git creates the branch against an empty lease.
		return nil
	case !onOrigin:
		// origin dropped the branch: no fetch writes a ref for one that is gone,
		// and --prune spares an excluded ref, so it goes by hand.
		return fmt.Errorf("origin's fetch refspec excludes %s, and origin no longer has that branch, so %s is left over and --force-with-lease measures against it. Neither a fetch nor \"git fetch --prune\" will clear an excluded ref. Run %s, then push again. To stop it coming back, remove from remote.origin.fetch: %s",
			branch, tracking, shell.Named("git update-ref -d "+shell.Quote(tracking)), excluded)
	case here == tip:
		return nil
	case tip == refValue(repo, "refs/heads/"+branch):
		// Already where origin has it: git reports up to date and skips the lease.
		return nil
	default:
		fetch := shell.Named("git fetch origin " + shell.Quote("+refs/heads/"+branch+":"+tracking))
		// git will not fetch into a branch a worktree has open, and dropping the exclusion
		// only lets the fetch reach it and be refused again.
		if holding := checkedOutIn(repo, tracking); holding != "" {
			return fmt.Errorf("origin's fetch refspec excludes %s, and %s no longer agrees with origin, which is what --force-with-lease measures against. No fetch will put it right where it stands: a plain one is kept off by the exclusion, and one naming it outright would be fetching into a branch checked out at %s. Check something else out there, or remove that worktree, then run %s and push again. To stop it coming back, remove from remote.origin.fetch: %s",
				branch, tracking, holding, fetch, excluded)
		}
		return fmt.Errorf("origin's fetch refspec excludes %s, and %s no longer agrees with origin, which is what --force-with-lease measures against. No plain fetch will touch it while the exclusion stands. Run %s, which names the ref outright and so goes around it, then push again. To stop it coming back, remove from remote.origin.fetch: %s",
			branch, tracking, fetch, excluded)
	}
}

// The worktree holding a ref checked out, empty when none does: git will not
// fetch into such a ref, so advising it would be useless.
func checkedOutIn(repo *git.Repo, ref string) string {
	out, err := repo.Output("for-each-ref", "--format=%(worktreepath)", ref)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// The commit a ref is at here, empty when there is none: a lease against a
// missing ref and one against a moved ref fail alike.
func refValue(repo *git.Repo, ref string) string {
	out, err := repo.Output("rev-parse", "--verify", "--quiet", ref)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// One side of a refspec. git allows a single * anywhere, not only at the end, and
// read literally such a line matches nothing, so holt would refuse a push git takes.
func patternCovers(pattern, ref string) bool {
	prefix, suffix, wildcard := strings.Cut(pattern, "*")
	if !wildcard {
		return pattern == ref
	}
	return len(ref) >= len(prefix)+len(suffix) &&
		strings.HasPrefix(ref, prefix) && strings.HasSuffix(ref, suffix)
}
