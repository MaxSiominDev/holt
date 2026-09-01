package worktree

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
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

// What git prints where the line counts of a binary file would go.
const binaryFile = "-"

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
// returns the files it merged. The error is the first conflict it will not touch,
// or the git call that failed; the rest are merged anyway, so a caller that leaves
// the rebase standing leaves as little as possible of it to do by hand.
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
		return fmt.Errorf("%s is missing from one of the three versions being merged, so a side added or deleted the file rather than lines in it", file.path)
	}
	if file.upstream.mode != file.mine.mode {
		return fmt.Errorf("%s does not have the same file mode on the two sides", file.path)
	}
	// Agreed on by both sides and still a change, which lines have no say in.
	if file.upstream.mode != file.ancestor.mode {
		return fmt.Errorf("%s had its file mode changed, and holt merges a file only where the lines are the whole of the disagreement", file.path)
	}
	if file.ancestor.mode != regularFile && file.ancestor.mode != executableFile {
		return fmt.Errorf("%s is not a plain file", file.path)
	}

	for _, side := range []struct {
		name  string
		stage version
	}{
		{"the default branch", file.upstream},
		{"this branch", file.mine},
	} {
		removed, err := linesRemoved(root, file.ancestor.object, side.stage.object)
		if err != nil {
			return err
		}
		if removed == binaryFile {
			return fmt.Errorf("%s: git counts it as binary, so it has no lines to add to", file.path)
		}
		if removed != "0" {
			// The number is git's count of lines taken away, which a rewritten line
			// is one of, and holt refuses over the count rather than over anything
			// it worked out itself.
			return fmt.Errorf("%s: git counts %s of its lines as removed or rewritten on %s rather than added, and holt merges a file only where both sides did nothing but add. A last line with no newline of its own counts that way as soon as another follows it",
				file.path, removed, side.name)
		}
	}

	held, err := readContents(root, file)
	if err != nil {
		return err
	}
	merged, err := unionMerge(root, held)
	if err != nil {
		return err
	}
	// git carries out the union, and this is holt's own check on the result rather
	// than a promise taken on trust: a dropped line would go unnoticed to a commit.
	if !keepsEveryLine(held.ancestor, merged) {
		return fmt.Errorf("%s came out of the merge without lines it had before, so holt left it alone", file.path)
	}
	if !holdsNoLineTwice(held, merged) {
		return fmt.Errorf("%s came out of the merge holding a line more often than the two sides hold it between them, so holt left it alone", file.path)
	}

	if err := os.WriteFile(filepath.Join(top, filepath.FromSlash(file.path)), merged, 0o644); err != nil {
		return err
	}
	// Output rather than Run, which would leave git's own complaint unquoted. The
	// path goes in as a literal, or a tracked file whose name holds a "*" would
	// stage its namesakes along with it, a refused one among them.
	_, err = root.Output("add", "--", ":(literal)"+file.path)
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

// git says what a change did, so holt diffs nothing itself: the second of the two
// numbers is the lines the change took away, and a "-" in its place is a file git
// reads as binary.
func linesRemoved(root *git.Repo, ancestor, side string) (string, error) {
	out, err := root.Output("diff", "--numstat", ancestor, side)
	if err != nil {
		return "", err
	}
	// Two versions that are the same are no change at all, and git says nothing.
	if out == "" {
		return "0", nil
	}
	fields := strings.Split(out, "\t")
	if len(fields) < 2 {
		return "", fmt.Errorf("git described a change as %q, which holt cannot read", out)
	}
	return fields[1], nil
}

// The three versions read out, as against the index entries that name them.
type contents struct {
	mine     []byte
	ancestor []byte
	upstream []byte
}

func readContents(root *git.Repo, file conflictedFile) (contents, error) {
	var read contents
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
			return contents{}, err
		}
		*wanted.content = content
	}
	return read, nil
}

// git merge-file takes the two sides as files and hands the result back, and
// --union keeps both sides' lines wherever they disagree. That is a merge only
// because neither side took anything away, which is checked before we get here.
func unionMerge(root *git.Repo, held contents) ([]byte, error) {
	dir, err := os.MkdirTemp("", "holt-merge")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)

	// The order of the arguments is the order the lines come out in, and this
	// branch's own lines go first.
	names := make([]string, 0, 3)
	for index, content := range [][]byte{held.mine, held.ancestor, held.upstream} {
		name := filepath.Join(dir, strconv.Itoa(index))
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
	remaining := linesOf(merged)
	for _, line := range linesOf(ancestor) {
		index := slices.IndexFunc(remaining, func(candidate []byte) bool { return bytes.Equal(candidate, line) })
		if index < 0 {
			return false
		}
		remaining = remaining[index+1:]
	}
	return true
}

// A union repeats the lines between two insertions it cannot keep apart, and
// those are the ancestor's own lines in their own order, so the check above lets
// them through. Neither side took anything away, so each holds the ancestor's
// copies of a line along with its own additions, and the merge may hold no more
// than the two of them do together.
func holdsNoLineTwice(held contents, merged []byte) bool {
	allowed := occurrences(held.mine)
	for line, count := range occurrences(held.upstream) {
		allowed[line] += count
	}
	for line, count := range occurrences(held.ancestor) {
		allowed[line] -= count
	}
	for line, count := range occurrences(merged) {
		if count > allowed[line] {
			return false
		}
	}
	return true
}

func occurrences(content []byte) map[string]int {
	counted := make(map[string]int)
	for _, line := range linesOf(content) {
		counted[string(line)]++
	}
	return counted
}

// A newline ends a line rather than starting an empty one, and an empty file has
// no lines at all. git merge-file gives the result the last of its inputs' ending,
// so a file whose lines all came through can still end differently from the one
// they came from.
func linesOf(content []byte) [][]byte {
	if len(content) == 0 {
		return nil
	}
	return bytes.Split(bytes.TrimSuffix(content, []byte("\n")), []byte("\n"))
}
