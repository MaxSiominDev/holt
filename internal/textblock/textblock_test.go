package textblock

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
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
			// What a bad merge leaves: the second copy keeps its entries for good,
			// while the block holt rewrites reads as healthy.
			name:    "both copies of the block go",
			content: begin + "\nold\n" + end + "\nkept\n" + begin + "\nolder\n" + end + "\n",
			lines:   []string{"new"},
			want:    "kept\n" + begin + "\nnew\n" + end + "\n",
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
	// The temporary the write goes through starts at 0o600, so a mode that is
	// not 0o600 is the only one that shows the existing file being followed.
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}

	if _, err := ReplaceInFile(path, "# begin", "# end", []string{"holt"}); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o640 {
		t.Errorf("the file is now %v, want the 0640 it had", got)
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

func TestReplaceInFileThroughAChainOfSymlinks(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "dotfiles-rc")
	write(t, real, "a line the user wrote\n")
	// A link to a link, as a dotfiles directory plus a per-machine override leaves:
	// one hop only writes over the middle link and takes it out of the chain.
	middle := filepath.Join(dir, "middle")
	if err := os.Symlink(real, middle); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "rc")
	if err := os.Symlink(middle, path); err != nil {
		t.Fatal(err)
	}

	if _, err := ReplaceInFile(path, "# begin", "# end", []string{"holt"}); err != nil {
		t.Fatal(err)
	}

	if content := read(t, real); !strings.Contains(content, "holt") {
		t.Errorf("the file at the end of the chain reads %q", content)
	}
	for _, link := range []string{path, middle} {
		info, err := os.Lstat(link)
		if err != nil || info.Mode()&os.ModeSymlink == 0 {
			t.Errorf("%s is no longer a symlink (%v)", link, err)
		}
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

func TestReplaceFindsMarkersInCRLFFile(t *testing.T) {
	// What an editor set to CRLF leaves behind after holt wrote the block once.
	content := "# comment\r\n" + begin + "\r\n/A.local\r\n" + end + "\r\n"

	got := replace(content, begin, end, []string{"/B.local"})

	if strings.Count(got, begin) != 1 {
		t.Fatalf("got %q, want the old block replaced rather than a second one added", got)
	}
	if strings.Contains(got, "/A.local") {
		t.Fatalf("got %q, want the stale entry gone", got)
	}
}

func TestPresentInFileFindsMarkerInCRLFFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "exclude")
	if err := os.WriteFile(path, []byte("# comment\r\n"+begin+"\r\n/A.local\r\n"+end+"\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	present, err := PresentInFile(path, begin)
	if err != nil {
		t.Fatal(err)
	}

	// doctor reads this. Reported missing, it sends the user to repair a block
	// that is there and that holt is perfectly able to rewrite.
	if !present {
		t.Fatal("the block was reported missing in a file holt itself wrote")
	}
}

func TestReplaceInFileKeepsHardLink(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rc")
	dotfile := filepath.Join(dir, "dotfiles-rc")
	write(t, path, "a line the user wrote\n")
	// The arrangement a dotfiles directory makes with a hard link: one file,
	// two names. Renaming over one of them leaves the other on the old content.
	if err := os.Link(path, dotfile); err != nil {
		t.Fatal(err)
	}

	if _, err := ReplaceInFile(path, "# begin", "# end", []string{"holt"}); err != nil {
		t.Fatal(err)
	}

	content, err := os.ReadFile(dotfile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "holt") {
		t.Fatalf("the other name holds %q, so the edit never reached the dotfiles copy", content)
	}
	if !strings.Contains(string(content), "a line the user wrote") {
		t.Errorf("the user's own line is gone from %q", content)
	}
}

func TestReplaceInFileKeepsReadOnlyHardLink(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rc")
	dotfile := filepath.Join(dir, "dotfiles-rc")
	write(t, path, "a line the user wrote\n")
	if err := os.Link(path, dotfile); err != nil {
		t.Fatal(err)
	}
	// A dotfiles directory keeping its files read-only: a rename never needed
	// permission on the old file, so this used to go through and has to keep doing so.
	if err := os.Chmod(path, 0o444); err != nil {
		t.Fatal(err)
	}

	if _, err := ReplaceInFile(path, "# begin", "# end", []string{"holt"}); err != nil {
		t.Fatalf("a read-only file with a second name was refused: %v", err)
	}

	content, err := os.ReadFile(dotfile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "holt") {
		t.Fatalf("the other name holds %q", content)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o444 {
		t.Errorf("the file is now %v, want the 0444 it had", got)
	}
}

func TestReplaceInFileNamesTheFileItCouldNotWrite(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes a directory whatever its mode says")
	}
	dir := t.TempDir()
	closed := filepath.Join(dir, "info")
	if err := os.Mkdir(closed, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(closed, "exclude")
	write(t, path, "written by hand\n")
	if err := os.Chmod(closed, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(closed, 0o755) })

	_, err := ReplaceInFile(path, "# begin", "# end", []string{"holt"})

	if err == nil {
		t.Fatal("a directory holt cannot write was reported as written")
	}
	// The temporary file is gone by the time the message is read, so naming it alone
	// reads as a fault inside holt. Ending the name, since the temporary one begins with it.
	if !strings.Contains(err.Error(), path+":") {
		t.Errorf("error %q names no file the user can go and look at", err)
	}
}

func TestReplaceInFileHardLinkInReadOnlyDirectory(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes a directory whatever its mode says")
	}
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	if err := os.Mkdir(home, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, "rc")
	dotfile := filepath.Join(dir, "dotfiles-rc")
	write(t, path, "a line the user wrote\n")
	if err := os.Link(path, dotfile); err != nil {
		t.Fatal(err)
	}
	// Writing the file itself asks nothing of the directory holding it, so a
	// closed directory is no reason to refuse.
	if err := os.Chmod(home, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(home, 0o755) })

	if _, err := ReplaceInFile(path, "# begin", "# end", []string{"holt"}); err != nil {
		t.Fatalf("a file with a second name was refused over its directory: %v", err)
	}

	content, err := os.ReadFile(dotfile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "holt") {
		t.Fatalf("the other name holds %q", content)
	}
}
