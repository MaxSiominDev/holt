package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"text/tabwriter"

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

func newDoctorCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "doctor",
		Short:   "Check what is set up here and what is not",
		GroupID: groupSetup,
		Args:    cobra.NoArgs,
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
		{statusOK, "worktrees", plural(len(worktrees)-1, "worktree") + " besides the main checkout"},
		defaultBranchCheck(repo),
		driftCheck(repo),
		shellFunctionCheck(),
	}

	hookChecks, err := hookCheck(repo)
	if err != nil {
		return nil, err
	}
	checks = append(checks, hookChecks...)

	list, err := mirror.LoadList(repo)
	if err != nil {
		return nil, err
	}
	paths := list.Paths()
	if len(paths) == 0 {
		return append(checks, check{statusWarn, "mirrored paths", `none; add one with "holt mirror add <path>"`}), nil
	}

	mirrored, err := mirrorChecks(repo, mainCheckout, worktrees[1:], paths)
	if err != nil {
		return nil, err
	}
	return append(checks, mirrored...), nil
}

// shellFunctionCheck goes by the marker the snippet exports, the only trace the
// wrapper leaves in holt's environment. A startup file that loads holt without
// the marker being set means the shell predates the edit, not a missing line.
func shellFunctionCheck() check {
	if os.Getenv(shell.EnvVar) != "" {
		return check{statusOK, "shell function", "loaded"}
	}

	path, err := shell.ConfigFile(currentShell())
	if err == nil {
		if installed, err := shell.Installed(path); err == nil && installed {
			return check{statusWarn, "shell function",
				path + " loads it, but this shell started before that; open a new one"}
		}
	}
	return check{statusWarn, "shell function",
		`not loaded, so "holt cd" and "holt home" only print a path ("holt shell-init zsh --install")`}
}

func currentShell() string {
	if name := filepath.Base(os.Getenv("SHELL")); slices.Contains(shell.Supported, name) {
		return name
	}
	return "zsh"
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
	return check{statusOK, "ahead/behind columns", "available"}
}

func hookCheck(repo *git.Repo) ([]check, error) {
	dir, insideWorkTree, err := mirror.HookDir(repo)
	if err != nil {
		return nil, err
	}
	if insideWorkTree {
		return []check{{statusFail, "hook directory", dir + " is inside the working tree, so holt will not write there"}}, nil
	}

	path, state, err := mirror.InspectHook(repo)
	if err != nil {
		return nil, err
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
	}
	return []check{{statusOK, "hook directory", dir}, hook}, nil
}

func mirrorChecks(repo *git.Repo, mainCheckout string, linked []worktree.Worktree, paths []string) ([]check, error) {
	missing, err := mirror.MissingPatterns(mainCheckout, paths)
	if err != nil {
		return nil, err
	}
	listed := check{statusOK, "mirrored paths", fmt.Sprintf("%d, all present in the main checkout", len(paths))}
	if len(missing) > 0 {
		listed.status = statusWarn
		listed.detail = fmt.Sprintf("%d, %d not found in the main checkout: %v", len(paths), len(missing), missing)
	}

	hasExcludes, err := mirror.HasExcludeBlock(repo)
	if err != nil {
		return nil, err
	}
	excludes := check{statusOK, "info/exclude", "holt's block is present"}
	if !hasExcludes {
		excludes.status = statusWarn
		excludes.detail = `missing; the symlinks will show up as untracked ("holt mirror sync")`
	}

	var absent, diverted, blocked, gone int
	for _, w := range linked {
		state, err := mirror.Inspect(mainCheckout, w.Path, paths)
		if errors.Is(err, mirror.ErrWorktreeGone) {
			gone++
			continue
		}
		if err != nil {
			return nil, err
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
	}

	present := len(linked) - gone
	worktrees := plural(present, "worktree")
	links := check{statusOK, "symlinks", "complete in " + worktrees}
	switch {
	case absent > 0:
		links.status = statusWarn
		links.detail = fmt.Sprintf("missing in %d of %s (\"holt mirror sync\")", absent, worktrees)
	case diverted > 0:
		// "sync" would replace a link the user aimed elsewhere, so no fix is offered.
		links.status = statusWarn
		links.detail = fmt.Sprintf("%d of %s hold a symlink pointing elsewhere", diverted, worktrees)
	case blocked > 0:
		links.status = statusWarn
		links.detail = fmt.Sprintf("a real file is in the way in %d of %s", blocked, worktrees)
	}

	checks := []check{listed, excludes, links}
	if gone > 0 {
		checks = append(checks, check{statusWarn, "stale worktrees",
			fmt.Sprintf("%s listed whose directory is gone (\"git worktree prune\")", plural(gone, "worktree"))})
	}
	return checks, nil
}
