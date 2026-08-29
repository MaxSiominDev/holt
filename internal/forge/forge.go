// Package forge turns a git remote into the web addresses of its hosting service.
package forge

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os/exec"
	"path"
	"strings"
	"time"

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

// HostKey names the host the web UI answers on, for a remote that does not, an ssh
// Host alias being the ordinary reason. Asking ssh answers a different question.
const HostKey = "holt.forgeHost"

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
	configuredHost, err := forgeHost(repo)
	if err != nil {
		return "", err
	}
	if configuredHost != "" {
		remote.host = configuredHost
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

// Or a tool waiting on a dead network hangs holt open. A var so the test need not wait.
var lookupTimeout = 10 * time.Second

// Empty means no open request, or a forge tool absent or unable to answer.
func existingRequestURL(remote remoteRepo, kind forgeKind, branch string, notes io.Writer) string {
	name, args := lookupCommand(kind, remote, branch)
	if _, err := exec.LookPath(name); err != nil {
		return "" // ordinary: the address holt builds needs no CLI
	}

	ctx, cancel := context.WithTimeout(context.Background(), lookupTimeout)
	defer cancel()
	command := exec.CommandContext(ctx, name, args...)
	// Killing the tool alone is not enough: a child holding the pipe would keep
	// the read waiting.
	command.WaitDelay = time.Second
	command.Env = append(git.EnvWithoutRedirects(), "LC_ALL=C")

	var stderr bytes.Buffer
	command.Stderr = &stderr
	out, err := command.Output()
	if err != nil {
		// Installed and still no answer, usually an expired token; falling back
		// quietly would read as the request having gone.
		reason := firstLine(stderr.String())
		switch {
		case ctx.Err() != nil:
			reason = "it did not answer in " + lookupTimeout.String()
		case reason == "":
			// The line names a cause, which a silent failure leaves hanging. Only its own exit
			// status: a failure to start speaks Go and carries the binary's full path.
			var exit *exec.ExitError
			if errors.As(err, &exit) {
				reason = err.Error()
			} else {
				reason = "holt could not run it"
			}
		}
		fmt.Fprintf(notes, "holt: %s could not be asked about an open request: %s\n", name, reason)
		return ""
	}
	return browsableURL(string(out))
}

// --jq extracts, so holt never parses a document glab may write a raw newline into,
// which Go's decoder refuses. The repository carries its host, or glab asks gitlab.com.
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

// browsableURL takes an https address and nothing else, so a usage error, a table or a
// self-hosted forge served over plain http falls back to the page holt builds, which
// costs whoever runs one the link to an existing request and no more.
func browsableURL(out string) string {
	line := firstLine(out)
	if !strings.HasPrefix(line, "https://") {
		return ""
	}
	return line
}

func firstLine(text string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	return strings.TrimSpace(line)
}

// A host and nothing else, since a scheme or path would come back escaped into
// nonsense; read through a URL, so a port and an IPv6 literal still pass.
func forgeHost(repo *git.Repo) (string, error) {
	configured, set, err := repo.Config(HostKey)
	if err != nil || !set {
		return "", err
	}
	host := strings.TrimSpace(configured)
	if host == "" {
		return "", fmt.Errorf("%s is set to nothing; unset it or give it a host name", HostKey)
	}
	// Host keeps the port, so a bare port round-trips through it while Hostname
	// comes back empty; a trailing colon is a port the user left off.
	parsed, err := url.Parse("https://" + host)
	if err != nil || parsed.Host != host || parsed.User != nil ||
		parsed.Hostname() == "" || strings.HasSuffix(host, ":") {
		return "", fmt.Errorf("%s is set to %q; it takes a host name on its own, without a scheme, a path or a user", HostKey, configured)
	}
	return host, nil
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
		return "", fmt.Errorf("cannot tell what runs %s, set it with %q", host, "git config "+KindKey+" <github|gitlab>")
	}
	return kind, nil
}

type remoteRepo struct {
	host string // gitlab.example.com
	path string // group/platform/v9/stream-connector
}

// The path is kept whole rather than split, since GitLab nests groups deep.
func parseRemote(remote string) (remoteRepo, error) {
	// Trailing slashes come off first, or "repo.git/" keeps its suffix.
	trimmed := strings.TrimSuffix(strings.TrimRight(strings.TrimSpace(remote), "/"), ".git")

	// In "git@host:path" the colon is not a port, so url.Parse misreads it.
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

// Host names ignore case, and so does whoever types holt.forgeHost.
func detectKind(host string) (forgeKind, bool) {
	lowered := strings.ToLower(host)
	switch {
	case strings.Contains(lowered, "github"):
		return gitHub, true
	case strings.Contains(lowered, "gitlab"):
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
