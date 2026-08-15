package cmd

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/MaxSiominDev/holt/internal/testutil"
)

func TestStatusMatchesGit(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.WriteFile(t, filepath.Join(main, "README.md"), "edited\n")
	t.Chdir(main)

	stdout, _ := runHolt(t, "status")

	if want := testutil.Git(t, main, "status"); strings.TrimSpace(stdout) != strings.TrimSpace(want) {
		t.Fatalf("got:\n%s\nwant:\n%s", stdout, want)
	}
}

func TestStatusAlias(t *testing.T) {
	main := testutil.NewRepo(t)
	t.Chdir(main)

	stdout, _ := runHolt(t, "st")

	if !strings.Contains(stdout, "nothing to commit") {
		t.Fatalf("got %q, want the alias to reach git status", stdout)
	}
}

func TestStatusTakesNoFlagsOrArguments(t *testing.T) {
	main := testutil.NewRepo(t)
	t.Chdir(main)

	runHoltExpectingFailure(t, "st", "-s")
	runHoltExpectingFailure(t, "st", "--porcelain")
	runHoltExpectingFailure(t, "st", "README.md")
}
