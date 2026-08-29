package mirror

import (
	"path/filepath"
	"slices"

	"github.com/MaxSiominDev/holt/internal/git"
	"github.com/MaxSiominDev/holt/internal/textblock"
)

const (
	excludeFile  = "info/exclude"
	excludeBegin = "# >>> holt mirror >>>"
	excludeEnd   = "# <<< holt mirror <<<"
)

// WriteExcludes lists the mirrored patterns in the info/exclude git shares across
// worktrees, so the symlinks do not show up as untracked. .gitignore is tracked,
// and would carry one author's own paths to everybody else.
func WriteExcludes(repo *git.Repo, patterns []string) error {
	commonDir, err := repo.CommonDir()
	if err != nil {
		return err
	}

	path := filepath.Join(commonDir, excludeFile)
	_, err = textblock.ReplaceInFile(path, excludeBegin, excludeEnd, anchor(patterns))
	return err
}

// ExcludesMatch reports whether the block covers exactly the paths given: a stale one
// leaves the symlinks showing as untracked, the one thing the block is there to stop.
func ExcludesMatch(repo *git.Repo, patterns []string) (present, matching bool, err error) {
	commonDir, err := repo.CommonDir()
	if err != nil {
		return false, false, err
	}
	lines, present, err := textblock.LinesInFile(filepath.Join(commonDir, excludeFile), excludeBegin, excludeEnd)
	if err != nil || !present {
		return false, false, err
	}
	return true, slices.Equal(lines, anchor(patterns)), nil
}

// A leading slash anchors each pattern to the repository root, so mirroring
// CLAUDE.local.md does not also hide an unrelated sub/CLAUDE.local.md.
func anchor(patterns []string) []string {
	anchored := make([]string, 0, len(patterns))
	for _, pattern := range patterns {
		anchored = append(anchored, "/"+pattern)
	}
	return anchored
}
