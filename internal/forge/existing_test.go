package forge

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var testRemote = remoteRepo{host: "github.com", path: "MaxSiominDev/holt"}

func TestExistingRequestURL(t *testing.T) {
	stubGh(t, "echo https://github.com/MaxSiominDev/holt/pull/7", 0)

	var notes strings.Builder
	got := existingRequestURL(testRemote, gitHub, "feature", &notes)

	if want := "https://github.com/MaxSiominDev/holt/pull/7"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if notes.String() != "" {
		t.Errorf("notes are %q, want nothing said on the ordinary path", notes.String())
	}
}

func TestExistingRequestURLNone(t *testing.T) {
	// gh prints nothing and succeeds when there is no pull request.
	stubGh(t, "", 0)

	var notes strings.Builder
	got := existingRequestURL(testRemote, gitHub, "feature", &notes)

	if got != "" {
		t.Fatalf("got %q, want nothing", got)
	}
	if notes.String() != "" {
		t.Errorf("notes are %q, want silence when there is no pull request", notes.String())
	}
}

func TestExistingRequestURLGhFails(t *testing.T) {
	// Exit 4 is gh with no authentication.
	stubGh(t, "echo 'To get started with GitHub CLI, please run:  gh auth login' >&2", 4)

	var notes strings.Builder
	got := existingRequestURL(testRemote, gitHub, "feature", &notes)

	if got != "" {
		t.Fatalf("got %q, want nothing", got)
	}
	if !strings.Contains(notes.String(), "gh auth login") {
		t.Errorf("notes are %q, want gh's own reason repeated", notes.String())
	}
}

func TestExistingRequestURLNoGh(t *testing.T) {
	// An empty PATH stands in for gh never being installed.
	t.Setenv("PATH", t.TempDir())

	var notes strings.Builder
	got := existingRequestURL(testRemote, gitHub, "feature", &notes)

	if got != "" {
		t.Fatalf("got %q, want nothing", got)
	}
	if notes.String() != "" {
		t.Errorf("notes are %q, want silence when gh was never there", notes.String())
	}
}

func TestExistingRequestURLArgs(t *testing.T) {
	log := filepath.Join(t.TempDir(), "argv")
	stubGh(t, "echo \"$@\" > "+log, 0)

	existingRequestURL(remoteRepo{host: "github.example.com", path: "group/nested/project"}, gitHub, "feat/x", &strings.Builder{})

	recorded, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	// The host travels with the path, and the whole nested path survives.
	for _, expected := range []string{"github.example.com/group/nested/project", "--head feat/x", "--state open"} {
		if !strings.Contains(string(recorded), expected) {
			t.Errorf("gh was called with %q, missing %q", strings.TrimSpace(string(recorded)), expected)
		}
	}
}

var testGitLabRemote = remoteRepo{host: "gitlab.example.com", path: "group/platform/v9/stream-connector"}

func TestExistingRequestURLGitLab(t *testing.T) {
	// glab does the extracting, so holt sees a bare address.
	stubGlab(t, "echo https://gitlab.example.com/group/platform/v9/stream-connector/-/merge_requests/12", 0)

	var notes strings.Builder
	got := existingRequestURL(testGitLabRemote, gitLab, "PROJ-1-fix", &notes)

	if want := "https://gitlab.example.com/group/platform/v9/stream-connector/-/merge_requests/12"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if notes.String() != "" {
		t.Errorf("notes are %q, want nothing said on the ordinary path", notes.String())
	}
}

func TestExistingRequestURLGitLabNone(t *testing.T) {
	// glab prints nothing and succeeds when there is no merge request.
	stubGlab(t, "", 0)

	var notes strings.Builder
	got := existingRequestURL(testGitLabRemote, gitLab, "PROJ-1-fix", &notes)

	if got != "" {
		t.Fatalf("got %q, want nothing", got)
	}
	if notes.String() != "" {
		t.Errorf("notes are %q, want silence when there is no merge request", notes.String())
	}
}

func TestExistingRequestURLGlabFails(t *testing.T) {
	stubGlab(t, "echo 'authentication required, run glab auth login' >&2", 1)

	var notes strings.Builder
	got := existingRequestURL(testGitLabRemote, gitLab, "PROJ-1-fix", &notes)

	if got != "" {
		t.Fatalf("got %q, want nothing", got)
	}
	if !strings.Contains(notes.String(), "glab auth login") {
		t.Errorf("notes are %q, want glab's own reason repeated", notes.String())
	}
}

