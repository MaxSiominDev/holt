// Package mirror symlinks personal, gitignored files back to the main checkout,
// since git does not carry untracked files into a worktree it creates.
package mirror

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/MaxSiominDev/holt/internal/git"
)

// Per-repository and untracked, in the shared repository directory.
const listFile = "holt/mirror.list"

const listHeader = `# Paths mirrored into every worktree of this repository.
# Managed by holt: see "holt mirror add" and "holt mirror rm".
`

// An entry may be a glob, expanded against the main checkout.
type List struct {
	path  string
	paths []string
}

// LoadList returns an empty list when the file does not exist yet.
func LoadList(repo *git.Repo) (*List, error) {
	commonDir, err := repo.CommonDir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(commonDir, listFile)

	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &List{path: path}, nil
	}
	if err != nil {
		return nil, err
	}
	paths, err := parseList(string(content))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &List{path: path, paths: paths}, nil
}

func (l *List) Paths() []string {
	return slices.Clone(l.paths)
}

// Add reports whether the list changed.
func (l *List) Add(path string) (bool, error) {
	cleaned, err := CleanPath(path)
	if err != nil {
		return false, err
	}
	if slices.Contains(l.paths, cleaned) {
		return false, nil
	}
	l.paths = append(l.paths, cleaned)
	return true, nil
}

// Remove reports whether the list changed.
func (l *List) Remove(path string) (bool, error) {
	cleaned, err := CleanPath(path)
	if err != nil {
		return false, err
	}
	index := slices.Index(l.paths, cleaned)
	if index < 0 {
		return false, nil
	}
	l.paths = slices.Delete(l.paths, index, index+1)
	return true, nil
}

func (l *List) Save() error {
	if err := os.MkdirAll(filepath.Dir(l.path), 0o755); err != nil {
		return err
	}
	content := listHeader
	if len(l.paths) > 0 {
		content += strings.Join(l.paths, "\n") + "\n"
	}
	return os.WriteFile(l.path, []byte(content), 0o644)
}

// A hand-edited file is held to the same rules, so a bad entry is reported once
// instead of failing every later command.
func parseList(content string) ([]string, error) {
	var paths []string
	for number, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		cleaned, err := CleanPath(line)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", number+1, err)
		}
		paths = append(paths, cleaned)
	}
	return paths, nil
}

// CleanPath rejects anything that would reach outside the repository.
func CleanPath(path string) (string, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return "", errors.New("the path is empty")
	}
	if filepath.IsAbs(trimmed) {
		return "", fmt.Errorf("%s must be given relative to the repository root", trimmed)
	}
	cleaned := filepath.Clean(trimmed)
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%s points outside the repository", trimmed)
	}
	if cleaned == "." {
		return "", errors.New("the repository root itself cannot be mirrored")
	}
	// A malformed glob would otherwise fail from the hook, on every checkout.
	if _, err := filepath.Match(cleaned, ""); err != nil {
		return "", fmt.Errorf("%s is not a valid pattern: %w", trimmed, err)
	}
	return cleaned, nil
}

// A pattern that reads as a glob while a file of exactly that name is here: the
// glob wins in both the expansion and the exclude entry, so holt would mirror
// some other file and leave the named one untracked.
func CheckAmbiguous(mainCheckout, pattern string) error {
	if !strings.ContainsAny(pattern, `*?[`) {
		return nil
	}
	if _, err := os.Lstat(filepath.Join(mainCheckout, pattern)); err != nil {
		return nil
	}
	return fmt.Errorf("%s names a file that is here, but the pattern also reads as a glob. Write it as %s to mean the file itself",
		pattern, escapeGlob(pattern))
}

// The characters filepath.Match and gitignore both read as pattern syntax.
func escapeGlob(pattern string) string {
	var escaped strings.Builder
	for _, r := range pattern {
		if strings.ContainsRune(`*?[]\`, r) {
			escaped.WriteByte('\\')
		}
		escaped.WriteRune(r)
	}
	return escaped.String()
}
