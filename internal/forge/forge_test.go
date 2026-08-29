package forge

import (
	"strings"
	"testing"
)

func TestParseRemote(t *testing.T) {
	tests := []struct {
		name    string
		remote  string
		want    remoteRepo
		wantErr bool
	}{
		{
			name:   "the scp-like form with nested groups",
			remote: "git@gitlab.example.com:group/platform/v9/stream-connector.git",
			want:   remoteRepo{host: "gitlab.example.com", path: "group/platform/v9/stream-connector"},
		},
		{
			name:   "https",
			remote: "https://github.com/MaxSiominDev/holt.git",
			want:   remoteRepo{host: "github.com", path: "MaxSiominDev/holt"},
		},
		{
			name:   "https carrying a user name",
			remote: "https://max@github.com/MaxSiominDev/holt.git",
			want:   remoteRepo{host: "github.com", path: "MaxSiominDev/holt"},
		},
		{
			name:   "ssh url on a non-standard port",
			remote: "ssh://git@gitlab.example.com:2222/group/platform/v9/stream-connector.git",
			// The port belongs to ssh; the web interface is not on it.
			want: remoteRepo{host: "gitlab.example.com", path: "group/platform/v9/stream-connector"},
		},
		{
			name:   "without the .git suffix",
			remote: "git@github.com:MaxSiominDev/holt",
			want:   remoteRepo{host: "github.com", path: "MaxSiominDev/holt"},
		},
		{
			name:   "a trailing slash after the suffix",
			remote: "https://github.com/MaxSiominDev/holt.git/",
			// Stripping ".git" before the slash would leave it in the path.
			want: remoteRepo{host: "github.com", path: "MaxSiominDev/holt"},
		},
		{
			name:    "a host with no repository",
			remote:  "git@github.com:",
			wantErr: true,
		},
		{
			name:    "not a remote at all",
			remote:  "just-a-word",
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseRemote(test.remote)

			if test.wantErr {
				if err == nil {
					t.Fatalf("got %+v, want an error", got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("got %+v, want %+v", got, test.want)
			}
		})
	}
}

func TestDetectKind(t *testing.T) {
	tests := []struct {
		host       string
		want       forgeKind
		recognised bool
	}{
		{host: "github.com", want: gitHub, recognised: true},
		{host: "gitlab.com", want: gitLab, recognised: true},
		{host: "gitlab.example.com", want: gitLab, recognised: true},
		{host: "github.enterprise.example.com", want: gitHub, recognised: true},
		{host: "git.example.com", recognised: false},
	}

	for _, test := range tests {
		t.Run(test.host, func(t *testing.T) {
			got, recognised := detectKind(test.host)

			if recognised != test.recognised {
				t.Fatalf("got recognised=%v, want %v", recognised, test.recognised)
			}
			if got != test.want {
				t.Fatalf("got %q, want %q", got, test.want)
			}
		})
	}
}

func TestNewChangeRequestURL(t *testing.T) {
	tests := []struct {
		name    string
		remote  remoteRepo
		kind    forgeKind
		branch  string
		want    string
		wantErr bool
	}{
		{
			name:   "gitlab keeps the whole nested path",
			remote: remoteRepo{host: "gitlab.example.com", path: "group/platform/v9/stream-connector"},
			kind:   gitLab, branch: "PROJ-1-fix",
			want: "https://gitlab.example.com/group/platform/v9/stream-connector/-/merge_requests/new" +
				"?merge_request%5Bsource_branch%5D=PROJ-1-fix",
		},
		{
			name:   "a gitlab branch with a slash is escaped in the query",
			remote: remoteRepo{host: "gitlab.com", path: "group/project"},
			kind:   gitLab, branch: "feature/nested",
			want: "https://gitlab.com/group/project/-/merge_requests/new" +
				"?merge_request%5Bsource_branch%5D=feature%2Fnested",
		},
		{
			name:   "github compares the default branch against this one",
			remote: remoteRepo{host: "github.com", path: "MaxSiominDev/holt"},
			kind:   gitHub, branch: "add-open",
			want: "https://github.com/MaxSiominDev/holt/compare/main...add-open?expand=1",
		},
		{
			name:   "a github branch with a slash stays a path",
			remote: remoteRepo{host: "github.com", path: "MaxSiominDev/holt"},
			kind:   gitHub, branch: "feature/nested",
			want: "https://github.com/MaxSiominDev/holt/compare/main...feature/nested?expand=1",
		},
		{
			name:   "a gitlab branch with characters a URL reads specially",
			remote: remoteRepo{host: "gitlab.com", path: "group/project"},
			kind:   gitLab, branch: "fix #42 100% done",
			want: "https://gitlab.com/group/project/-/merge_requests/new" +
				"?merge_request%5Bsource_branch%5D=fix+%2342+100%25+done",
		},
		{
			name:   "a github branch with characters a URL reads specially",
			remote: remoteRepo{host: "github.com", path: "MaxSiominDev/holt"},
			kind:   gitHub, branch: "fix #42 100% done",
			// A raw "#" would cut the address short at the fragment.
			want: "https://github.com/MaxSiominDev/holt/compare/main...fix%20%2342%20100%25%20done?expand=1",
		},
		{
			name:   "a forge holt builds no addresses for",
			remote: remoteRepo{host: "bitbucket.example.com", path: "group/project"},
			kind:   "bitbucket", branch: "PROJ-1-fix",
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := newChangeRequestURL(test.remote, test.kind, test.branch, "main")

			if test.wantErr {
				if err == nil {
					t.Fatalf("got %s, want an error", got)
				}
				if !strings.Contains(err.Error(), string(test.kind)) {
					t.Fatalf("the error is %q, want the forge it could not build for named", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("got  %s\nwant %s", got, test.want)
			}
		})
	}
}

func TestDetectKindIgnoresCase(t *testing.T) {
	// Spelled out rather than lower-cased and matched here, which is the rule
	// under test and would agree with itself however wrong it was.
	for host, want := range map[string]forgeKind{
		"GitHub.COM":         gitHub,
		"github.com":         gitHub,
		"GITLAB.example.com": gitLab,
		"gitlab.example.com": gitLab,
	} {
		kind, recognised := detectKind(host)
		if !recognised {
			t.Errorf("%s was not recognised", host)
			continue
		}
		if kind != want {
			t.Errorf("%s was read as %s, want %s", host, kind, want)
		}
	}
}
