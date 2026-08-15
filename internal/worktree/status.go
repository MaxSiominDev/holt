package worktree

import (
	"errors"
	"io/fs"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/MaxSiominDev/holt/internal/git"
)

// Measured over 98 worktrees, warm cache, runs interleaved: 2 workers 1.92s,
// 4 workers 1.20s, 8 workers 1.29s, 16 workers 1.58s. Past four, the walks for
// untracked files take the disk from each other. Re-measure before raising it.
const statusWorkers = 4

// State is what stands out about a worktree, if anything.
type State string

const (
	StateClean State = ""
	// What "git worktree remove" refuses over. Ignored build output does not count.
	StateDirty State = "dirty"
	// Still listed by git, but the directory is not there.
	StateGone State = "gone"
	// The directory is there and git cannot read it as a worktree.
	StateBroken State = "broken"
)

type Status struct {
	Worktree
	Ahead    int
	Behind   int
	Compared bool // whether Ahead and Behind mean anything
	State    State
}

func Statuses(repo *git.Repo) ([]Status, error) {
	worktrees, err := List(repo)
	if err != nil {
		return nil, err
	}

	statuses := make([]Status, len(worktrees))
	for index, w := range worktrees {
		statuses[index] = Status{Worktree: w}
	}
	if err := compareToDefault(repo, statuses); err != nil {
		return nil, err
	}
	markState(repo, statuses)
	return statuses, nil
}

// SupportsDrift looks for the ahead-behind format atom, which arrived in 2.41.
// Only "unknown field name" counts: an unborn HEAD fails here too, and
// upgrading git would not help that.
func SupportsDrift(repo *git.Repo) bool {
	_, err := repo.Output("for-each-ref", "--count=1", "--format=%(ahead-behind:HEAD)", "refs/heads/")

	var exit *git.ExitError
	return !errors.As(err, &exit) || !strings.Contains(exit.Stderr, "unknown field name")
}

// One git call for every branch. A failure empties the two columns rather than
// refusing to list anything.
func compareToDefault(repo *git.Repo, statuses []Status) error {
	branch, err := DefaultBranch(repo)
	if errors.Is(err, ErrNoDefaultBranch) {
		return nil
	}
	if err != nil {
		return err
	}

	// for-each-ref fails outright when the ref it compares against is missing.
	ref := "refs/remotes/origin/" + branch
	if _, err := repo.Output("rev-parse", "--verify", "--quiet", ref); err != nil {
		return nil
	}

	// lstrip=2, not :short, which answers "heads/x" when a tag shares the name.
	out, err := repo.Output("for-each-ref", "--format=%(refname:lstrip=2)\t%(ahead-behind:"+ref+")", "refs/heads/")
	if err != nil {
		return nil // git older than 2.41 has no ahead-behind atom
	}

	counts := parseAheadBehind(out)
	for index := range statuses {
		if moved, ok := counts[statuses[index].Branch]; ok {
			statuses[index].Ahead = moved.ahead
			statuses[index].Behind = moved.behind
			statuses[index].Compared = true
		}
	}
	return nil
}

type drift struct{ ahead, behind int }

func parseAheadBehind(out string) map[string]drift {
	counts := make(map[string]drift)
	for line := range strings.SplitSeq(out, "\n") {
		branch, numbers, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		aheadText, behindText, ok := strings.Cut(numbers, " ")
		if !ok {
			continue
		}
		ahead, aheadErr := strconv.Atoi(aheadText)
		behind, behindErr := strconv.Atoi(behindText)
		if aheadErr != nil || behindErr != nil {
			continue
		}
		counts[branch] = drift{ahead: ahead, behind: behind}
	}
	return counts
}

// A worktree git cannot read is labelled, so one damaged directory does not
// blank the listing.
func markState(repo *git.Repo, statuses []Status) {
	limit := make(chan struct{}, statusWorkers)
	var wait sync.WaitGroup

	for index := range statuses {
		status := &statuses[index]
		if status.Bare {
			continue
		}
		if _, err := os.Stat(status.Path); err != nil {
			status.State = StateBroken
			if errors.Is(err, fs.ErrNotExist) {
				status.State = StateGone
			}
			continue
		}

		wait.Add(1)
		go func() {
			defer wait.Done()
			limit <- struct{}{}
			defer func() { <-limit }()

			// Untracked counts and ignored does not, matching what "git worktree
			// remove" refuses over. --no-optional-locks keeps holt from taking
			// index.lock off an IDE doing the same in the background.
			out, err := repo.At(status.Path).Output(
				"--no-optional-locks", "status", "--porcelain", "--untracked-files=normal")
			switch {
			case err != nil:
				status.State = StateBroken
			case out != "":
				status.State = StateDirty
			}
		}()
	}
	wait.Wait()
}
