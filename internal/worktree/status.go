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

// How many "git status" run at once. Over 98 worktrees, warm cache, interleaved: 2 at
// 1.92s, 4 at 1.20s, 8 at 1.29s, 16 at 1.58s. Past four the walks for untracked files
// take the disk from each other, so re-measure before raising it.
const statusAtOnce = 4

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
	MarkState(repo, statuses)
	return statuses, nil
}

// SupportsDrift looks for the ahead-behind atom, which arrived in 2.41. Only
// "unknown field name" counts: an unborn HEAD fails here too, and no upgrade helps.
func SupportsDrift(repo *git.Repo) bool {
	_, err := repo.Output("for-each-ref", "--count=1", "--format=%(ahead-behind:HEAD)", "refs/heads/")

	var exit *git.ExitError
	return !errors.As(err, &exit) || !strings.Contains(exit.Stderr, "unknown field name")
}

// One git call for all the branches. A failure empties the two columns rather than
// refusing to list anything.
func compareToDefault(repo *git.Repo, statuses []Status) error {
	branch, err := DefaultBranch(repo)
	if errors.Is(err, ErrNoDefaultBranch) {
		return nil
	}
	if err != nil {
		return err
	}

	// DefaultBranch only answers with a branch it has already verified, and
	// for-each-ref below returns nothing useful if the ref went in between.
	ref := "refs/remotes/origin/" + branch

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

// MarkState labels each worktree with the state its directory is in, so one damaged
// directory does not blank the listing. Exported for doctor, which wants no drift.
func MarkState(repo *git.Repo, statuses []Status) {
	limit := make(chan struct{}, statusAtOnce)
	var wait sync.WaitGroup

	// Asked once for the whole listing rather than once per worktree: it is the
	// side of the comparison that is the same every time.
	here, hereErr := commonDirInfo(repo)

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

			// Asked of git, not of the .git entry alone: without one discovery walks
			// up, and a reused path answers for its own project, so the status below
			// would be about a repository the user never asked about.
			if hereErr != nil || !sameRepositoryAs(repo, here, status.Path) {
				status.State = StateBroken
				return
			}

			// Untracked counts and ignored does not, as "git worktree remove" has it,
			// and --no-optional-locks leaves index.lock to whoever else wants it.
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
