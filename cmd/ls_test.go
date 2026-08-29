package cmd

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/MaxSiominDev/holt/internal/testutil"
	"github.com/MaxSiominDev/holt/internal/worktree"
)

func TestListDriftAndDirty(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.SetOriginHead(t, main, "main")
	feature := testutil.AddWorktree(t, main, "feature")
	testutil.WriteFile(t, filepath.Join(feature, "one.txt"), "one\n")
	testutil.Git(t, feature, "add", "one.txt")
	testutil.Git(t, feature, "commit", "-m", "one")
	testutil.WriteFile(t, filepath.Join(feature, "README.md"), "edited\n")
	t.Chdir(main)

	stdout, _ := runHolt(t, "ls")

	row := rowFor(t, stdout, "feature")
	if !strings.Contains(row, "+1 / 0") {
		t.Errorf("row %q does not show the branch one commit ahead", row)
	}
	if !strings.Contains(row, "*") {
		t.Errorf("row %q does not mark the uncommitted change", row)
	}
}

func TestListBehindCount(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.SetOriginHead(t, main, "main")
	testutil.AddWorktree(t, main, "feature")
	for _, name := range []string{"first.txt", "second.txt"} {
		testutil.WriteFile(t, filepath.Join(main, name), name+"\n")
		testutil.Git(t, main, "add", name)
		testutil.Git(t, main, "commit", "-m", "add "+name)
	}
	testutil.Git(t, main, "update-ref", "refs/remotes/origin/main", testutil.Git(t, main, "rev-parse", "HEAD"))
	t.Chdir(main)

	stdout, _ := runHolt(t, "ls")

	if row := rowFor(t, stdout, "feature"); !strings.Contains(row, "0 / -2") {
		t.Fatalf("row %q does not show the branch two commits behind", row)
	}
}

