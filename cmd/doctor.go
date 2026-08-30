package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/MaxSiominDev/holt/internal/config"
	"github.com/MaxSiominDev/holt/internal/git"
	"github.com/MaxSiominDev/holt/internal/mirror"
	"github.com/MaxSiominDev/holt/internal/shell"
	"github.com/MaxSiominDev/holt/internal/worktree"
	"github.com/spf13/cobra"
)

type checkStatus string

const (
	statusOK   checkStatus = "ok"
	statusWarn checkStatus = "warn"
	statusFail checkStatus = "fail"
)

type check struct {
	status checkStatus
	label  string
	detail string
}

// Enough to recognise the problem, short enough to fit in one table cell.
const itemsInMessage = 3

func newDoctorCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "doctor",
		Short:   "Check what is set up here and what is not",
		GroupID: groupSetup,
		Long: "Goes through this repository: the worktrees, the default branch, the\n" +
			"shell function, the post-checkout hook, the mirror list and the symlinks\n" +
			"it should have made. Nothing is changed.\n\n" +
			"Most trouble is reported as a warning and still exits zero. A failure means\n" +
			"holt cannot do its job here at all: a file or a worktree it cannot read, or\n" +
			"a hook directory inside the working tree, where it will not write.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			repo, err := openRepo()
			if err != nil {
				return err
			}
			checks, err := runChecks(repo)
			if err != nil {
				return err
			}

			table := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			failed := 0
			for _, c := range checks {
				if c.status == statusFail {
					failed++
				}
				fmt.Fprintf(table, "%s\t%s\t%s\n", c.status, c.label, c.detail)
			}
			if err := table.Flush(); err != nil {
				return err
			}
			if failed > 0 {
				return fmt.Errorf("%s failed", plural(failed, "check"))
			}
			return nil
		},
	}
}

func runChecks(repo *git.Repo) ([]check, error) {
	worktrees, err := worktree.List(repo)
	if err != nil {
		return nil, err
	}
	mainCheckout := worktrees[0].Path

	checks := []check{
		mainCheckoutCheck(worktrees[0]),
		worktreeHealthCheck(repo, mainCheckout, worktrees[1:]),
		defaultBranchCheck(repo),
		driftCheck(repo),
		shellFunctionCheck(),
	}
	checks = append(checks, mergeListChecks()...)

	// A row rather than a return, here and below: one unreadable file must not
	// take every unrelated row of the report with it.
	checks = append(checks, hookCheck(repo, worktrees[0].Bare)...)

	list, err := mirror.LoadList(repo)
	if err != nil {
		return append(checks, check{statusFail, "mirror list", err.Error()}), nil
	}
	if bad := list.Rejected(); len(bad) > 0 {
		checks = append(checks, check{statusWarn, "mirror list",
			listSome(bad, itemsInMessage) + ` (removed by the next "holt mirror add" or "rm")`})
	}
	paths := list.Paths()
	if len(paths) == 0 {
		return append(checks, check{statusWarn, "mirrored paths", `none; add one with "holt mirror add <path>"`}), nil
	}
	// The rows below read a main checkout there is none of, so they would answer
	// about the repository directory and send the user to a sync that refuses.
	if worktrees[0].Bare {
		return append(checks, check{statusWarn, "mirrored paths",
			fmt.Sprintf("%s listed, and nothing here to mirror from (\"holt mirror rm\")", plural(len(paths), "path"))}), nil
	}

	return append(checks, mirrorChecks(repo, mainCheckout, worktrees[1:], list)...), nil
}

// A gone or unreadable worktree is reported whatever else is set up, at a git call
// each: a .git leading nowhere stats fine, and only git tells it from a healthy one.
func worktreeHealthCheck(repo *git.Repo, mainCheckout string, linked []worktree.Worktree) check {
	row := check{statusOK, "worktrees", plural(len(linked), "worktree") + " besides the main checkout"}

	statuses := make([]worktree.Status, len(linked))
	for index, w := range linked {
		statuses[index] = worktree.Status{Worktree: w}
	}
	worktree.MarkState(repo, statuses)

	var gone, moved, broken, locked []string
	for _, status := range statuses {
		switch status.State {
		case worktree.StateGone:
			// A locked entry outliving its directory is what lock is for, and
			// prune spares it; said out loud anyway, since "holt ls" shows it.
			if status.Locked {
				locked = append(locked, status.Path)
				continue
			}
			// The project directory moved, told apart from a gone worktree because a prune
			// here would drop the registration for good and strand the files.
			if relocated := worktree.RelocatedPath(repo, mainCheckout, status.Worktree); relocated != "" {
				moved = append(moved, relocated)
				continue
			}
			gone = append(gone, status.Path)
		case worktree.StateBroken:
			broken = append(broken, status.Path)
		}
	}
	// A clause per kind rather than the worst one standing for the rest: the
	// remedies differ, and an unsaid kind is one "holt ls" shows and doctor does not.
	clauses := []string{row.detail}
	if len(broken) > 0 {
		row.status = statusFail
		// repair mends one way to be unreadable and not the only one, so it is offered
		// rather than prescribed, and pathless it reaches every worktree at once.
		clauses = append(clauses, fmt.Sprintf("%s git cannot read: %s (try \"git worktree repair\"; git status there says what else it could be)",
			plural(len(broken), "worktree"), listSome(broken, itemsInMessage)))
	}
	if len(moved) > 0 {
		if row.status != statusFail {
			row.status = statusWarn
		}
		// Both commands, in this order: repair registers the worktree again, and
		// only a sync repoints symlinks that still name the old main checkout.
		clauses = append(clauses, fmt.Sprintf("%s git records elsewhere, sitting where holt keeps them: %s (%s, then %s)",
			plural(len(moved), "worktree"), listSome(moved, itemsInMessage),
			shell.Named("git worktree repair "+shell.Quote(moved[0])), `"holt mirror sync"`))
	}
	if len(gone) > 0 {
		if row.status != statusFail {
			row.status = statusWarn
		}
		clauses = append(clauses, fmt.Sprintf("%s listed whose directory is gone: %s (\"git worktree prune\")",
			plural(len(gone), "worktree"), listSome(gone, itemsInMessage)))
	}
	if len(locked) > 0 {
		clauses = append(clauses, fmt.Sprintf("%d locked with the directory not there", len(locked)))
	}
	row.detail = strings.Join(clauses, ", and ")
	return row
}

