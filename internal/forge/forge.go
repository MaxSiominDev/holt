// Package forge turns a git remote into the web addresses of its hosting service.
package forge

import (
	"bytes"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path"
	"strings"

	"github.com/MaxSiominDev/holt/internal/git"
)

// forgeKind is the software running the forge, which decides its URL shapes.
type forgeKind string

const (
	gitHub forgeKind = "github"
	gitLab forgeKind = "gitlab"
)

// KindKey names the forge when the host name does not.
const KindKey = "holt.forge"

// ChangeRequestURL returns the request already raised from branch where that
// can be told, otherwise the page to raise one.
func ChangeRequestURL(repo *git.Repo, branch, defaultBranch string, notes io.Writer) (string, error) {
	origin, err := repo.Output("remote", "get-url", "origin")
	if err != nil {
		return "", err
	}
	remote, err := parseRemote(origin)
	if err != nil {
		return "", err
	}
	kind, err := kindOf(repo, remote.host)
	if err != nil {
		return "", err
	}

	if existing := existingRequestURL(remote, kind, branch, notes); existing != "" {
		return existing, nil
	}
	return newChangeRequestURL(remote, kind, branch, defaultBranch)
}

// The forge command line tools, which hold the authentication holt does not.
const (
	ghCommand   = "gh"
	glabCommand = "glab"
)

// An empty string means there is no open request, or that the forge's own tool
// is absent or could not answer.
func existingRequestURL(remote remoteRepo, kind forgeKind, branch string, notes io.Writer) string {
	name, args := lookupCommand(kind, remote, branch)
	if _, err := exec.LookPath(name); err != nil {
		return "" // ordinary: the address holt builds needs no CLI
	}

	command := exec.Command(name, args...)
	command.Env = append(os.Environ(), "LC_ALL=C")

	var stderr bytes.Buffer
	command.Stderr = &stderr
	out, err := command.Output()
	if err != nil {
		// Installed and still no answer, usually an expired token. Falling back
		// quietly would read as the request having gone.
		fmt.Fprintf(notes, "holt: %s could not be asked about an open request: %s\n",
			name, firstLine(stderr.String()))
		return ""
	}
	return browsableURL(string(out))
}

// Both tools are asked for the bare address rather than JSON: glab passes the
// merge request description through unescaped, and one holding a newline makes
// the whole document invalid. The host travels with the repository, or glab
// asks gitlab.com and answers 404 for a company instance.
func lookupCommand(kind forgeKind, remote remoteRepo, branch string) (string, []string) {
	repository := remote.host + "/" + remote.path
	if kind == gitLab {
		return glabCommand, []string{"mr", "list",
			"--repo", repository,
			"--source-branch", branch,
			"--output", "json",
			"--jq", ".[].web_url"}
	}
	return ghCommand, []string{"pr", "list",
		"--repo", repository,
		"--head", branch,
		"--state", "open",
		"--json", "url",
		"--jq", ".[].url"}
}

// browsableURL accepts only what can be handed to a browser, so a tool that
// answered something else falls back to the page holt builds itself.
func browsableURL(out string) string {
	line := firstLine(out)
	if !strings.HasPrefix(line, "https://") {
		return ""
	}
	return line
}

func firstLine(text string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	return line
}

func kindOf(repo *git.Repo, host string) (forgeKind, error) {
	// Read first, so the setting also overrides a misleading host name.
	configured, set, err := repo.Config(KindKey)
	if err != nil {
		return "", err
	}
	if set {
		kind := forgeKind(configured)
		switch kind {
		case gitHub, gitLab:
			return kind, nil
		default:
			return "", fmt.Errorf("%s is set to %q, which is neither %q nor %q", KindKey, configured, gitHub, gitLab)
		}
	}

	kind, recognised := detectKind(host)
	if !recognised {
		// Guessing would open a plausible address that goes nowhere.
		return "", fmt.Errorf("cannot tell what runs %s, set it with %q", host, "git config "+KindKey+" gitlab")
	}
	return kind, nil
}

type remoteRepo struct {
	host string // gitlab.example.com
	path string // group/platform/v9/stream-connector
}

// The path is kept whole rather than split into owner and repository, since
// GitLab nests groups several deep.
func parseRemote(remote string) (remoteRepo, error) {
	// Trailing slashes come off first: git accepts "repo.git/" verbatim, and
	// stripping ".git" before them would leave it in the path.
	trimmed := strings.TrimSuffix(strings.TrimRight(strings.TrimSpace(remote), "/"), ".git")

	// In "git@host:path" the colon separates host from path rather than
	// introducing a port, so url.Parse misreads it.
	if !strings.Contains(trimmed, "://") {
		host, repoPath, found := strings.Cut(trimmed, ":")
		if !found {
			return remoteRepo{}, fmt.Errorf("cannot tell a host from a path in the remote %q", remote)
		}
		if _, afterUser, hasUser := strings.Cut(host, "@"); hasUser {
			host = afterUser
		}
		return newRemote(host, repoPath, remote)
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return remoteRepo{}, fmt.Errorf("parsing the remote %q: %w", remote, err)
	}
	// Hostname drops any port: ssh may live on 2222, the web UI does not.
	return newRemote(parsed.Hostname(), parsed.Path, remote)
}

func detectKind(host string) (forgeKind, bool) {
	switch {
	case strings.Contains(host, "github"):
		return gitHub, true
	case strings.Contains(host, "gitlab"):
		return gitLab, true
	}
	return "", false
}

// Both forges open a request from a URL alone, so holt needs no API access.
func newChangeRequestURL(remote remoteRepo, kind forgeKind, branch, defaultBranch string) (string, error) {
	target := url.URL{Scheme: "https", Host: remote.host}
	switch kind {
	case gitHub:
		target.Path = path.Join("/", remote.path, "compare", defaultBranch+"..."+branch)
		target.RawQuery = url.Values{"expand": {"1"}}.Encode()
	case gitLab:
		// The "-/" keeps GitLab from reading a nested group path as a route.
		target.Path = path.Join("/", remote.path, "-", "merge_requests", "new")
		target.RawQuery = url.Values{"merge_request[source_branch]": {branch}}.Encode()
	default:
		return "", fmt.Errorf("the forge is %q, which is neither %q nor %q", kind, gitHub, gitLab)
	}
	return target.String(), nil
}

func newRemote(host, repoPath, original string) (remoteRepo, error) {
	repoPath = strings.Trim(repoPath, "/")
	if host == "" || repoPath == "" {
		return remoteRepo{}, fmt.Errorf("the remote %q names no repository", original)
	}
	return remoteRepo{host: host, path: repoPath}, nil
}
