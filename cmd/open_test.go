package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MaxSiominDev/holt/internal/forge"
	"github.com/MaxSiominDev/holt/internal/testutil"
)

func TestOpenGitLabNewMergeRequest(t *testing.T) {
	main := repoWithOrigin(t, "git@gitlab.example.com:group/platform/v9/stream-connector.git")
	testutil.Git(t, main, "switch", "--quiet", "--create", "PROJ-1-fix")
	stubForgeTools(t, "", "")
	t.Chdir(main)

	stdout, _ := runHolt(t, "open", "--print")

	// Taking the last two segments would give v9/stream-connector, not a repository.
	want := "https://gitlab.example.com/group/platform/v9/stream-connector/-/merge_requests/new" +
		"?merge_request%5Bsource_branch%5D=PROJ-1-fix\n"
	if stdout != want {
		t.Fatalf("got  %q\nwant %q", stdout, want)
	}
}

func TestOpenExistingMergeRequest(t *testing.T) {
	main := repoWithOrigin(t, "git@gitlab.example.com:group/project.git")
	testutil.Git(t, main, "switch", "--quiet", "--create", "PROJ-1-fix")
	stubForgeTools(t, "", "echo https://gitlab.example.com/group/project/-/merge_requests/12")
	t.Chdir(main)

	stdout, _ := runHolt(t, "open", "--print")

	if want := "https://gitlab.example.com/group/project/-/merge_requests/12\n"; stdout != want {
		t.Fatalf("got %q, want the open merge request %q", stdout, want)
	}
}

func TestOpenOnDefaultBranch(t *testing.T) {
	main := repoWithOrigin(t, "git@gitlab.com:group/project.git")
	t.Chdir(main)

	err := runHoltExpectingFailure(t, "open", "--print")

	if !strings.Contains(err.Error(), "main") {
		t.Errorf("error %q does not name the branch", err)
	}
}

func TestOpenUnknownHost(t *testing.T) {
	main := repoWithOrigin(t, "git@git.example.com:group/project.git")
	testutil.Git(t, main, "switch", "--quiet", "--create", "feature")
	t.Chdir(main)

	err := runHoltExpectingFailure(t, "open", "--print")

	if !strings.Contains(err.Error(), forge.KindKey) {
		t.Errorf("error %q does not say how to tell holt what runs there", err)
	}
}

func TestOpenConfiguredForge(t *testing.T) {
	main := repoWithOrigin(t, "git@git.example.com:group/project.git")
	testutil.Git(t, main, "config", forge.KindKey, "gitlab")
	testutil.Git(t, main, "switch", "--quiet", "--create", "feature")
	stubForgeTools(t, "", "")
	t.Chdir(main)

	stdout, _ := runHolt(t, "open", "--print")

	if !strings.Contains(stdout, "/-/merge_requests/new") {
		t.Fatalf("got %q, want the GitLab shape the configuration asked for", stdout)
	}
}

func TestOpenExistingPullRequest(t *testing.T) {
	main := repoWithOrigin(t, "https://github.com/MaxSiominDev/holt.git")
	testutil.Git(t, main, "switch", "--quiet", "--create", "add-open")
	stubForgeTools(t, "echo https://github.com/MaxSiominDev/holt/pull/7", "")
	t.Chdir(main)

	stdout, _ := runHolt(t, "open", "--print")

	if want := "https://github.com/MaxSiominDev/holt/pull/7\n"; stdout != want {
		t.Fatalf("got %q, want the open pull request %q", stdout, want)
	}
}

func TestOpenNoPullRequest(t *testing.T) {
	main := repoWithOrigin(t, "https://github.com/MaxSiominDev/holt.git")
	testutil.Git(t, main, "switch", "--quiet", "--create", "add-open")
	// Stubbed: the real answer would depend on what is open on GitHub today.
	stubForgeTools(t, "", "")
	t.Chdir(main)

	stdout, _ := runHolt(t, "open", "--print")

	if want := "https://github.com/MaxSiominDev/holt/compare/main...add-open?expand=1\n"; stdout != want {
		t.Fatalf("got %q, want %q", stdout, want)
	}
}

func TestOpenAsksOnlyItsOwnForge(t *testing.T) {
	main := repoWithOrigin(t, "git@gitlab.com:group/project.git")
	testutil.Git(t, main, "switch", "--quiet", "--create", "feature")
	// gh would answer if asked, and its answer belongs to another forge.
	stubForgeTools(t, "echo https://github.com/wrong/place/pull/1", "")
	t.Chdir(main)

	stdout, _ := runHolt(t, "open", "--print")

	if !strings.Contains(stdout, "/-/merge_requests/new") {
		t.Fatalf("got %q, want the GitLab page holt builds itself", stdout)
	}
}

func TestOpenUnknownForgeKind(t *testing.T) {
	main := repoWithOrigin(t, "git@git.example.com:group/project.git")
	testutil.Git(t, main, "config", forge.KindKey, "bitbucket")
	testutil.Git(t, main, "switch", "--quiet", "--create", "feature")
	t.Chdir(main)

	err := runHoltExpectingFailure(t, "open", "--print")

	if !strings.Contains(err.Error(), "bitbucket") {
		t.Errorf("error %q does not name the value it cannot use", err)
	}
}

// Both tools, so no test reaches a real one. Separate bodies let one answer
// while the other stays silent.
func stubForgeTools(t *testing.T, gh, glab string) {
	t.Helper()
	dir := t.TempDir()
	for name, body := range map[string]string{"gh": gh, "glab": glab} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func repoWithOrigin(t *testing.T, url string) string {
	t.Helper()
	main := testutil.NewRepo(t)
	testutil.Git(t, main, "remote", "add", "origin", url)
	testutil.SetOriginHead(t, main, "main")
	return main
}
