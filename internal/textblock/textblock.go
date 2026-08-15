// Package textblock maintains a holt-owned section inside a file holt does not own.
package textblock

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// No lines removes the block. A file whose end marker was edited away loses
// only the begin marker, not everything following it.
func replace(content, begin, end string, lines []string) string {
	kept := strings.Split(content, "\n")

	if start := slices.Index(kept, begin); start >= 0 {
		last := start
		if offset := slices.Index(kept[start:], end); offset >= 0 {
			last = start + offset
		}
		kept = slices.Delete(kept, start, last+1)
	}

	// Split leaves a trailing empty element for the final newline.
	for len(kept) > 0 && kept[len(kept)-1] == "" {
		kept = kept[:len(kept)-1]
	}

	if len(lines) > 0 {
		kept = append(kept, begin)
		kept = append(kept, lines...)
		kept = append(kept, end)
	}
	if len(kept) == 0 {
		return ""
	}
	return strings.Join(kept, "\n") + "\n"
}

// A file that is not there counts as empty and is created with its directory.
func ReplaceInFile(path, begin, end string, lines []string) (bool, error) {
	existing, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}

	updated := replace(string(existing), begin, end, lines)
	if updated == string(existing) {
		return false, nil
	}

	// A ~/.zshrc kept in a dotfiles repository is a symlink, and the rename
	// below would replace the link with a plain file instead of editing what it
	// names, quietly detaching the file from the repository holding it.
	path = followLinks(path)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	return true, writeWhole(path, updated)
}

// maxLinkHops bounds a chain of symlinks pointing at one another.
const maxLinkHops = 32

func followLinks(path string) string {
	for range maxLinkHops {
		target, err := os.Readlink(path)
		if err != nil {
			return path
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(path), target)
		}
		path = target
	}
	return path
}

// One rename, or a failure halfway would leave a truncated ~/.zshrc behind. The
// temporary shares the directory because rename is atomic within a filesystem.
func writeWhole(path, content string) error {
	// An existing file keeps its mode; a new one keeps CreateTemp's 0600, since
	// chmod ignores the umask and widening it would override the user.
	existing, statErr := os.Stat(path)

	temporary, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(temporary.Name())

	if _, err := temporary.WriteString(content); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if statErr == nil {
		if err := os.Chmod(temporary.Name(), existing.Mode().Perm()); err != nil {
			return err
		}
	}
	return os.Rename(temporary.Name(), path)
}

// A file that is not there holds nothing.
func PresentInFile(path, begin string) (bool, error) {
	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	// A whole line, as replace matches it: a file that merely mentions the
	// marker does not hold the block.
	return slices.Contains(strings.Split(string(content), "\n"), begin), nil
}