// mergeListChecks read holt's own file rather than anything in this repository:
// the files it names are merged wherever this machine rebases.
func mergeListChecks() []check {
	list, err := config.LoadMergeList()
	if err != nil {
		return []check{{statusFail, "merge list", err.Error()}}
	}

	var rows []check
	// Said out loud, or a typo turns up as a rebase that put the branch back.
	if bad := list.Rejected(); len(bad) > 0 {
		rows = append(rows, check{statusWarn, "merge list",
			fmt.Sprintf("%s (%s)", listSome(bad, itemsInMessage), list.Path())})
	}

	patterns := list.Patterns()
	if len(patterns) == 0 {
		return append(rows, check{statusOK, "merged paths",
			fmt.Sprintf("nothing listed in %s, so every conflict of a rebase is yours", list.Path())})
	}
	return append(rows, check{statusOK, "merged paths", listSome(patterns, itemsInMessage)})
}

// shellFunctionCheck goes by the marker the snippet exports, the wrapper's only
// trace here; a startup file loading holt without it means the shell predates the edit.
func shellFunctionCheck() check {
	if os.Getenv(shell.EnvVar) != "" {
		return check{statusOK, "shell function", "loaded"}
	}

	unloaded := `not loaded, so "holt new", "holt cd" and "holt home" only print a path`
	name, known := shellFromEnv()
	if !known {
		// Nothing below fits a shell holt has no function for: naming a file or
		// a command would send a fish user to write ~/.zshrc for nothing.
		return check{statusWarn, "shell function", fmt.Sprintf("%s; holt has one for %s",
			unloaded, strings.Join(shell.Supported, " and "))}
	}

	path, err := shell.ConfigFile(name)
	if err == nil {
		if installed, err := shell.Installed(path); err == nil && installed {
			return check{statusWarn, "shell function",
				path + " loads it, but this shell started before that; open a new one"}
		}
	}
	return check{statusWarn, "shell function", fmt.Sprintf(`%s ("holt shell-init %s --install")`, unloaded, name)}
}

// The shell from $SHELL, and whether holt has a function for it.
func shellFromEnv() (string, bool) {
	if name := filepath.Base(os.Getenv("SHELL")); slices.Contains(shell.Supported, name) {
		return name, true
	}
	return "", false
}

func mainCheckoutCheck(main worktree.Worktree) check {
	if main.Bare {
		return check{statusWarn, "main checkout", main.Path + " is bare, so there are no files to mirror from"}
	}
	return check{statusOK, "main checkout", main.Path}
}

func defaultBranchCheck(repo *git.Repo) check {
	branch, err := worktree.DefaultBranch(repo)
	if err != nil {
		// Only the commands that branch off the default need an answer here.
		return check{statusWarn, "default branch", err.Error()}
	}
	return check{statusOK, "default branch", branch}
}

func driftCheck(repo *git.Repo) check {
	if !worktree.SupportsDrift(repo) {
		return check{statusWarn, "ahead/behind columns",
			`unavailable, "holt ls" leaves them empty; git 2.41 or newer compares branches in one call`}
	}
	// What is measured is git, not the columns: no default branch empties them too.
	return check{statusOK, "ahead/behind columns", "git compares branches in one call"}
}

