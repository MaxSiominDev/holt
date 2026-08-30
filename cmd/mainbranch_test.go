package cmd

import (
	"strings"
	"testing"

	"github.com/MaxSiominDev/holt/internal/testutil"
)

func TestMainSwitchesDefaultBranch(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.SetOriginHead(t, main, "main")
	testutil.Git(t, main, "switch", "--quiet", "--create", "feature")
	t.Chdir(main)

	runHolt(t, "main")

	if branch := testutil.Git(t, main, "rev-parse", "--abbrev-ref", "HEAD"); branch != "main" {
		t.Fatalf("the checkout is on %q, want main", branch)
	}
}

func TestMainRefusesInWorktree(t *testing.T) {
	repo := testutil.NewRepo(t)
	testutil.SetOriginHead(t, repo, "main")
	feature := testutil.AddWorktree(t, repo, "feature")
	t.Chdir(feature)

	err := runHoltExpectingFailure(t, "main")

	if !strings.Contains(err.Error(), "holt home") {
		t.Errorf("error %q does not say how to reach the main checkout", err)
	}
}

func TestMainTakesNoArguments(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.SetOriginHead(t, main, "main")
	t.Chdir(main)

	_ = runHoltExpectingFailure(t, "main", "master")
}
