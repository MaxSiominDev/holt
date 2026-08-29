package shell

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ZDOTDIR", "")

	tests := []struct {
		name    string
		shell   string
		zdotdir string
		want    string
		wantErr bool
	}{
		{name: "zsh", shell: "zsh", want: filepath.Join(home, ".zshrc")},
		{name: "bash", shell: "bash", want: filepath.Join(home, ".bashrc")},
		{
			// With ZDOTDIR set, writing to ~/.zshrc would have no effect.
			name: "zsh with ZDOTDIR set", shell: "zsh", zdotdir: "/somewhere/else",
			want: filepath.Join("/somewhere/else", ".zshrc"),
		},
		{name: "a shell holt has no function for", shell: "fish", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("ZDOTDIR", test.zdotdir)

			got, err := ConfigFile(test.shell)

			if test.wantErr {
				if !errors.Is(err, ErrUnsupported) {
					t.Fatalf("got %v, want ErrUnsupported", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("got %q, want %q", got, test.want)
			}
		})
	}
}

func TestInstallWritesEvalLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".zshrc")

	changed, err := Install("zsh", path)
	if err != nil {
		t.Fatal(err)
	}

	if !changed {
		t.Fatal("a startup file with nothing in it was left unchanged")
	}
	content := read(t, path)
	// The line keeps the function coming from the binary; a copy would freeze it.
	if !strings.Contains(content, `eval "$(holt shell-init zsh)"`) {
		t.Errorf("the file does not load holt:\n%s", content)
	}
	if strings.Contains(content, "command holt") {
		t.Errorf("the function body was copied in instead of evaluated:\n%s", content)
	}
}

func TestInstallGuardsMissingHolt(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".zshrc")
	if _, err := Install("zsh", path); err != nil {
		t.Fatal(err)
	}

	// Uninstall holt and every new shell would say "command not found: holt".
	if !strings.Contains(read(t, path), "command -v holt") {
		t.Errorf("the line runs unconditionally:\n%s", read(t, path))
	}
}

func TestSnippetSilentWithoutHolt(t *testing.T) {
	forEachInstalledShell(t, func(t *testing.T, name, rc string) {
		// An empty PATH is the same as an uninstalled holt.
		command := exec.Command(name, "-c", "source "+rc+"; echo done")
		command.Env = []string{"PATH=", "HOME=" + filepath.Dir(rc)}

		out, err := command.CombinedOutput()

		if err != nil {
			t.Fatalf("sourcing the startup file failed: %v\n%s", err, out)
		}
		if !strings.Contains(string(out), "done") {
			t.Fatalf("the startup file did not run through:\n%s", out)
		}
		if strings.Contains(string(out), "not found") {
			t.Errorf("a missing holt made noise:\n%s", out)
		}
	})
}

func TestSnippetSurvivesSetU(t *testing.T) {
	forEachInstalledShell(t, func(t *testing.T, name, rc string) {
		// A bare "holt" reads as an argument the function was not given, and under
		// set -u "$1" aborts the shell instead of printing holt's help.
		command := exec.Command(name, "-c", "set -u; source "+rc+"; holt; echo done")
		command.Env = []string{"PATH=", "HOME=" + filepath.Dir(rc)}

		out, err := command.CombinedOutput()

		if err != nil {
			t.Fatalf("the bare command failed under set -u: %v\n%s", err, out)
		}
		if !strings.Contains(string(out), "done") {
			t.Fatalf("the shell stopped at the unset argument:\n%s", out)
		}
	})
}

func forEachInstalledShell(t *testing.T, run func(t *testing.T, name, rc string)) {
	t.Helper()
	for _, name := range Supported {
		t.Run(name, func(t *testing.T) {
			if _, err := exec.LookPath(name); err != nil {
				t.Skipf("%s is not installed", name)
			}
			rc := filepath.Join(t.TempDir(), "rc")
			if _, err := Install(name, rc); err != nil {
				t.Fatal(err)
			}
			run(t, name, rc)
		})
	}
}

func TestInstallKeepsOtherLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".zshrc")
	write(t, path, "export PATH=\"$HOME/.local/bin:$PATH\"\neval \"$(jenv init -)\"\n")

	if _, err := Install("zsh", path); err != nil {
		t.Fatal(err)
	}

	content := read(t, path)
	for _, line := range []string{`export PATH="$HOME/.local/bin:$PATH"`, `eval "$(jenv init -)"`} {
		if !strings.Contains(content, line) {
			t.Errorf("the file lost %q:\n%s", line, content)
		}
	}
}

func TestInstallTwice(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".zshrc")
	if _, err := Install("zsh", path); err != nil {
		t.Fatal(err)
	}
	first := read(t, path)

	changed, err := Install("zsh", path)
	if err != nil {
		t.Fatal(err)
	}

	if changed {
		t.Error("a second install reported a change")
	}
	if read(t, path) != first {
		t.Error("a second install rewrote the file")
	}
	if strings.Count(read(t, path), blockBegin) != 1 {
		t.Error("the block was added twice")
	}
}

func TestInstalledDetectsLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".zshrc")

	before, err := Installed(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Install("zsh", path); err != nil {
		t.Fatal(err)
	}
	after, err := Installed(path)
	if err != nil {
		t.Fatal(err)
	}

	if before {
		t.Error("a file that does not exist was reported as loading holt")
	}
	if !after {
		t.Error("the file was not recognised after the line was added")
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestQuoteAndNamedSurviveAnEmbeddedQuote(t *testing.T) {
	// A refname may hold a quote: git refuses a space but takes this.
	command := Named("git branch --set-upstream-to=" + Quote("origin/we&ird'q"))

	// Through %q the backslash is escaped again and the line stops being one
	// command, which is what these two exist to avoid.
	if want := `"git branch --set-upstream-to='origin/we&ird'\''q'"`; command != want {
		t.Fatalf("got %s, want %s", command, want)
	}
}
