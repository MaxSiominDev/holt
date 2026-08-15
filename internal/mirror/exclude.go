package mirror

import (
	"path/filepath"

	"github.com/MaxSiominDev/holt/internal/git"
	"github.com/MaxSiominDev/holt/internal/textblock"
)

const (
	excludeFile  = "info/exclude"
	excludeBegin = "# >>> holt mirror >>>"
	excludeEnd   = "# <<< holt mirror <<<"
)

// WriteExcludes lists the mirrored patterns in the repository's info/exclude,
// which git shares across every worktree, so the symlinks do not show up as
// untracked. A tracked .gitignore cannot replace it: a directory pattern such
// as "skills/*-local/" does not match a symlink, which git treats as a file.
func WriteExcludes(repo *git.Repo, patterns []string) error {
	commonDir, err := repo.CommonDir()
	if err != nil {
		return err
	}

	// A leading slash anchors each pattern to the repository root, so mirroring
	// CLAUDE.local.md does not also hide an unrelated sub/CLAUDE.local.md.
	anchored := make([]string, 0, len(patterns))
	for _, pattern := range patterns {
		anchored = append(anchored, "/"+pattern)
	}

	path := filepath.Join(commonDir, excludeFile)
	_, err = textblock.ReplaceInFile(path, excludeBegin, excludeEnd, anchored)
	return err
}

func HasExcludeBlock(repo *git.Repo) (bool, error) {
	commonDir, err := repo.CommonDir()
	if err != nil {
		return false, err
	}
	return textblock.PresentInFile(filepath.Join(commonDir, excludeFile), excludeBegin)
}
