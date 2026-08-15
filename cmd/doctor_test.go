package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MaxSiominDev/holt/internal/shell"
	"github.com/MaxSiominDev/holt/internal/testutil"
)

func TestDoctorHealthyRepository(t *testing.T) {
	main := testutil.NewRepo(t)
	// The marker the snippet exports; doctor warns without it.
	t.Setenv(shell.EnvVar, "1")
	testutil.WriteFile(t, filepath.Join(main, "CLAUDE.local.md"), "notes\n")
	testutil.SetOriginHead(t, main, "main")
	testutil.AddWorktree(t, main, "feature")
	t.Chdir(main)
	runHolt(t, "mirror", "add", "CLAUDE.local.md")

	out, _ := runHolt(t, "doctor")

	if strings.Contains(out, "warn") || strings.Contains(out, "fail") {
		t.Fatalf("doctor reported a problem in a fully set up repository:\n%s", out)
	}
}

func TestDoctorBareMainCheckout(t *testing.T) {
	bare := testutil.NewBareRepo(t)
	t.Chdir(bare)

	out, _ := runHolt(t, "doctor")

	wantCheck(t, out, "main checkout", statusWarn, "no files to mirror from")
}

func TestDoctorHooksPathInWorkTree(t *testing.T) {
	main := testutil.NewRepo(t)
	// What a hook manager leaves behind: a directory tracked with the project.
	testutil.Git(t, main, "config", "core.hooksPath", ".husky")
	t.Chdir(main)

	root, out, _ := holtCommand([]string{"doctor"})
	err := root.Execute()

	if err == nil {
		t.Fatalf("doctor exited zero with a failing check:\n%s", out.String())
	}
	if !strings.Contains(err.Error(), "1 check failed") {
		t.Errorf("error %q does not say how many checks failed", err)
	}
	wantCheck(t, out.String(), "hook directory", statusFail, "inside the working tree")
}

func TestDoctorHookStates(t *testing.T) {
	tests := []struct {
		name    string
		install func(t *testing.T, main string)
		status  checkStatus
		detail  string
	}{
		{
			name:   "nothing installed",
			status: statusWarn,
			detail: "not installed",
		},
		{
			name:    "holt's own",
			install: func(t *testing.T, _ string) { runHolt(t, "mirror", "hook") },
			status:  statusOK,
			detail:  filepath.Join(".git", "hooks", "post-checkout"),
		},
		{
			name: "someone else's",
			install: func(t *testing.T, main string) {
				testutil.WriteFile(t, filepath.Join(main, ".git", "hooks", "post-checkout"),
					"#!/bin/sh\necho someone else\n")
			},
			status: statusWarn,
			detail: "--replace",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			main := testutil.NewRepo(t)
			t.Chdir(main)
			if test.install != nil {
				test.install(t, main)
			}

			out, _ := runHolt(t, "doctor")

			wantCheck(t, out, "post-checkout hook", test.status, test.detail)
		})
	}
}

func TestDoctorDefaultBranch(t *testing.T) {
	tests := []struct {
		name   string
		setUp  func(t *testing.T, main string)
		status checkStatus
		detail string
	}{
		{
			name:   "no origin",
			status: statusWarn,
			detail: "no origin remote",
		},
		{
			name: "origin without a head",
			setUp: func(t *testing.T, main string) {
				testutil.Git(t, main, "remote", "add", "origin", filepath.Join(main, "..", "origin.git"))
			},
			status: statusWarn,
			detail: "git remote set-head origin --auto",
		},
		{
			name:   "resolved",
			setUp:  func(t *testing.T, main string) { testutil.SetOriginHead(t, main, "main") },
			status: statusOK,
			detail: "main",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			main := testutil.NewRepo(t)
			t.Chdir(main)
			if test.setUp != nil {
				test.setUp(t, main)
			}

			out, _ := runHolt(t, "doctor")

			wantCheck(t, out, "default branch", test.status, test.detail)
		})
	}
}

func TestDoctorMirroredPaths(t *testing.T) {
	tests := []struct {
		name   string
		setUp  func(t *testing.T, main string)
		status checkStatus
		detail string
	}{
		{
			name:   "nothing mirrored",
			status: statusWarn,
			detail: "holt mirror add",
		},
		{
			name:   "all present",
			setUp:  func(t *testing.T, _ string) { runHolt(t, "mirror", "add", "CLAUDE.local.md") },
			status: statusOK,
			detail: "all present",
		},
		{
			name: "the file is gone from the main checkout",
			setUp: func(t *testing.T, main string) {
				runHolt(t, "mirror", "add", "CLAUDE.local.md")
				removeFile(t, filepath.Join(main, "CLAUDE.local.md"))
			},
			status: statusWarn,
			detail: "not found in the main checkout",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			main := testutil.NewRepo(t)
			testutil.WriteFile(t, filepath.Join(main, "CLAUDE.local.md"), "notes\n")
			t.Chdir(main)
			if test.setUp != nil {
				test.setUp(t, main)
			}

			out, _ := runHolt(t, "doctor")

			wantCheck(t, out, "mirrored paths", test.status, test.detail)
		})
	}
}

