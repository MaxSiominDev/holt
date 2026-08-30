package worktree

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/MaxSiominDev/holt/internal/config"
	"github.com/MaxSiominDev/holt/internal/git"
)

// The versions git leaves in the index for a conflicted file. A rebase replays
// this branch's commits onto the default branch, so what is checked out at the
// time is the default branch's version and what is being applied is this branch's.
const (
	stageAncestor = "1"
	stageUpstream = "2"
	stageMine     = "3"
)

// Anything else conflicts over what it points at or which commit it holds, and
// no amount of merging lines settles that.
const (
	regularFile    = "100644"
	executableFile = "100755"
)

type version struct {
	mode string
	// The object git named, which is not a file anywhere.
	object string
}

type conflictedFile struct {
	path     string
	ancestor version
	upstream version
	mine     version
}

// autoMerge resolves and stages the conflicts holt is allowed to take on, and
// returns the files it merged. The error names the first conflict it will not
// touch; the rest are merged anyway, so a caller that leaves the rebase standing
// leaves as little as possible of it to do by hand.
func autoMerge(repo *git.Repo, list *config.MergeList) ([]string, error) {
	top, err := repo.Toplevel()
	if err != nil {
		return nil, err
	}
	// git lists only what is under the directory it runs in, and a conflict in the
	// root is invisible from a subdirectory holt happens to be called in.
	root := repo.At(top)

	conflicts, err := unmergedFiles(root)
	if err != nil {
		return nil, err
	}

	var merged []string
	var refused error
	for _, file := range conflicts {
		err := mergeAdditions(root, top, file, list)
		if err == nil {
			merged = append(merged, file.path)
			continue
		}
		if refused == nil {
			refused = err
		}
	}
	return merged, refused
}

// The one conflict holt takes on: a file the user listed, that both sides did
// nothing to but add lines. Every other shape is refused by name and left as git
// left it.
func mergeAdditions(root *git.Repo, top string, file conflictedFile, list *config.MergeList) error {
	if !list.Matches(file.path) {
		return fmt.Errorf("%s is not one of the files holt merges itself", file.path)
	}
	if file.ancestor.object == "" || file.upstream.object == "" || file.mine.object == "" {
		return fmt.Errorf("%s is missing from one of the three versions being merged, so there is nothing to add lines to", file.path)
	}
	if file.ancestor.mode != file.upstream.mode || file.ancestor.mode != file.mine.mode {
		return fmt.Errorf("%s does not have the same file mode on both sides", file.path)
	}
	if file.ancestor.mode != regularFile && file.ancestor.mode != executableFile {
		return fmt.Errorf("%s is not a plain file", file.path)
	}

	for _, side := range []struct {
		name    string
		version version
	}{
		{"the default branch", file.upstream},
		{"this branch", file.mine},
	} {
		added, err := addsOnly(root, file.ancestor.object, side.version.object)
		if err != nil {
			return err
		}
		if !added {
			return fmt.Errorf("%s: %s changed lines that were already there, and holt merges a file only where both sides did nothing but add",
				file.path, side.name)
		}
	}

	contents, err := readVersions(root, file)
	if err != nil {
		return err
	}
	merged, err := unionMerge(root, contents)
	if err != nil {
		return err
	}
	// git carries out the union, and this is holt's own check on the result rather
	// than a promise taken on trust: a dropped line would go unnoticed to a commit.
	if !keepsEveryLine(contents.ancestor, merged) {
		return fmt.Errorf("%s came out of the merge without lines it had before, so holt left it alone", file.path)
	}

	if err := os.WriteFile(filepath.Join(top, filepath.FromSlash(file.path)), merged, 0o644); err != nil {
		return err
	}
	// Output rather than Run, which would leave git's own complaint unquoted.
	_, err = root.Output("add", "--", file.path)
	return err
}

func unmergedFiles(root *git.Repo) ([]conflictedFile, error) {
	// A path may hold anything a byte can be, and git quotes nothing under -z.
	out, err := root.Output("ls-files", "--unmerged", "-z")
	if err != nil {
		return nil, err
	}

	var files []conflictedFile
	for _, entry := range strings.Split(out, "\x00") {
		if entry == "" {
			continue
		}
		// "<mode> <object> <stage>\t<path>", one line per version of the file.
		meta, path, found := strings.Cut(entry, "\t")
		fields := strings.Fields(meta)
		if !found || len(fields) != 3 {
			return nil, fmt.Errorf("git named an unmerged file as %q, which holt cannot read", entry)
		}

		index := slices.IndexFunc(files, func(file conflictedFile) bool { return file.path == path })
		if index < 0 {
			files = append(files, conflictedFile{path: path})
			index = len(files) - 1
		}
		files[index].keep(fields[2], version{mode: fields[0], object: fields[1]})
	}
	return files, nil
}

func (f *conflictedFile) keep(stage string, held version) {
	switch stage {
	case stageAncestor:
		f.ancestor = held
	case stageUpstream:
		f.upstream = held
	case stageMine:
		f.mine = held
	}
}

// git says what a change did, so holt diffs nothing itself: the second number is
// the lines the change took away, and a "-" in its place means a binary file.
func addsOnly(root *git.Repo, ancestor, side string) (bool, error) {
	out, err := root.Output("diff", "--numstat", ancestor, side)
	if err != nil {
		return false, err
	}
	// Two versions that are the same are no change at all, and git says nothing.
	if out == "" {
		return true, nil
	}
	fields := strings.Split(out, "\t")
	if len(fields) < 2 {
		return false, fmt.Errorf("git described a change as %q, which holt cannot read", out)
	}
	return fields[1] == "0", nil
}

type versions struct {
	mine     []byte
	ancestor []byte
	upstream []byte
}

func readVersions(root *git.Repo, file conflictedFile) (versions, error) {
	var read versions
	for _, wanted := range []struct {
		object  string
		content *[]byte
	}{
		{file.mine.object, &read.mine},
		{file.ancestor.object, &read.ancestor},
		{file.upstream.object, &read.upstream},
	} {
		content, err := root.OutputRaw("cat-file", "blob", wanted.object)
		if err != nil {
			return versions{}, err
		}
		*wanted.content = content
	}
	return read, nil
}

// git merge-file takes the two sides as files and hands the result back, and
// --union keeps both sides' lines wherever they disagree. That is a merge only
// because neither side took anything away, which is checked before we get here.
func unionMerge(root *git.Repo, contents versions) ([]byte, error) {
	dir, err := os.MkdirTemp("", "holt-merge")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)

	// The order of the arguments is the order the lines come out in, and this
	// branch's own lines go first.
	names := make([]string, 0, 3)
	for index, content := range [][]byte{contents.mine, contents.ancestor, contents.upstream} {
		name := filepath.Join(dir, fmt.Sprintf("%d", index))
		if err := os.WriteFile(name, content, 0o600); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return root.OutputRaw(append([]string{"merge-file", "--union", "--stdout"}, names...)...)
}

// Every line the ancestor had has to come through the merge, in the order it had
// them.
func keepsEveryLine(ancestor, merged []byte) bool {
	remaining := bytes.Split(merged, []byte("\n"))
	for _, line := range bytes.Split(ancestor, []byte("\n")) {
		index := slices.IndexFunc(remaining, func(candidate []byte) bool { return bytes.Equal(candidate, line) })
		if index < 0 {
			return false
		}
		remaining = remaining[index+1:]
	}
	return true
}
