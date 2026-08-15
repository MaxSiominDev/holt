// Package git runs git commands against a repository on disk.
package git

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

var ErrNotARepository = errors.New("not a git repository")

type ExitError struct {
	Args   []string
	Code   int
	Stderr string
}

func (e *ExitError) Error() string {
	detail := strings.TrimSpace(e.Stderr)
	if detail == "" {
		detail = fmt.Sprintf("exit status %d", e.Code)
	}
	return fmt.Sprintf("git %s: %s", strings.Join(e.Args, " "), detail)
}

type Repo struct {
	dir string
}

func Open(dir string) (*Repo, error) {
	// git reports resolved paths, so a Repo opened through a symlink would
	// compare unequal to every path git hands back.
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, fmt.Errorf("resolving %s: %w", dir, err)
	}

	repo := &Repo{dir: resolved}
	if _, err := repo.Output("rev-parse", "--git-dir"); err != nil {
		var exit *ExitError
		if errors.As(err, &exit) && strings.Contains(exit.Stderr, "not a git repository") {
			return nil, fmt.Errorf("%w: %s", ErrNotARepository, resolved)
		}
		return nil, err
	}
	return repo, nil
}

// At skips Open's check, so pass only paths git itself reported.
func (r *Repo) At(dir string) *Repo {
	return &Repo{dir: dir}
}

func (r *Repo) command(args []string) *exec.Cmd {
	cmd := exec.Command("git", append([]string{"-C", r.dir}, args...)...)
	// holt matches on git's messages, which git translates unless LC_ALL is C.
	// The last value of a duplicated key wins.
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	return cmd
}

// A nil stderr means git wrote straight to the user, leaving nothing to quote.
func wait(cmd *exec.Cmd, args []string, stderr *bytes.Buffer) error {
	err := cmd.Run()
	if err == nil {
		return nil
	}

	var exit *exec.ExitError
	if errors.As(err, &exit) {
		failed := &ExitError{Args: args, Code: exit.ExitCode()}
		if stderr != nil {
			failed.Stderr = stderr.String()
		}
		return failed
	}
	return fmt.Errorf("running git %s: %w", strings.Join(args, " "), err)
}

func (r *Repo) Output(args ...string) (string, error) {
	cmd := r.command(args)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := wait(cmd, args, &stderr); err != nil {
		return "", err
	}
	// Not TrimSpace, which would eat a trailing space belonging to a path.
	return strings.TrimRight(stdout.String(), "\r\n"), nil
}

// Run sends both streams to progress, keeping holt's stdout free for the path.
func (r *Repo) Run(progress io.Writer, args ...string) error {
	cmd := r.command(args)
	cmd.Stdout = progress
	cmd.Stderr = progress
	return wait(cmd, args, nil)
}

// Passthrough keeps the two streams apart, so piping works and git still colours.
func (r *Repo) Passthrough(stdout, stderr io.Writer, args ...string) error {
	cmd := r.command(args)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return wait(cmd, args, nil)
}

// GitDir is per-worktree, and holds the markers a rebase or merge leaves.
func (r *Repo) GitDir() (string, error) {
	out, err := r.Output("rev-parse", "--git-dir")
	if err != nil {
		return "", err
	}
	if filepath.IsAbs(out) {
		return filepath.Clean(out), nil
	}
	return filepath.Join(r.dir, out), nil
}

// CommonDir is shared by every worktree. git reports it relative in the main
// checkout and absolute in a linked one.
func (r *Repo) CommonDir() (string, error) {
	out, err := r.Output("rev-parse", "--git-common-dir")
	if err != nil {
		return "", err
	}
	if filepath.IsAbs(out) {
		return filepath.Clean(out), nil
	}
	return filepath.Join(r.dir, out), nil
}

func (r *Repo) Toplevel() (string, error) {
	return r.Output("rev-parse", "--show-toplevel")
}

// The boolean reports whether the key is set.
func (r *Repo) Config(key string) (string, bool, error) {
	return r.config(key)
}

// ConfigPath expands a leading ~ as git does; without it a hooksPath of
// "~/hooks" is joined onto the working tree.
func (r *Repo) ConfigPath(key string) (string, bool, error) {
	return r.config("--path", key)
}

// ConfigAll returns every value of a key that may be set more than once, such
// as a remote's fetch refspecs, in the order git reports them.
func (r *Repo) ConfigAll(key string) ([]string, error) {
	out, err := r.Output("config", "--get-all", key)
	if err != nil {
		var exit *ExitError
		if errors.As(err, &exit) && exit.Code == 1 {
			return nil, nil
		}
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}

func (r *Repo) config(args ...string) (string, bool, error) {
	out, err := r.Output(append([]string{"config", "--get"}, args...)...)
	if err != nil {
		// git config exits 1 for a key that is not set.
		var exit *ExitError
		if errors.As(err, &exit) && exit.Code == 1 {
			return "", false, nil
		}
		return "", false, err
	}
	return out, true, nil
}
