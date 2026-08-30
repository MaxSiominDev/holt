package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/MaxSiominDev/holt/internal/testutil"
)

func TestNewPrintsOnlyPath(t *testing.T) {
	clone, _ := testutil.NewClonedRepo(t)
	t.Chdir(clone)

	stdout, stderr := runHolt(t, "new", "feature")

	want := filepath.Join(filepath.Dir(clone), filepath.Base(clone)+"-worktrees", "feature")
	if stdout != want+"\n" {
		t.Fatalf("stdout is %q, want exactly the path %q", stdout, want)
	}
	if stderr == "" {
		t.Error("git's progress was swallowed")
	}
}

func TestNewNoFetchSkipsOrigin(t *testing.T) {
	clone, origin := testutil.NewClonedRepo(t)
	if err := os.RemoveAll(origin); err != nil {
		t.Fatal(err)
	}
	t.Chdir(clone)

	stdout, _ := runHolt(t, "new", "--no-fetch", "feature")

	want := filepath.Join(filepath.Dir(clone), filepath.Base(clone)+"-worktrees", "feature")
	if stdout != want+"\n" {
		t.Fatalf("stdout is %q, want exactly the path %q", stdout, want)
	}
}

func TestNewReachesOriginByDefault(t *testing.T) {
	clone, origin := testutil.NewClonedRepo(t)
	if err := os.RemoveAll(origin); err != nil {
		t.Fatal(err)
	}
	t.Chdir(clone)

	// Without --no-fetch the origin is reached, so the test above pins something down.
	_ = runHoltExpectingFailure(t, "new", "feature")
}