func TestExistingRequestURLGitLabArgs(t *testing.T) {
	log := filepath.Join(t.TempDir(), "argv")
	stubGlab(t, "echo \"$@\" > "+log, 0)

	existingRequestURL(testGitLabRemote, gitLab, "feat/x", &strings.Builder{})

	recorded, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	// Without the host, glab asks gitlab.com.
	for _, expected := range []string{"gitlab.example.com/group/platform/v9/stream-connector", "--source-branch feat/x", "--jq .[].web_url"} {
		if !strings.Contains(string(recorded), expected) {
			t.Errorf("glab was called with %q, missing %q", strings.TrimSpace(string(recorded)), expected)
		}
	}
}

func TestExistingRequestURLTakesTheFirstOfSeveral(t *testing.T) {
	// GitLab allows several open requests from one branch, and glab prints one
	// address per line, which handed on whole is no address at all.
	stubGlab(t, `printf '%s\n%s\n' https://x/y/-/merge_requests/1 https://x/y/-/merge_requests/2`, 0)

	got := existingRequestURL(testGitLabRemote, gitLab, "feature", &strings.Builder{})

	if want := "https://x/y/-/merge_requests/1"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestExistingRequestURLNotAnAddress(t *testing.T) {
	tests := []struct {
		name string
		kind forgeKind
		body string
	}{
		{name: "gh printed a usage error to stdout", kind: gitHub, body: `echo "unknown flag: --head"`},
		{name: "glab printed a table instead of json", kind: gitLab, body: `echo "12  PROJ-1-fix  open"`},
		{name: "glab printed the json rather than the address", kind: gitLab, body: `echo '[{"web_url":"https://x/y/-/merge_requests/1"}]'`},
		{name: "the address is not https", kind: gitHub, body: `echo "file:///etc/passwd"`},
		// A self-hosted forge served over plain http. holt hands what it finds
		// to a browser, so the scheme has to be the one it asked for.
		{name: "the address is plain http", kind: gitLab, body: `echo "http://gitlab.internal/g/p/-/merge_requests/12"`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stubCommands(t, test.body, 0, ghCommand, glabCommand)

			// An answer holt cannot read means the page holt builds itself.
			got := existingRequestURL(testGitLabRemote, test.kind, "feature", &strings.Builder{})

			if got != "" {
				t.Fatalf("got %q, want it refused", got)
			}
		})
	}
}

func TestExistingRequestURLHangs(t *testing.T) {
	// Long enough that a test which does wait would time out rather than pass.
	stubGh(t, "sleep 30", 0)
	previous := lookupTimeout
	lookupTimeout = 150 * time.Millisecond
	t.Cleanup(func() { lookupTimeout = previous })

	var notes strings.Builder
	start := time.Now()
	got := existingRequestURL(testRemote, gitHub, "feature", &notes)

	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("waited %s, want the lookup given up on", elapsed)
	}
	if got != "" {
		t.Fatalf("got %q, want nothing", got)
	}
	if !strings.Contains(notes.String(), "did not answer") {
		t.Errorf("notes are %q, want the timeout named", notes.String())
	}
}

// stubGh stands in for the GitHub CLI.
func stubGh(t *testing.T, body string, status int) {
	t.Helper()
	stubCommands(t, body, status, ghCommand)
}

// stubGlab stands in for the GitLab CLI.
func stubGlab(t *testing.T, body string, status int) {
	t.Helper()
	stubCommands(t, body, status, glabCommand)
}

// A script for each name, first on PATH, running body and exiting with status.
func stubCommands(t *testing.T, body string, status int, names ...string) {
	t.Helper()
	dir := t.TempDir()
	script := fmt.Sprintf("#!/bin/sh\n%s\nexit %d\n", body, status)
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestExistingRequestURLSilentFailure(t *testing.T) {
	// A tool that fails without a word on stderr. Left as it was, the note
	// ended on its colon and named no cause at all.
	stubGh(t, "", 3)

	var notes strings.Builder
	existingRequestURL(testRemote, gitHub, "feature", &notes)

	if strings.HasSuffix(strings.TrimRight(notes.String(), "\n"), ": ") {
		t.Fatalf("notes are %q, which trails off", notes.String())
	}
	if !strings.Contains(notes.String(), "3") {
		t.Errorf("notes are %q, want the exit status gh went out with", notes.String())
	}
}

func TestExistingRequestURLUnrunnableTool(t *testing.T) {
	// On PATH and executable but unstartable, where Go says "no such file or
	// directory" about a file holt just found and names its whole path.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ghCommand), []byte("#!/nonexistent/interpreter\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	var notes strings.Builder
	existingRequestURL(testRemote, gitHub, "feature", &notes)

	if !strings.Contains(notes.String(), "holt could not run it") {
		t.Fatalf("notes are %q, want holt's own words for a tool it could not start", notes.String())
	}
	if strings.Contains(notes.String(), "fork/exec") {
		t.Fatalf("notes are %q, which hands the user Go's own wording", notes.String())
	}
	if strings.Contains(notes.String(), dir) {
		t.Errorf("notes are %q, which spells out where the binary lives", notes.String())
	}
}
