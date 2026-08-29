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
	path      string
	paths     []string
	rejected  []string
	caseFolds bool // the filesystem under this repository ignores case
}

// LoadList returns an empty list when the file does not exist yet.
func LoadList(repo *git.Repo) (*List, error) {
	commonDir, err := repo.CommonDir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(commonDir, listFile)

	// git records this when the repository is created, which beats guessing from the OS.
	caseFolds, err := repo.ConfigBool("core.ignorecase")
	if err != nil {
		return nil, err
	}

	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &List{path: path, caseFolds: caseFolds}, nil
	}
	if err != nil {
		return nil, err
	}
	list := &List{path: path, caseFolds: caseFolds}
	list.paths, list.rejected = list.parse(string(content))
	return list, nil
}

func (l *List) Paths() []string {
	return slices.Clone(l.paths)
}

// Rejected describes unreadable lines of a hand-edited file; Save drops them, so
// a caller reporting them first is the only warning there is.
func (l *List) Rejected() []string {
	return slices.Clone(l.rejected)
}

// Where the filesystem ignores case, two spellings are one file and so one entry.
// Add, Remove and parse all go by this, or they disagree about what is listed.
func (l *List) same(cleaned string) func(string) bool {
	return func(listed string) bool {
		return listed == cleaned || (l.caseFolds && strings.EqualFold(listed, cleaned))
	}
}

// Add reports whether the list changed.
func (l *List) Add(path string) (bool, error) {
	cleaned, err := CleanPath(path)
	if err != nil {
		return false, err
	}
	if slices.ContainsFunc(l.paths, l.same(cleaned)) {
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
	index := slices.IndexFunc(l.paths, l.same(cleaned))
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

// A hand-edited file keeps whatever holt can still use: refusing it whole would
// leave every mirror command, doctor included, answering with that one line.
func (l *List) parse(content string) (paths, rejected []string) {
	// A byte order mark sits before the first character, and holt's own header
	// then fails the comment test and reads as a path matching nothing.
	content = strings.TrimPrefix(content, "\ufeff")
	for number, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		cleaned, err := CleanPath(line)
		if err != nil {
			rejected = append(rejected, fmt.Sprintf("line %d: %v", number+1, err))
			continue
		}
		// Add and Remove assume one appearance apiece, or a hand-written
		// duplicate survives every "holt mirror rm".
		if slices.ContainsFunc(paths, l.same(cleaned)) {
			continue
		}
		paths = append(paths, cleaned)
	}
	return paths, rejected
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
	// A linked worktree's .git is a file, so nothing under it can be linked. Refused
	// where the user writes it, the one place holt can say why: expand only drops it.
	if first, _, _ := strings.Cut(cleaned, string(filepath.Separator)); strings.EqualFold(first, ".git") {
		return "", fmt.Errorf("holt does not mirror %s: every worktree keeps its own .git", trimmed)
	}
	// One path per line, with a leading # a comment: neither survives the round trip.
	if strings.ContainsAny(trimmed, "\n\r") {
		return "", fmt.Errorf("%q holds a line break, which the mirror list cannot store", trimmed)
	}
	if strings.HasPrefix(cleaned, "#") {
		return "", fmt.Errorf("%s starts with #, which the mirror list reads as a comment", trimmed)
	}
	// A malformed glob would otherwise fail from the hook, on every checkout.
	if _, err := filepath.Match(cleaned, ""); err != nil {
		return "", fmt.Errorf("%s is not a valid pattern: %w", trimmed, err)
	}
	// filepath.Match stops a star at a separator while info/exclude reads "**" as any
	// depth, so git would hide files holt never mirrors; name the directory instead.
	if strings.Contains(cleaned, "**") {
		return "", fmt.Errorf("%s reaches through directories for git and not for holt, so the two would disagree about what it covers. Mirror the directory itself to carry everything under it", trimmed)
	}
	if class := divergingClass(cleaned); class != "" {
		return "", fmt.Errorf("%s holds %s, so the two would disagree about what it covers. Write out the names, or mirror the directory holding them", trimmed, class)
	}
	return cleaned, nil
}

// The bracket expressions Go and git disagree about, named as the user wrote them:
// the one line goes to both filepath.Match and info/exclude, and a divergent
// reading hides or exposes the wrong files while doctor calls the setup complete.
//
// "[ab]", "[a-c]" and "[^a]" agree; only these two part company: Go takes "!" for
// an ordinary member where git negates, and a POSIX class is git's alone.
func divergingClass(pattern string) string {
	for index := 0; index < len(pattern); index++ {
		switch pattern[index] {
		case '\\':
			index++ // the escaped character is a literal, brackets included
		case '[':
			if strings.HasPrefix(pattern[index+1:], "!") {
				return `a "[!" class, which negates for git and does not for holt`
			}
			if strings.HasPrefix(pattern[index+1:], "[:") {
				return `a "[[:" class, which is git's alone and which holt reads letter by letter`
			}
		}
	}
	return ""
}

// A glob-looking pattern naming a file that is really here: the glob wins in both
// the expansion and the exclude entry, so the named file is left untracked.
func CheckAmbiguous(mainCheckout, pattern string) error {
	if !strings.ContainsAny(pattern, `*?[`) {
		return nil
	}
	if _, err := os.Lstat(filepath.Join(mainCheckout, pattern)); err != nil {
		return nil
	}
	// Quoted, since the shell eats the backslashes on the way back in.
	return fmt.Errorf("%s names a file that is here, but the pattern also reads as a glob. Write it as %s to mean the file itself",
		pattern, ShellQuote(escapeGlob(pattern)))
}

// The characters filepath.Match and gitignore give meaning to. "]" is there for
// completeness: with "[" escaped it closes nothing, and both readings take it plain.
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