func hookCheck(repo *git.Repo, bare bool) []check {
	dir, insideWorkTree, err := mirror.HookDir(repo)
	if err != nil {
		return []check{{statusFail, "hook directory", err.Error()}}
	}
	if insideWorkTree {
		return []check{{statusFail, "hook directory", dir + " is inside the working tree, so holt will not write there"}}
	}
	directory := check{statusOK, "hook directory", dir}

	path, state, err := mirror.InspectHook(repo)
	if err != nil {
		// The directory row is sound whatever the hook file turned out to be.
		return []check{directory, {statusFail, "post-checkout hook", err.Error()}}
	}
	hook := check{label: "post-checkout hook"}
	switch state {
	case mirror.HookOurs:
		hook.status, hook.detail = statusOK, path
	case mirror.HookForeign:
		hook.status = statusWarn
		hook.detail = path + `: written by something else; "holt mirror hook --replace" takes it over`
	default:
		hook.status = statusWarn
		hook.detail = `not installed; new worktrees will not be mirrored ("holt mirror hook")`
		// The row above says there is nothing to mirror from, and the command refuses too.
		if bare {
			hook.detail = "not installed, and holt does not install it in a bare repository"
		}
	}
	return []check{directory, hook}
}

func mirrorChecks(repo *git.Repo, mainCheckout string, linked []worktree.Worktree, list *mirror.List) []check {
	paths := list.Paths()
	missing, err := mirror.MissingPatterns(mainCheckout, paths)
	if err != nil {
		return []check{{statusFail, "mirrored paths", err.Error()}}
	}
	listed := check{statusOK, "mirrored paths", fmt.Sprintf("%d, all present in the main checkout", len(paths))}
	if len(missing) > 0 {
		listed.status = statusWarn
		listed.detail = fmt.Sprintf("%d, %d not found in the main checkout: %s", len(paths), len(missing), listSome(missing, itemsInMessage))
	}

	hasExcludes, matching, excludeErr := mirror.ExcludesMatch(repo, paths)
	excludes := check{statusOK, "info/exclude", "holt's block covers the mirrored paths"}
	switch {
	case excludeErr != nil:
		// The symlink loop below reads the worktrees, not this file, so it survives.
		excludes = check{statusFail, "info/exclude", excludeErr.Error()}
	case !hasExcludes:
		excludes.status = statusWarn
		excludes.detail = `missing; the symlinks will show up as untracked ("holt mirror sync")`
	case !matching:
		// Not merely present: a stale block leaves the symlinks showing as
		// untracked, which is the one thing the block is for.
		excludes.status = statusWarn
		excludes.detail = `does not cover what is mirrored; the symlinks will show up as untracked ("holt mirror sync")`
	}

	var absent, diverted, blocked, stale, gone int
	var unreadable []string
	for _, w := range linked {
		// Counted with the gone: the row above already called it unreadable, and
		// Inspect would answer about whatever repository now sits there.
		if !worktree.SameRepository(repo, w.Path) {
			gone++
			continue
		}
		state, err := mirror.Inspect(mainCheckout, w.Path, paths)
		// Counted, not reported: the row above already said it is gone.
		if errors.Is(err, mirror.ErrWorktreeGone) {
			gone++
			continue
		}
		if err != nil {
			// One unreadable worktree must not take the rest of the report with it.
			unreadable = append(unreadable, w.Path)
			continue
		}
		if len(state.Absent) > 0 {
			absent++
		}
		if len(state.Diverted) > 0 {
			diverted++
		}
		if len(state.Blocked) > 0 {
			blocked++
		}
		if len(state.Stale) > 0 {
			stale++
		}
	}

	present := len(linked) - gone - len(unreadable)
	worktrees := plural(present, "worktree")
	links := check{statusOK, "symlinks", "complete in " + worktrees}
	if present == 0 {
		links.detail = "nothing to check yet, no linked worktree"
		// The row above says what became of them, so do not deny they exist.
		if gone+len(unreadable) > 0 {
			links.detail = "not checked, no worktree left to look in"
		}
	}
	// A clause per state present, not the first standing for the rest: a diverted
	// link is permanent by design, and would hide a blocked path behind it forever.
	var wrong []string
	if absent > 0 {
		wrong = append(wrong, fmt.Sprintf("missing in %d of %s (\"holt mirror sync\")", absent, worktrees))
	}
	if diverted > 0 {
		// Two states, one clause: sync leaves a user's own link alone and repoints
		// one the moved main checkout left behind, so the advice suits both.
		wrong = append(wrong, fmt.Sprintf("a symlink points elsewhere in %d of %s (\"holt mirror sync\")", diverted, worktrees))
	}
	if blocked > 0 {
		wrong = append(wrong, fmt.Sprintf("something holt did not put there is in the way in %d of %s", blocked, worktrees))
	}
	if stale > 0 {
		wrong = append(wrong, fmt.Sprintf("a symlink leads to a file the main checkout no longer has in %d of %s (\"holt mirror sync\")", stale, worktrees))
	}
	if len(wrong) > 0 {
		links.status = statusWarn
		links.detail = strings.Join(wrong, ", and ")
	}

	checks := []check{listed, excludes, links}
	if len(unreadable) > 0 {
		checks = append(checks, check{statusFail, "unreadable worktrees",
			listSome(unreadable, itemsInMessage)})
	}
	return checks
}