func TestListPorcelainFields(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.SetOriginHead(t, main, "main")
	feature := testutil.AddWorktree(t, main, "feature")
	testutil.WriteFile(t, filepath.Join(feature, "one.txt"), "one\n")
	testutil.Git(t, feature, "add", "one.txt")
	testutil.Git(t, feature, "commit", "-m", "one")
	testutil.WriteFile(t, filepath.Join(feature, "README.md"), "edited\n")
	t.Chdir(main)

	stdout, _ := runHolt(t, "ls", "--porcelain")

	if strings.Contains(stdout, "BRANCH") {
		t.Error("the porcelain output carries the human header")
	}
	record := rowFor(t, stdout, "feature")
	want := []string{"feature", "1", "0", "dirty", feature}
	if got := strings.Split(record, "\t"); !slices.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestListPorcelainLeavesDriftEmptyWhenGitCannotCompare(t *testing.T) {
	main := testutil.NewRepo(t)
	// No origin, so there is nothing to compare against and no answer to give.
	t.Chdir(main)

	stdout, _ := runHolt(t, "ls", "--porcelain")

	// Zeroes here would read as "exactly in sync" to whatever is parsing this,
	// which is the one thing holt cannot tell it.
	record := rowFor(t, stdout, "main")
	want := []string{"main", "", "", "", main}
	if got := strings.Split(record, "\t"); !slices.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestListEmptyDriftNote(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.AddWorktree(t, main, "feature")
	t.Chdir(main)

	stdout, stderr := runHolt(t, "ls")

	if !strings.Contains(stderr, "no ahead/behind numbers") {
		t.Errorf("stderr is %q, want it to explain the empty columns", stderr)
	}
	if strings.Contains(stdout, "no ahead/behind") {
		t.Errorf("the note landed on stdout:\n%s", stdout)
	}
}

func TestListDriftNoteWhenGitWillNotCompare(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.SetOriginHead(t, main, "main")
	testutil.AddWorktree(t, main, "feature")
	// The ref is here and this git can compare, so git is refusing the comparison
	// itself, which holt cannot name and must not report as a missing ref.
	tree := testutil.Git(t, main, "rev-parse", "HEAD^{tree}")
	testutil.Git(t, main, "update-ref", "refs/remotes/origin/main", tree)
	t.Chdir(main)

	_, stderr := runHolt(t, "ls")

	if !strings.Contains(stderr, "no ahead/behind numbers") {
		t.Errorf("stderr is %q, want it to explain the empty columns", stderr)
	}
	// The branch holt resolved through has to be in the note, since that is the
	// one thing holt knows about a failure it cannot otherwise explain.
	if !strings.Contains(stderr, "origin/main") {
		t.Errorf("stderr %q does not name the branch the comparison was against", stderr)
	}
}

func TestListQuietWithDrift(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.SetOriginHead(t, main, "main")
	testutil.AddWorktree(t, main, "feature")
	t.Chdir(main)

	_, stderr := runHolt(t, "ls")

	if stderr != "" {
		t.Fatalf("stderr is %q, want nothing when the numbers are there", stderr)
	}
}

func TestWorktreeLabels(t *testing.T) {
	tests := []struct {
		name   string
		status worktree.Status
		branch string
		drift  string
		state  string
	}{
		{
			name:   "branch ahead and behind",
			status: worktree.Status{Worktree: worktree.Worktree{Branch: "feature"}, Ahead: 3, Behind: 12, Compared: true},
			branch: "feature", drift: "+3 / -12", state: "",
		},
		{
			name:   "branch level with the default",
			status: worktree.Status{Worktree: worktree.Worktree{Branch: "feature"}, Compared: true},
			branch: "feature", drift: "0 / 0", state: "",
		},
		{
			name:   "never compared",
			status: worktree.Status{Worktree: worktree.Worktree{Branch: "feature"}, Ahead: 3, Behind: 1},
			branch: "feature", drift: "", state: "",
		},
		{
			name:   "uncommitted work",
			status: worktree.Status{Worktree: worktree.Worktree{Branch: "feature"}, State: worktree.StateDirty},
			branch: "feature", drift: "", state: "*",
		},
		{
			name:   "directory deleted",
			status: worktree.Status{Worktree: worktree.Worktree{Branch: "feature"}, State: worktree.StateGone},
			branch: "feature", drift: "", state: "gone",
		},
		{
			name:   "directory unreadable",
			status: worktree.Status{Worktree: worktree.Worktree{Branch: "feature"}, State: worktree.StateBroken},
			branch: "feature", drift: "", state: "broken",
		},
		{
			name:   "detached head",
			status: worktree.Status{Worktree: worktree.Worktree{Detached: true}},
			branch: "(detached)", drift: "", state: "",
		},
		{
			// git detaches HEAD during a rebase, and holt reads back the branch name
			// that "holt cd" and --porcelain both answer to.
			name:   "stopped in a rebase",
			status: worktree.Status{Worktree: worktree.Worktree{Branch: "feature", Detached: true, Rebasing: true}},
			branch: "feature", drift: "", state: "",
		},
		{
			// A rebase begun detached: git records "detached HEAD" where the branch would
			// be, and holt keeps the name empty, so the column would come out blank.
			name:   "stopped in a rebase begun detached",
			status: worktree.Status{Worktree: worktree.Worktree{Detached: true, Rebasing: true}},
			branch: "(detached)", drift: "", state: "",
		},
		{
			name:   "bare repository",
			status: worktree.Status{Worktree: worktree.Worktree{Bare: true}},
			branch: "(bare)", drift: "", state: "",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := branchLabel(test.status); got != test.branch {
				t.Errorf("branch: got %q, want %q", got, test.branch)
			}
			if got := driftLabel(test.status); got != test.drift {
				t.Errorf("drift: got %q, want %q", got, test.drift)
			}
			if got := stateLabel(test.status); got != test.state {
				t.Errorf("state: got %q, want %q", got, test.state)
			}
		})
	}
}

func TestShortenHome(t *testing.T) {
	separator := string(filepath.Separator)
	tests := []struct {
		name string
		path string
		home string
		want string
	}{
		{name: "inside home", path: "/Users/max/code/holt", home: "/Users/max", want: "~" + separator + "code/holt"},
		{name: "home itself", path: "/Users/max", home: "/Users/max", want: "~"},
		{name: "outside home", path: "/opt/holt", home: "/Users/max", want: "/opt/holt"},
		{name: "prefix is not a path component", path: "/Users/maxine/code", home: "/Users/max", want: "/Users/maxine/code"},
		{name: "home unknown", path: "/Users/max/code", home: "", want: "/Users/max/code"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := shortenHome(test.path, test.home); got != test.want {
				t.Fatalf("got %q, want %q", got, test.want)
			}
		})
	}
}

func rowFor(t *testing.T, table, branch string) string {
	t.Helper()
	for _, line := range strings.Split(table, "\n") {
		if strings.HasPrefix(line, branch) {
			return line
		}
	}
	t.Fatalf("no row for %q in:\n%s", branch, table)
	return ""
}
