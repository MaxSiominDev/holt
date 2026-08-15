package cmd

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MaxSiominDev/holt/internal/shell"
)

// The one part of holt that is never compiled, so it is run for real here.

func TestShellFunctionEntersPath(t *testing.T) {
	forEachShell(t, func(t *testing.T, name string) {
		destination, stubDir := stubHolt(t)

		out := runShell(t, name, stubDir, shell.Snippet+"\nholt cd whatever\npwd\n")

		if got := lastLine(out); got != destination {
			t.Fatalf("the shell ended up in %q, want %q", got, destination)
		}
	})
}

func TestShellFunctionForwardsHelp(t *testing.T) {
	forEachShell(t, func(t *testing.T, name string) {
		destination, stubDir := stubHolt(t)
		start := resolvedTempDir(t)

		script := shell.Snippet + "\ncd " + start + "\nholt cd --help\npwd\n"
		out := runShell(t, name, stubDir, script)

		// Without the guard the help text would be captured and handed to cd.
		if got := lastLine(out); got != start {
			t.Fatalf("the shell moved to %q, want to stay in %q (destination was %q)", got, start, destination)
		}
	})
}

func TestShellFunctionHoltFails(t *testing.T) {
	forEachShell(t, func(t *testing.T, name string) {
		// Prints a path and still fails: without "|| return" cd would happen anyway.
		destination, stubDir := stubHoltExiting(t, 1)
		start := resolvedTempDir(t)

		script := shell.Snippet + "\ncd " + start + "\nholt cd whatever\necho \"status=$?\"\npwd\n"
		out := runShell(t, name, stubDir, script)

		if got := lastLine(out); got != start {
			t.Errorf("the shell moved to %q after a failing holt, want to stay in %q (stub printed %q)", got, start, destination)
		}
		if !strings.Contains(out, "status=1") {
			t.Errorf("the function reported success after holt failed:\n%s", out)
		}
	})
}

func TestShellFunctionStrictMode(t *testing.T) {
	forEachShell(t, func(t *testing.T, name string) {
		destination, stubDir := stubHolt(t)
		start := resolvedTempDir(t)

		// set -u trips over a bare "holt"; a non-default IFS used to hide --help.
		script := "set -u\nIFS=,\n" + shell.Snippet + "\ncd " + start + "\nholt\nholt cd --help >/dev/null\npwd\n"
		out := runShell(t, name, stubDir, script)

		if got := lastLine(out); got != start {
			t.Fatalf("the shell moved to %q, want to stay in %q (destination was %q)", got, start, destination)
		}
	})
}

func TestShellFunctionOtherCommands(t *testing.T) {
	forEachShell(t, func(t *testing.T, name string) {
		_, stubDir := stubHolt(t)
		start := resolvedTempDir(t)

		script := shell.Snippet + "\ncd " + start + "\nholt doctor\npwd\n"
		out := runShell(t, name, stubDir, script)

		if got := lastLine(out); got != start {
			t.Fatalf("a command that is not cd or home moved the shell to %q", got)
		}
	})
}

func TestShellInitExportsMarker(t *testing.T) {
	out, _ := runHolt(t, "shell-init", "zsh")

	if !strings.Contains(out, "export "+shell.EnvVar+"=1") {
		t.Fatalf("the snippet does not export %s, so doctor cannot tell it was loaded:\n%s", shell.EnvVar, out)
	}
}

func TestShellInitInstall(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ZDOTDIR", "")

	stdout, _ := runHolt(t, "shell-init", "zsh", "--install")

	rc := filepath.Join(home, ".zshrc")
	content, err := os.ReadFile(rc)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), `eval "$(holt shell-init zsh)"`) {
		t.Errorf("%s does not load holt:\n%s", rc, content)
	}
	// A shell reading a different startup file is the one silent failure here.
	if !strings.Contains(stdout, rc) {
		t.Errorf("stdout %q does not name the file it wrote", stdout)
	}
}

func TestShellInitPrintsOnly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	runHolt(t, "shell-init", "zsh")

	if _, err := os.Stat(filepath.Join(home, ".zshrc")); !os.IsNotExist(err) {
		t.Fatal("printing the snippet wrote to the startup file")
	}
}

func TestShellInitUnsupportedShell(t *testing.T) {
	root := newRootCommand("test")
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"shell-init", "fish"})

	err := root.Execute()

	if err == nil {
		t.Fatal("a shell holt has no function for was accepted")
	}
	if !strings.Contains(err.Error(), "fish") {
		t.Errorf("error %q does not name the shell", err)
	}
}

func forEachShell(t *testing.T, run func(t *testing.T, name string)) {
	t.Helper()
	for _, name := range shell.Supported {
		t.Run(name, func(t *testing.T) {
			if _, err := exec.LookPath(name); err != nil {
				t.Skipf("%s is not installed", name)
			}
			run(t, name)
		})
	}
}

// A binary on PATH that prints one directory, the way "holt cd" does.
func stubHolt(t *testing.T) (destination, binDir string) {
	t.Helper()
	return stubHoltExiting(t, 0)
}

func stubHoltExiting(t *testing.T, status int) (destination, binDir string) {
	t.Helper()
	destination = resolvedTempDir(t)
	binDir = t.TempDir()
	script := fmt.Sprintf("#!/bin/sh\necho '%s'\nexit %d\n", destination, status)
	if err := os.WriteFile(filepath.Join(binDir, "holt"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return destination, binDir
}

func runShell(t *testing.T, name, binDir, script string) string {
	t.Helper()
	command := exec.Command(name, "-c", script)
	command.Env = append(os.Environ(), "PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	var stderr bytes.Buffer
	command.Stderr = &stderr
	out, err := command.Output()
	if err != nil {
		t.Fatalf("%s failed: %v\nscript:\n%s\nstderr:\n%s", name, err, script, stderr.String())
	}
	return string(out)
}

// resolvedTempDir holds no symlinks, so pwd prints what the test compares against.
func resolvedTempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func lastLine(out string) string {
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	return lines[len(lines)-1]
}
