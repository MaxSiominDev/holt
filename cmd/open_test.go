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

func TestOpenConfiguredForgeOverridesTheHostName(t *testing.T) {
	// A host whose name says one forge while another runs there, as a migration to
	// GitHub Enterprise leaves, which is what the setting exists for.
	main := repoWithOrigin(t, "git@gitlab.corp.example:group/project.git")
	testutil.Git(t, main, "config", forge.KindKey, "github")
	testutil.Git(t, main, "switch", "--quiet", "--create", "feature")
	stubForgeTools(t, "", "")
	t.Chdir(main)

	stdout, _ := runHolt(t, "open", "--print")

	if !strings.Contains(stdout, "/compare/") {
		t.Fatalf("got %q, want the GitHub shape the configuration asked for", stdout)
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

func TestOpenHostFromConfig(t *testing.T) {
	// A Host alias from ~/.ssh/config fetches and pushes but is no address, and only
	// the user knows what it stands for.
	main := repoWithOrigin(t, "git@github.com-work:me/proj.git")
	testutil.Git(t, main, "config", forge.HostKey, "github.com")
	testutil.Git(t, main, "switch", "--quiet", "--create", "feature")
	stubForgeTools(t, "", "")
	t.Chdir(main)

	stdout, _ := runHolt(t, "open", "--print")

	if want := "https://github.com/me/proj/compare/main...feature?expand=1\n"; stdout != want {
		t.Fatalf("got  %q\nwant %q", stdout, want)
	}
}

func TestOpenRejectsUnusableForgeHost(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		// git stores an empty value, and holt would then ask the user to name
		// a forge for a host that is not there.
		{name: "nothing at all", value: ""},
		{name: "only spaces", value: "   "},
		// Put into a URL as it stands, these come back escaped into nonsense.
		{name: "a whole address", value: "https://github.com"},
		{name: "a host and a path", value: "github.com/extra"},
		{name: "a scheme without slashes", value: "https:github.com"},
		{name: "the ssh user as well", value: "git@github.com"},
		{name: "two words", value: "my github.com"},
		{name: "only a port", value: ":8443"},
		{name: "a host with the port left off", value: "github.com:"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			main := repoWithOrigin(t, "git@github.com:me/proj.git")
			testutil.Git(t, main, "config", forge.HostKey, test.value)
			testutil.Git(t, main, "switch", "--quiet", "--create", "feature")
			t.Chdir(main)

			err := runHoltExpectingFailure(t, "open", "--print")

			if !strings.Contains(err.Error(), forge.HostKey) {
				t.Errorf("error %q names another setting than the one at fault", err)
			}
		})
	}
}

func TestOpenAllowsForgeHostWithPort(t *testing.T) {
	// A self-hosted forge can answer on a port, and that goes into the URL.
	main := repoWithOrigin(t, "git@gitlab.internal:group/proj.git")
	testutil.Git(t, main, "config", forge.HostKey, "gitlab.internal:8443")
	testutil.Git(t, main, "switch", "--quiet", "--create", "feature")
	stubForgeTools(t, "", "")
	t.Chdir(main)

	stdout, _ := runHolt(t, "open", "--print")

	if !strings.HasPrefix(stdout, "https://gitlab.internal:8443/group/proj/-/merge_requests/new") {
		t.Fatalf("got %q, want the port carried into the URL", stdout)
	}
}
