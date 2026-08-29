package mirror

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/MaxSiominDev/holt/internal/git"
	"github.com/MaxSiominDev/holt/internal/worktree"
)

const (
	hookName   = "post-checkout"
	hookMarker = "# installed by holt"
)

type HookState int

const (
	HookMissing HookState = iota
	HookOurs
	HookForeign
)

// holt never appends to someone else's hook: it may exit or exec before
// reaching the end, and its owner overwrites the file on their next update.
var ErrForeignHook = errors.New("a post-checkout hook that holt did not write is already installed")

// A hook written inside the project would show up untracked and get committed.
var ErrHookDirInWorkTree = errors.New("core.hooksPath points inside the working tree")

func HookDir(repo *git.Repo) (dir string, insideWorkTree bool, err error) {
	commonDir, err := repo.CommonDir()
	if err != nil {
		return "", false, err
	}

	configured, ok, err := repo.ConfigPath("core.hooksPath")
	if err != nil {
		return "", false, err
	}
	if !ok {
		// The default is never in the working tree, and asking for one here
		// would fail in a bare repository.
		return filepath.Join(commonDir, "hooks"), false, nil
	}

	worktrees, err := worktree.List(repo)
	if err != nil {
		return "", false, err
	}
	dir = configured
	if !filepath.IsAbs(dir) {
		// git resolves a relative core.hooksPath against the working tree of the moment, so
		// holt names one file, the main checkout's, which is the hook run as worktrees are made.
		if worktrees[0].Bare {
			// Which a bare repository is not, whatever its worktrees hold.
			return "", false, fmt.Errorf("core.hooksPath is %s, and holt was called in a bare repository, which is no working tree to resolve it against", configured)
		}
		dir = filepath.Join(worktrees[0].Path, dir)
	}
	dir = filepath.Clean(dir)

	// .git sits below the working tree by path, which is what separates
	// ".git/hooks" from a tracked ".husky".
	resolved := resolveExisting(dir)
	if under(resolved, commonDir) {
		return dir, false, nil
	}
	// Every worktree, not the one holt was called in, or the same path is refused
	// from the main checkout and written from a linked one.
	for _, w := range worktrees {
		// A bare repository's entry names the repository directory, already taken
		// above; its linked worktrees are working trees like any other.
		if !w.Bare && under(resolved, resolveExisting(w.Path)) {
			return dir, true, nil
		}
	}
	return dir, false, nil
}

// InspectHook reports where the hook would live and what is there.
func InspectHook(repo *git.Repo) (path string, state HookState, err error) {
	dir, _, err := HookDir(repo)
	if err != nil {
		return "", HookMissing, err
	}
	path = filepath.Join(dir, hookName)

	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return path, HookMissing, nil
	}
	if err != nil {
		return path, HookMissing, err
	}
	// Whole-line match, so a hook that merely mentions holt is not overwritten.
	for line := range strings.SplitSeq(string(content), "\n") {
		if strings.TrimSpace(line) == hookMarker {
			return path, HookOurs, nil
		}
	}
	return path, HookForeign, nil
}

type HookOptions struct {
	// Replace takes over a post-checkout hook holt did not write.
	Replace bool
}

// InstallHook writes the hook that mirrors into each worktree as git makes it.
func InstallHook(repo *git.Repo, options HookOptions) (string, error) {
	// The hook runs "holt mirror sync", which has nothing to mirror from in a bare
	// repository, so installed there it only talks on every checkout.
	if _, err := MainCheckout(repo); err != nil {
		return "", err
	}
	dir, insideWorkTree, err := HookDir(repo)
	if err != nil {
		return "", err
	}
	if insideWorkTree {
		return "", fmt.Errorf("%w (%s), where a hook holt wrote would show up as an untracked file in the project", ErrHookDirInWorkTree, dir)
	}

	path, state, err := InspectHook(repo)
	if err != nil {
		return "", err
	}
	if state == HookForeign && !options.Replace {
		return "", fmt.Errorf("%w: %s", ErrForeignHook, path)
	}

	binary, err := os.Executable()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	// Removing first fixes the mode and avoids writing through a symlink.
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	return path, os.WriteFile(path, []byte(hookScript(binary)), 0o755)
}

// The script prefers holt from PATH, falling back to the binary that wrote it.
func hookScript(fallbackBinary string) string {
	return fmt.Sprintf(`#!/bin/sh
%s
# Mirrors personal gitignored files into a worktree as git checks it out.
# Arguments: $1 previous HEAD, $2 new HEAD, $3 is 1 for a branch checkout,
# which is what "git worktree add" performs.
[ "$3" = "1" ] || exit 0

holt=$(command -v holt) || holt=%s
[ -x "$holt" ] || exit 0

top=$(git rev-parse --show-toplevel 2>/dev/null) || exit 0
"$holt" mirror sync --worktree "$top"

# A checkout has to succeed even when mirroring does not.
exit 0
`, hookMarker, ShellQuote(fallbackBinary))
}

// Single quotes take everything literally, except a quote of their own, which is
// closed, escaped and reopened. Used for the hook and for anything typed back.
func ShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// Resolves as deep as the path exists, so a hook directory reaching the working
// tree through a symlink is still caught.
func resolveExisting(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	parent := filepath.Dir(path)
	if parent == path {
		return path
	}
	return filepath.Join(resolveExisting(parent), filepath.Base(path))
}

// Compared as whole path components, so "..hooks" is a name inside base while
// "../hooks" is a step out of it.
func under(path, base string) bool {
	rel, err := filepath.Rel(base, path)
	return err == nil && filepath.IsLocal(rel)
}