func TestDoctorExcludeBlockGone(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.WriteFile(t, filepath.Join(main, "CLAUDE.local.md"), "notes\n")
	t.Chdir(main)
	runHolt(t, "mirror", "add", "CLAUDE.local.md")
	if err := os.WriteFile(filepath.Join(main, ".git", "info", "exclude"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	out, _ := runHolt(t, "doctor")

	wantCheck(t, out, "info/exclude", statusWarn, "missing")
}

func TestDoctorSymlinkStates(t *testing.T) {
	tests := []struct {
		name    string
		disturb func(t *testing.T, link string)
		status  checkStatus
		detail  string
	}{
		{
			name:   "in place",
			status: statusOK,
			detail: "complete in 1 worktree",
		},
		{
			name:    "deleted",
			disturb: removeFile,
			status:  statusWarn,
			detail:  "missing in 1 of 1 worktree",
		},
		{
			name: "aimed elsewhere",
			disturb: func(t *testing.T, link string) {
				removeFile(t, link)
				if err := os.Symlink(filepath.Join(t.TempDir(), "notes.md"), link); err != nil {
					t.Fatal(err)
				}
			},
			status: statusWarn,
			detail: "pointing elsewhere",
		},
		{
			name: "a real file in its place",
			disturb: func(t *testing.T, link string) {
				removeFile(t, link)
				testutil.WriteFile(t, link, "the worktree's own copy\n")
			},
			status: statusWarn,
			detail: "a real file is in the way",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			main := testutil.NewRepo(t)
			testutil.WriteFile(t, filepath.Join(main, "CLAUDE.local.md"), "notes\n")
			feature := testutil.AddWorktree(t, main, "feature")
			t.Chdir(main)
			runHolt(t, "mirror", "add", "CLAUDE.local.md")
			if test.disturb != nil {
				test.disturb(t, filepath.Join(feature, "CLAUDE.local.md"))
			}

			out, _ := runHolt(t, "doctor")

			wantCheck(t, out, "symlinks", test.status, test.detail)
		})
	}
}

func TestDoctorStaleWorktree(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.WriteFile(t, filepath.Join(main, "CLAUDE.local.md"), "notes\n")
	feature := testutil.AddWorktree(t, main, "feature")
	t.Chdir(main)
	runHolt(t, "mirror", "add", "CLAUDE.local.md")
	if err := os.RemoveAll(feature); err != nil {
		t.Fatal(err)
	}

	out, _ := runHolt(t, "doctor")

	wantCheck(t, out, "stale worktrees", statusWarn, "git worktree prune")
}

func TestShellFunctionCheck(t *testing.T) {
	tests := []struct {
		name     string
		exported bool // this shell carries the marker the snippet exports
		inConfig bool // the startup file loads the snippet
		status   checkStatus
		detail   string
	}{
		{name: "loaded", exported: true, status: statusOK, detail: "loaded"},
		{name: "in the startup file only", inConfig: true, status: statusWarn, detail: "open a new one"},
		{name: "nowhere", status: statusWarn, detail: "holt shell-init"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			// ZDOTDIR would send ConfigFile to the developer's own .zshrc.
			t.Setenv("ZDOTDIR", "")
			t.Setenv("SHELL", "/bin/zsh")
			t.Setenv(shell.EnvVar, "")
			if test.exported {
				t.Setenv(shell.EnvVar, "1")
			}
			if test.inConfig {
				if _, err := shell.Install("zsh", filepath.Join(home, ".zshrc")); err != nil {
					t.Fatal(err)
				}
			}

			got := shellFunctionCheck()

			if got.status != test.status {
				t.Errorf("got status %s, want %s (%s)", got.status, test.status, got.detail)
			}
			if !strings.Contains(got.detail, test.detail) {
				t.Errorf("detail %q does not mention %q", got.detail, test.detail)
			}
		})
	}
}

func TestCurrentShellFallback(t *testing.T) {
	tests := []struct {
		name string
		env  string
		want string
	}{
		{name: "bash", env: "/bin/bash", want: "bash"},
		{name: "zsh", env: "/opt/homebrew/bin/zsh", want: "zsh"},
		{name: "a shell holt has no function for", env: "/usr/bin/fish", want: "zsh"},
		{name: "unset", env: "", want: "zsh"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("SHELL", test.env)

			if got := currentShell(); got != test.want {
				t.Fatalf("got %q, want %q", got, test.want)
			}
		})
	}
}

// A fragment rather than the whole line, so rewording a message does not fail.
func wantCheck(t *testing.T, out, label string, status checkStatus, fragment string) {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		// tabwriter pads with two spaces or more, and no label holds a run of two.
		got, rest, _ := strings.Cut(strings.TrimSpace(line), "  ")
		name, detail, _ := strings.Cut(strings.TrimSpace(rest), "  ")
		if name != label {
			continue
		}
		detail = strings.TrimSpace(detail)
		if got != string(status) {
			t.Errorf("%s: got %s, want %s (%s)", label, got, status, detail)
		}
		if !strings.Contains(detail, fragment) {
			t.Errorf("%s: detail %q does not mention %q", label, detail, fragment)
		}
		return
	}
	t.Fatalf("no %q row in:\n%s", label, out)
}

func removeFile(t *testing.T, path string) {
	t.Helper()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
}
