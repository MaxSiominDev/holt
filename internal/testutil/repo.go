// Package testutil builds throwaway git repositories for tests.
package testutil

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MaxSiominDev/holt/internal/git"
)

// NewRepo has a single commit on branch main.
func NewRepo(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(newRoot(t), "repo")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	Git(t, dir, "init", "-b", "main")
	identify(t, dir)
	WriteFile(t, filepath.Join(dir, "README.md"), "holt test repository\n")
	Git(t, dir, "add", ".")
	Git(t, dir, "commit", "-m", "initial")
	return dir
}

// NewEmptyRepo is a fresh "git init": no commit, no HEAD, no refs.
func NewEmptyRepo(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(newRoot(t), "repo")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	Git(t, dir, "init", "-b", "main")
	identify(t, dir)
	return dir
}

func NewBareRepo(t *testing.T) string {
	t.Helper()
	root := newRoot(t)
	dir := filepath.Join(root, "repo.git")
	Git(t, root, "init", "--bare", "-b", "main", dir)
	return dir
}

// The origin is a directory, so fetching can be tested without a network.
func NewClonedRepo(t *testing.T) (clone, origin string) {
	t.Helper()
	root := newRoot(t)

	origin = filepath.Join(root, "origin")
	if err := os.Mkdir(origin, 0o755); err != nil {
		t.Fatal(err)
	}
	Git(t, origin, "init", "-b", "main")
	identify(t, origin)
	WriteFile(t, filepath.Join(origin, "README.md"), "holt test repository\n")
	Git(t, origin, "add", ".")
	Git(t, origin, "commit", "-m", "initial")

	clone = filepath.Join(root, "clone")
	Git(t, root, "clone", "--quiet", origin, clone)
	identify(t, clone)
	return clone, origin
}

// The origin is bare, since git refuses to push into a checked-out branch.
func NewPushableClone(t *testing.T) (clone, origin string) {
	t.Helper()
	root := newRoot(t)

	seed := filepath.Join(root, "seed")
	if err := os.Mkdir(seed, 0o755); err != nil {
		t.Fatal(err)
	}
	Git(t, seed, "init", "-b", "main")
	identify(t, seed)
	WriteFile(t, filepath.Join(seed, "README.md"), "holt test repository\n")
	Git(t, seed, "add", ".")
	Git(t, seed, "commit", "-m", "initial")

	origin = filepath.Join(root, "origin.git")
	Git(t, root, "clone", "--quiet", "--bare", seed, origin)
	return CloneOf(t, origin, "clone"), origin
}

// CloneOf clones into a sibling directory of the origin.
func CloneOf(t *testing.T, origin, name string) string {
	t.Helper()
	clone := filepath.Join(filepath.Dir(origin), name)
	Git(t, filepath.Dir(origin), "clone", "--quiet", origin, clone)
	identify(t, clone)
	return clone
}

// CommitTo returns the new commit.
func CommitTo(t *testing.T, dir, name, content string) string {
	t.Helper()
	WriteFile(t, filepath.Join(dir, name), content)
	Git(t, dir, "add", name)
	Git(t, dir, "commit", "-m", "add "+name)
	return Git(t, dir, "rev-parse", "HEAD")
}

// AddWorktree puts the worktree on a new branch.
func AddWorktree(t *testing.T, repo, branch string) string {
	t.Helper()
	path := filepath.Join(filepath.Dir(repo), "worktrees", branch)
	Git(t, repo, "worktree", "add", "-b", branch, path)
	return path
}

// What a fetch would leave behind, built directly so the fixture stays offline.
// The remote is configured too, since no real repository lacks one.
func SetOriginHead(t *testing.T, repo, branch string) {
	t.Helper()
	if _, err := exec.Command("git", "-C", repo, "remote", "get-url", "origin").Output(); err != nil {
		Git(t, repo, "remote", "add", "origin", filepath.Join(repo, "..", "origin.git"))
	}
	AddRemoteBranch(t, repo, branch)
	Git(t, repo, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/"+branch)
}

// Without touching origin/HEAD.
func AddRemoteBranch(t *testing.T, repo, branch string) {
	t.Helper()
	Git(t, repo, "update-ref", "refs/remotes/origin/"+branch, Git(t, repo, "rev-parse", "HEAD"))
}

// Leaves pointers to the branch dangling.
func RemoveRemoteBranch(t *testing.T, repo, branch string) {
	t.Helper()
	Git(t, repo, "update-ref", "-d", "refs/remotes/origin/"+branch)
}

// Git returns the trimmed standard output.
func Git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	// Standard output feeds other git commands, so warnings must stay out of it.
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, stderr.String())
	}
	return strings.TrimRight(string(out), "\r\n")
}

// GitStopping runs a git command meant to stop part way, whose non-zero status
// Git would otherwise take for a broken fixture.
func GitStopping(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if err := cmd.Run(); err == nil {
		t.Fatalf("git %v in %s ran to the end, so the fixture no longer stops part way", args, dir)
	}
}

// WriteFile creates parent directories as needed.
func WriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func identify(t *testing.T, dir string) {
	t.Helper()
	Git(t, dir, "config", "user.email", "holt@example.com")
	Git(t, dir, "config", "user.name", "holt")
}

// Root is a detached repository root for a shape the constructors here do not
// cover; without it the developer's own git configuration drives the fixture.
func Root(t *testing.T) string {
	t.Helper()
	return newRoot(t)
}

// The developer's configuration is detached so core.hooksPath cannot leak in, and
// so are the variables pointing git elsewhere, GIT_DIR under "rebase --exec" among
// them, which a fixture's "git init" would follow into holt's own repository.
func newRoot(t *testing.T) string {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	for _, name := range git.RedirectingVars() {
		t.Setenv(name, "")
		os.Unsetenv(name)
	}

	// macOS hides temporary directories behind /var -> /private/var, and git
	// reports the resolved path.
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return root
}
