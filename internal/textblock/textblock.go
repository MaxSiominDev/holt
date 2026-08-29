// Package textblock maintains a holt-owned section inside a file holt does not own.
package textblock

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// A file converted to CRLF carries a \r on holt's markers too, and compared as
// they stand the old block survives beside the new one forever.
func indexMarker(lines []string, marker string) int {
	return slices.IndexFunc(lines, func(line string) bool {
		return strings.TrimSuffix(line, "\r") == marker
	})
}

// No lines removes the block; a file missing its end marker loses only the
// begin marker, not everything below it.
func replace(content, begin, end string, lines []string) string {
	kept := strings.Split(content, "\n")

	// Every copy, not the first: a bad merge leaves two, and the survivor would keep
	// its entries for good while the rewritten block claims the file is in order.
	for {
		start := indexMarker(kept, begin)
		if start < 0 {
			break
		}
		last := start
		if offset := indexMarker(kept[start:], end); offset >= 0 {
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

	// A ~/.zshrc kept in a dotfiles repository is a symlink, and the rename below
	// would replace the link rather than edit what it names.
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

// A single rename wherever it can, or a failure halfway truncates ~/.zshrc; the
// temporary file shares the directory because rename cannot cross filesystems.
func writeWhole(path, content string) error {
	// An existing file keeps its mode; a new one keeps CreateTemp's 0600, since chmod
	// ignores the umask and widening it would override the user.
	existing, statErr := os.Stat(path)

	// A file the user gave a second name with a hard link: renaming would leave that
	// name on the old content, so the file itself is written, atomic swap given up.
	if statErr == nil && names(existing) > 1 {
		return writeInPlace(path, content, existing.Mode().Perm())
	}

	temporary, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*")
	if err != nil {
		// Named after the file holt was asked to write: the temporary one is gone by
		// the time the message is read, and alone it reads as a fault inside holt.
		return fmt.Errorf("writing %s: %w", path, err)
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

// A rename needs permission on the directory, not on the read-only file it
// replaces; writing in place does, so holt widens the mode and puts it back.
func writeInPlace(path, content string, mode fs.FileMode) (err error) {
	if mode&0o200 == 0 {
		if chmodErr := os.Chmod(path, mode|0o200); chmodErr != nil {
			return chmodErr
		}
		defer func() {
			if restoreErr := os.Chmod(path, mode); err == nil {
				err = restoreErr
			}
		}()
	}
	return os.WriteFile(path, []byte(content), mode)
}

// LinesInFile returns the lines between the markers and whether the block is there:
// an empty block and no block differ, and only the second means holt never wrote here.
func LinesInFile(path, begin, end string) ([]string, bool, error) {
	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}

	lines := strings.Split(string(content), "\n")
	start := indexMarker(lines, begin)
	if start < 0 {
		return nil, false, nil
	}
	// Without an end marker the rest of the file would look like the block, so holt
	// reports it present and empty instead of swallowing everything below.
	offset := indexMarker(lines[start:], end)
	if offset < 0 {
		return nil, true, nil
	}

	inside := make([]string, 0, offset-1)
	for _, line := range lines[start+1 : start+offset] {
		inside = append(inside, strings.TrimSuffix(line, "\r"))
	}
	return inside, true, nil
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
	// A whole line, matched as replace matches one, carriage return and all: a file
	// mentioning the marker does not hold the block, and one holt rewrites is not missing.
	return indexMarker(strings.Split(string(content), "\n"), begin) >= 0, nil
}
