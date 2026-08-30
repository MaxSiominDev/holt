// Package config reads the settings holt keeps for the whole machine, outside
// any repository.
package config

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// Under XDG_CONFIG_HOME when it is set, and ~/.config otherwise. os.UserConfigDir
// would answer ~/Library/Application Support on macOS, which is not where a
// command line tool's settings are looked for.
const listFile = "holt/merge.list"

// MergeList names the files holt may merge itself when a rebase conflicts.
type MergeList struct {
	path     string
	patterns []string
	rejected []string
}

// LoadMergeList returns an empty list when there is no file, which is holt as it
// arrives: nothing is merged automatically until the file names something.
func LoadMergeList() (*MergeList, error) {
	dir, err := configDir()
	if err != nil {
		return nil, err
	}
	list := &MergeList{path: filepath.Join(dir, filepath.FromSlash(listFile))}

	content, err := os.ReadFile(list.path)
	if errors.Is(err, os.ErrNotExist) {
		return list, nil
	}
	if err != nil {
		return nil, err
	}
	list.patterns, list.rejected = parse(string(content))
	return list, nil
}

func (l *MergeList) Path() string {
	return l.path
}

func (l *MergeList) Patterns() []string {
	return slices.Clone(l.patterns)
}

// Rejected describes the lines holt could not read, which are otherwise silently
// left out of a file the user wrote by hand.
func (l *MergeList) Rejected() []string {
	return slices.Clone(l.rejected)
}

// Matches takes a path as git reports it: relative to the repository root, with
// forward slashes on every platform.
func (l *MergeList) Matches(file string) bool {
	if l == nil {
		return false
	}
	return slices.ContainsFunc(l.patterns, func(pattern string) bool {
		// The patterns were checked on the way in, so Match can only match or not.
		matched, _ := path.Match(pattern, file)
		return matched
	})
}

func configDir() (string, error) {
	// The specification calls a relative value invalid, and following one would
	// have holt read a different file from every directory it is run in.
	if dir := os.Getenv("XDG_CONFIG_HOME"); filepath.IsAbs(dir) {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config"), nil
}

func parse(content string) (patterns, rejected []string) {
	// A byte order mark sits before the first character, and the line it belongs
	// to then fails the comment test and reads as a pattern matching nothing.
	content = strings.TrimPrefix(content, "\ufeff")
	for number, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		pattern, err := cleanPattern(line)
		if err != nil {
			rejected = append(rejected, fmt.Sprintf("line %d: %v", number+1, err))
			continue
		}
		if slices.Contains(patterns, pattern) {
			continue
		}
		patterns = append(patterns, pattern)
	}
	return patterns, rejected
}

// Matched against the paths git reports, which are slash separated everywhere,
// so this is the "path" package's business and not "path/filepath"'s.
func cleanPattern(pattern string) (string, error) {
	slashed := filepath.ToSlash(pattern)
	if strings.HasPrefix(slashed, "/") || filepath.IsAbs(pattern) {
		return "", fmt.Errorf("%s has to be given relative to the repository root", pattern)
	}
	cleaned := path.Clean(slashed)
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("%s points outside the repository", pattern)
	}
	// Every other file holt leaves to the user: the merge below keeps both sides'
	// lines, which is a sane thing to do to a list of entries and little else.
	if !strings.EqualFold(path.Ext(cleaned), ".md") {
		return "", fmt.Errorf("%s is not a .md file, and holt merges nothing else by itself", pattern)
	}
	// Go reads "**" as one "*", which stops at a directory separator, so a pattern
	// written for git would quietly cover less than it looks like it does.
	if strings.Contains(cleaned, "**") {
		return "", fmt.Errorf("%s reads ** as a single *, which does not reach through directories; name the directories instead", pattern)
	}
	if _, err := path.Match(cleaned, ""); err != nil {
		return "", fmt.Errorf("%s is not a valid pattern: %w", pattern, err)
	}
	return cleaned, nil
}
