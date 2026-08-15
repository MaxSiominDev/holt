package textblock

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const (
	begin = "# >>> holt >>>"
	end   = "# <<< holt <<<"
)

func TestReplace(t *testing.T) {
	tests := []struct {
		name    string
		content string
		lines   []string
		want    string
	}{
		{
			name:    "added to an empty file",
			content: "",
			lines:   []string{"one"},
			want:    begin + "\none\n" + end + "\n",
		},
		{
			name:    "added after what was already there",
			content: "written by hand\n",
			lines:   []string{"one"},
			want:    "written by hand\n" + begin + "\none\n" + end + "\n",
		},
		{
			name:    "rewritten rather than stacked",
			content: "kept\n" + begin + "\nold\n" + end + "\n",
			lines:   []string{"new", "newer"},
			want:    "kept\n" + begin + "\nnew\nnewer\n" + end + "\n",
		},
		{
			name:    "lines on either side survive",
			content: "above\n" + begin + "\nold\n" + end + "\nbelow\n",
			lines:   []string{"new"},
			want:    "above\nbelow\n" + begin + "\nnew\n" + end + "\n",
		},
		{
			name:    "removed when there is nothing to write",
			content: "kept\n" + begin + "\nold\n" + end + "\n",
			lines:   nil,
			want:    "kept\n",
		},
		{
			name: "a hand-deleted end marker costs only the marker",
			// Swallowing everything after it would delete lines holt never wrote.
			content: begin + "\nold\nwritten by hand\n",
			lines:   []string{"new"},
			want:    "old\nwritten by hand\n" + begin + "\nnew\n" + end + "\n",
		},
		{
			name:    "an empty file stays empty",
			content: "",
			lines:   nil,
			want:    "",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := replace(test.content, begin, end, test.lines); got != test.want {
				t.Fatalf("got:\n%q\nwant:\n%q", got, test.want)
			}
		})
	}
}

func TestReplaceInFileCreatesMissingFile(t *testing.T) {
	// The directory is missing too, as ~/.config is on a fresh machine.
	path := filepath.Join(t.TempDir(), "config", "rc")

	changed, err := ReplaceInFile(path, begin, end, []string{"one"})
	if err != nil {
		t.Fatal(err)
	}

	if !changed {
		t.Error("a file that was not there reported no change")
	}
	want := begin + "\none\n" + end + "\n"
	if got := read(t, path); got != want {
		t.Fatalf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestReplaceInFileKeepsOtherLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rc")
	write(t, path, "written by hand\n")

	changed, err := ReplaceInFile(path, begin, end, []string{"one"})
	if err != nil {
		t.Fatal(err)
	}

	if !changed {
		t.Error("a file without the block reported no change")
	}
	want := "written by hand\n" + begin + "\none\n" + end + "\n"
	if got := read(t, path); got != want {
		t.Fatalf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestReplaceInFileRewritesBlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rc")
	write(t, path, "written by hand\n"+begin+"\nold\n"+end+"\n")

	changed, err := ReplaceInFile(path, begin, end, []string{"new"})
	if err != nil {
		t.Fatal(err)
	}

	if !changed {
		t.Error("a rewritten block reported no change")
	}
	want := "written by hand\n" + begin + "\nnew\n" + end + "\n"
	if got := read(t, path); got != want {
		t.Fatalf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestReplaceInFileDropsBlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rc")
	write(t, path, "written by hand\n"+begin+"\none\n"+end+"\n")

	changed, err := ReplaceInFile(path, begin, end, nil)
	if err != nil {
		t.Fatal(err)
	}

	if !changed {
		t.Error("a dropped block reported no change")
	}
	if got := read(t, path); got != "written by hand\n" {
		t.Fatalf("got:\n%q\nwant:\n%q", got, "written by hand\n")
	}
}

func TestReplaceInFileLeavesSameContentAlone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rc")
	if _, err := ReplaceInFile(path, begin, end, []string{"one"}); err != nil {
		t.Fatal(err)
	}
	// Dating it back makes a rewrite show up at any timestamp resolution.
	earlier := time.Now().Add(-time.Hour)
	if err := os.Chtimes(path, earlier, earlier); err != nil {
		t.Fatal(err)
	}
	before := modTime(t, path)

	changed, err := ReplaceInFile(path, begin, end, []string{"one"})
	if err != nil {
		t.Fatal(err)
	}

	if changed {
		t.Error("writing the same block again reported a change")
	}
	if !modTime(t, path).Equal(before) {
		t.Error("the file was rewritten although its content stayed the same")
	}
}

func TestReplaceInFileNothingToWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rc")

	changed, err := ReplaceInFile(path, begin, end, nil)
	if err != nil {
		t.Fatal(err)
	}

	if changed {
		t.Error("dropping the block from a file that was not there reported a change")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("an empty file was left behind: %v", err)
	}
}

func TestReplaceInFileMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rc")
	if _, err := ReplaceInFile(path, begin, end, []string{"one"}); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// Checked for what a startup file must never be, not for one exact mode.
	if perm := info.Mode().Perm(); perm&0o133 != 0 {
		t.Fatalf("mode is %v, want a file that is neither executable nor writable by anyone else", perm)
	}
}

func TestReplaceInFileKeepsMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rc")
	write(t, path, "a line the user wrote\n")
	// The temporary the write goes through starts at 0o600.
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := ReplaceInFile(path, "# begin", "# end", []string{"holt"}); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("the file is now %v, want the 0600 it had", got)
	}

	// Writing the path directly would hand a new file whatever the umask allows.
	fresh := filepath.Join(dir, "new-rc")
	if _, err := ReplaceInFile(fresh, "# begin", "# end", []string{"holt"}); err != nil {
		t.Fatal(err)
	}
	created, err := os.Stat(fresh)
	if err != nil {
		t.Fatal(err)
	}
	if got := created.Mode().Perm(); got != 0o600 {
		t.Errorf("a new file came out %v, want 0600", got)
	}
	// The rename must leave no temporaries behind.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Errorf("the directory holds %d files, want only the two written", len(entries))
	}
}

func TestPresentInFile(t *testing.T) {
	dir := t.TempDir()
	plain := filepath.Join(dir, "plain")
	write(t, plain, "written by hand\n")
	blocked := filepath.Join(dir, "blocked")
	write(t, blocked, "written by hand\n"+begin+"\none\n"+end+"\n")

	tests := []struct {
		name string
		path string
		want bool
	}{
		{name: "a file that is not there", path: filepath.Join(dir, "missing")},
		{name: "a file without the block", path: plain},
		{name: "a file with the block", path: blocked, want: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := PresentInFile(test.path, begin)
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("got %v, want %v", got, test.want)
			}
		})
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func modTime(t *testing.T, path string) time.Time {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.ModTime()
}

func TestReplaceInFileThroughSymlink(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "dotfiles"), 0o755); err != nil {
		t.Fatal(err)
	}
	real := filepath.Join(dir, "dotfiles", "zshrc")
	write(t, real, "written by hand\n")
	link := filepath.Join(dir, ".zshrc")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}

	if _, err := ReplaceInFile(link, begin, end, []string{"one"}); err != nil {
		t.Fatal(err)
	}

	info, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("the symlink became a regular file, detaching it from the repository holding it")
	}
	want := "written by hand\n" + begin + "\none\n" + end + "\n"
	if got := read(t, real); got != want {
		t.Fatalf("the file behind the link holds:\n%q\nwant:\n%q", got, want)
	}
}

func TestReplaceInFileThroughBrokenSymlink(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "dotfiles", "zshrc")
	link := filepath.Join(dir, ".zshrc")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}

	if _, err := ReplaceInFile(link, begin, end, []string{"one"}); err != nil {
		t.Fatal(err)
	}

	info, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Error("a link whose target was missing was replaced instead of followed")
	}
	if got := read(t, real); got != begin+"\none\n"+end+"\n" {
		t.Fatalf("the file the link names holds %q", got)
	}
}

func TestPresentInFileMentionOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rc")
	write(t, path, "# see "+begin+" for what holt writes\n")

	got, err := PresentInFile(path, begin)
	if err != nil {
		t.Fatal(err)
	}

	if got {
		t.Error("a line merely mentioning the marker was taken for holt's block")
	}
}
