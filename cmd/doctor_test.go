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

	// Row by row: the words alone turn up in details and in repository paths.
	for _, label := range []string{
		"main checkout", "worktrees", "default branch", "ahead/behind columns",
		"shell function", "hook directory", "post-checkout hook", "merged paths",
		"mirrored paths", "info/exclude", "symlinks",
	} {
		wantCheck(t, out, label, statusOK, "")
	}
}

func TestDoctorBareMainCheckout(t *testing.T) {
	bare := testutil.NewBareRepo(t)
	t.Chdir(bare)

	out, _ := runHolt(t, "doctor")

	wantCheck(t, out, "main checkout", statusWarn, "no files to mirror from")
}

func TestDoctorBareMainCheckoutWithAMirrorList(t *testing.T) {
	bare := testutil.NewBareRepo(t)
	// A list an older holt wrote: mirroring is refused here now, so the rows reading
	// the main checkout would answer about the repository directory.
	testutil.WriteFile(t, filepath.Join(bare, "holt", "mirror.list"), "CLAUDE.local.md\n")
	t.Chdir(bare)

	out, _ := runHolt(t, "doctor")

	wantCheck(t, out, "mirrored paths", statusWarn, "nothing here to mirror from")
	if strings.Contains(out, "symlinks") {
		t.Errorf("doctor reports on symlinks it cannot have made:\n%s", out)
	}
}

func TestDoctorHooksPathInWorkTree(t *testing.T) {
	main := testutil.NewRepo(t)
	// What a hook manager leaves behind: a directory tracked with the project.
	testutil.Git(t, main, "config", "core.hooksPath", ".husky")
	t.Chdir(main)

	root, out, _ := holtCommand(t, "doctor")
	err := root.Execute()

	if err == nil {
		t.Fatalf("doctor exited zero with a failing check:\n%s", out.String())
	}
	if !strings.Contains(err.Error(), "1 check failed") {
		t.Errorf("error %q does not say how many checks failed", err)
	}
	wantCheck(t, out.String(), "hook directory", statusFail, "inside the working tree")
}

func TestDoctorBareRepositoryDoesNotAdviseTheHook(t *testing.T) {
	bare := testutil.NewBareRepo(t)
	t.Chdir(bare)

	out, _ := runHolt(t, "doctor")

	// A bare repository has no files to mirror from, and "holt mirror hook" refuses
	// there, so naming it would send the user to a command that cannot work.
	wantCheck(t, out, "post-checkout hook", statusWarn, "does not install it in a bare repository")
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

func TestDoctorMovedWorktreeNamesBothSteps(t *testing.T) {
	main := testutil.CloneOf(t, testutil.NewRepo(t), "project")
	root := filepath.Dir(main)
	testutil.WriteFile(t, filepath.Join(main, "CLAUDE.local.md"), "notes\n")
	t.Chdir(main)
	runHolt(t, "mirror", "add", "CLAUDE.local.md")
	runHolt(t, "new", "feature")
	// The project directory moved, by a rename or a restore: git records the old
	// path while the worktree sits under the new one.
	moved := filepath.Join(root, "moved")
	if err := os.Mkdir(moved, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"project", "project-worktrees"} {
		if err := os.Rename(filepath.Join(root, name), filepath.Join(moved, name)); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(filepath.Join(moved, "project"))

	out, _ := runHolt(t, "doctor")

	// A repair alone leaves holt's symlinks naming the old main checkout, which only
	// the sync puts right.
	wantCheck(t, out, "worktrees", statusWarn, "git worktree repair")
	if !strings.Contains(out, `"holt mirror sync"`) {
		t.Errorf("doctor names the repair and stops there:\n%s", out)
	}
}

func TestDoctorExcludeBlockBehindTheList(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.WriteFile(t, filepath.Join(main, "CLAUDE.local.md"), "notes\n")
	testutil.WriteFile(t, filepath.Join(main, ".envrc"), "export A=1\n")
	t.Chdir(main)
	runHolt(t, "mirror", "add", "CLAUDE.local.md")
	// A hand-edited list, or two holt commands writing at once: the marker is still
	// there, so a check looking for it alone calls this healthy.
	list := filepath.Join(main, ".git", "holt", "mirror.list")
	content, err := os.ReadFile(list)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(list, append(content, []byte(".envrc\n")...), 0o644); err != nil {
		t.Fatal(err)
	}

	out, _ := runHolt(t, "doctor")

	// The block no longer covers what is mirrored, so .envrc's symlink shows as untracked,
	// the one thing the block is there to stop.
	wantCheck(t, out, "info/exclude", statusWarn, "does not cover")
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
			detail: "a symlink points elsewhere",
		},
		{
			name: "a real file in its place",
			disturb: func(t *testing.T, link string) {
				removeFile(t, link)
				testutil.WriteFile(t, link, "the worktree's own copy\n")
			},
			status: statusWarn,
			detail: "something holt did not put there is in the way",
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

func TestDoctorNamesLinksLeftByADeletedSource(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.WriteFile(t, filepath.Join(main, "CLAUDE.local.md"), "notes\n")
	testutil.AddWorktree(t, main, "feature")
	t.Chdir(main)
	runHolt(t, "mirror", "add", "CLAUDE.local.md")
	if err := os.Remove(filepath.Join(main, "CLAUDE.local.md")); err != nil {
		t.Fatal(err)
	}

	out, _ := runHolt(t, "doctor")

	// Calling the links complete certifies a set of them that all lead nowhere, and the
	// mirrored paths row says nothing about the worktrees.
	wantCheck(t, out, "symlinks", statusWarn, "no longer has")
}

func TestDoctorKeepsTheFailureWhenAnotherWorktreeIsOnlyMoved(t *testing.T) {
	main := testutil.CloneOf(t, testutil.NewRepo(t), "project")
	root := filepath.Dir(main)
	t.Chdir(main)
	runHolt(t, "new", "feature")
	// One worktree unreadable, a failure, and one moved, a warning: the clauses come
	// in order and the second must not talk the first down.
	broken := testutil.AddWorktree(t, main, "broken")
	if err := os.Remove(filepath.Join(broken, ".git")); err != nil {
		t.Fatal(err)
	}
	moved := filepath.Join(root, "moved")
	if err := os.Mkdir(moved, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"project", "project-worktrees"} {
		if err := os.Rename(filepath.Join(root, name), filepath.Join(moved, name)); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(filepath.Join(moved, "project"))

	root2, out, _ := holtCommand(t, "doctor")
	err := root2.Execute()

	wantCheck(t, out.String(), "worktrees", statusFail, "git cannot read")
	// An unreadable worktree is what doctor exits non-zero for, which a script reads.
	if err == nil {
		t.Errorf("doctor exited zero with a worktree it cannot read:\n%s", out.String())
	}
}

func TestDoctorLeavesAnUnreadableWorktreeOutOfTheSymlinkTotal(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads a directory whatever its mode says")
	}
	main := testutil.NewRepo(t)
	testutil.WriteFile(t, filepath.Join(main, ".claude", "settings.local.json"), "{}\n")
	testutil.AddWorktree(t, main, "one")
	unreadable := testutil.AddWorktree(t, main, "two")
	t.Chdir(main)
	runHolt(t, "mirror", "add", ".claude/settings.local.json")
	if err := os.Chmod(filepath.Join(unreadable, ".claude"), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(unreadable, ".claude"), 0o755) })

	root, buffer, _ := holtCommand(t, "doctor")
	// The unreadable worktree fails a check of its own, so only the rows matter here.
	_ = root.Execute()
	out := buffer.String()

	// Counted in, the row calls the links complete in a worktree said to be unreadable.
	wantCheck(t, out, "symlinks", statusOK, "complete in 1 worktree")
	wantCheck(t, out, "unreadable worktrees", statusFail, unreadable)
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

	wantCheck(t, out, "worktrees", statusWarn, "git worktree prune")
	// Said this way rather than "no linked worktree", since the row above has counted one.
	wantCheck(t, out, "symlinks", statusOK, "no worktree left to look in")
}

func TestDoctorStaleWorktreeWithoutMirroredPaths(t *testing.T) {
	main := testutil.NewRepo(t)
	feature := testutil.AddWorktree(t, main, "feature")
	t.Chdir(main)
	if err := os.RemoveAll(feature); err != nil {
		t.Fatal(err)
	}

	out, _ := runHolt(t, "doctor")

	// The row has to survive doctor's early return when nothing is mirrored, which is
	// every repository not set up yet.
	wantCheck(t, out, "worktrees", statusWarn, "git worktree prune")
}

func TestDoctorNamesEveryKindOfDamageAtOnce(t *testing.T) {
	main := testutil.NewRepo(t)
	gone := testutil.AddWorktree(t, main, "gone")
	broken := testutil.AddWorktree(t, main, "broken")
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}
	testutil.WriteFile(t, filepath.Join(broken, ".git"), "gitdir: /nowhere/at/all\n")
	t.Chdir(main)

	root, out, _ := holtCommand(t, "doctor")
	if err := root.Execute(); err == nil {
		t.Fatalf("doctor exited zero with a worktree git cannot read:\n%s", out.String())
	}

	// The worst standing for the rest loses the other's remedy, while "holt ls"
	// shows both.
	wantCheck(t, out.String(), "worktrees", statusFail, "git worktree repair")
	if !strings.Contains(out.String(), "git worktree prune") {
		t.Errorf("the report drops the worktree whose directory is gone:\n%s", out.String())
	}
}

func TestDoctorKeepsTheHookDirectoryRowWhenTheHookIsUnreadable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads a file whatever its mode says")
	}
	main := testutil.NewRepo(t)
	t.Chdir(main)
	runHolt(t, "mirror", "hook")
	// The directory row is sound whatever the file in it turned out to be.
	hook := filepath.Join(main, ".git", "hooks", "post-checkout")
	if err := os.Chmod(hook, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(hook, 0o755) })

	root, out, _ := holtCommand(t, "doctor")
	if err := root.Execute(); err == nil {
		t.Fatalf("doctor exited zero with an unreadable hook:\n%s", out.String())
	}

	wantCheck(t, out.String(), "post-checkout hook", statusFail, "permission denied")
	wantCheck(t, out.String(), "hook directory", statusOK, filepath.Join(main, ".git", "hooks"))
}

func TestDoctorKeepsReportingPastAnUnreadableFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads a file whatever its mode says")
	}
	main := testutil.NewRepo(t)
	testutil.WriteFile(t, filepath.Join(main, "CLAUDE.local.md"), "notes\n")
	testutil.AddWorktree(t, main, "feature")
	t.Chdir(main)
	runHolt(t, "mirror", "add", "CLAUDE.local.md")

	// The rows below have nothing to do with this file, and doctor is run once
	// things are already broken.
	exclude := filepath.Join(main, ".git", "info", "exclude")
	if err := os.Chmod(exclude, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(exclude, 0o644) })

	root, out, _ := holtCommand(t, "doctor")
	if err := root.Execute(); err == nil {
		t.Fatalf("doctor exited zero with an unreadable file:\n%s", out.String())
	}

	wantCheck(t, out.String(), "info/exclude", statusFail, "permission denied")
	wantCheck(t, out.String(), "symlinks", statusOK, "complete in 1 worktree")
}

func TestDoctorNamesEverySymlinkStateAtOnce(t *testing.T) {
	main := testutil.NewRepo(t)
	testutil.WriteFile(t, filepath.Join(main, "CLAUDE.local.md"), "notes\n")
	absent := testutil.AddWorktree(t, main, "absent")
	blocked := testutil.AddWorktree(t, main, "blocked")
	diverted := testutil.AddWorktree(t, main, "diverted")
	t.Chdir(main)
	runHolt(t, "mirror", "add", "CLAUDE.local.md")

	if err := os.Remove(filepath.Join(absent, "CLAUDE.local.md")); err != nil {
		t.Fatal(err)
	}
	for _, worktree := range []string{blocked, diverted} {
		if err := os.Remove(filepath.Join(worktree, "CLAUDE.local.md")); err != nil {
			t.Fatal(err)
		}
	}
	testutil.WriteFile(t, filepath.Join(blocked, "CLAUDE.local.md"), "written by hand\n")
	elsewhere := filepath.Join(t.TempDir(), "notes-of-my-own.md")
	testutil.WriteFile(t, elsewhere, "written by hand\n")
	if err := os.Symlink(elsewhere, filepath.Join(diverted, "CLAUDE.local.md")); err != nil {
		t.Fatal(err)
	}

	out, _ := runHolt(t, "doctor")

	// A diverted link is permanent by design, so the first state standing for the
	// rest hides the blocked path for good.
	for _, want := range []string{"missing in 1 of 3", "points elsewhere in 1 of 3", "in the way in 1 of 3"} {
		if !strings.Contains(out, want) {
			t.Errorf("the symlinks row does not say %q:\n%s", want, out)
		}
	}
}

func TestDoctorLockedWorktreeWithoutItsDirectory(t *testing.T) {
	main := testutil.NewRepo(t)
	feature := testutil.AddWorktree(t, main, "feature")
	// What lock exists for, a worktree on a volume that comes and goes: prune spares
	// it, so advising prune never clears the warning.
	testutil.Git(t, main, "worktree", "lock", "--reason", "on another disk", feature)
	if err := os.RemoveAll(feature); err != nil {
		t.Fatal(err)
	}
	t.Chdir(main)

	out, _ := runHolt(t, "doctor")

	// Said out loud though prune cannot help, since "holt ls" shows it as gone.
	wantCheck(t, out, "worktrees", statusOK, "locked with the directory not there")
	if strings.Contains(out, "git worktree prune") {
		t.Errorf("output %q prescribes a prune that leaves a locked worktree alone", out)
	}
}

func TestDoctorBrokenWorktree(t *testing.T) {
	main := testutil.NewRepo(t)
	feature := testutil.AddWorktree(t, main, "feature")
	t.Chdir(main)
	// The directory is there and unreadable, which renaming the main checkout leaves
	// behind in every worktree at once.
	testutil.WriteFile(t, filepath.Join(feature, ".git"), "gitdir: /nowhere/at/all\n")

	root, out, _ := holtCommand(t, "doctor")
	err := root.Execute()

	wantCheck(t, out.String(), "worktrees", statusFail, "git worktree repair")
	if err == nil {
		t.Errorf("doctor reported a worktree git cannot read and still exited zero:\n%s", out.String())
	}
}

func TestShellFunctionCheck(t *testing.T) {
	tests := []struct {
		name     string
		shell    string // $SHELL, defaulting to zsh
		exported bool   // this shell carries the marker the snippet exports
		inConfig bool   // the startup file loads the snippet
		status   checkStatus
		detail   string
	}{
		{name: "loaded", exported: true, status: statusOK, detail: "loaded"},
		{name: "in the startup file only", inConfig: true, status: statusWarn, detail: "open a new one"},
		{name: "nowhere", status: statusWarn, detail: "holt shell-init"},
		// Naming a command would send this user to write ~/.zshrc, which the next
		// breath turns them away from.
		{name: "a shell holt has no function for", shell: "/usr/local/bin/fish", status: statusWarn, detail: "holt has one for bash and zsh"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			// ZDOTDIR would send ConfigFile to the developer's own .zshrc.
			t.Setenv("ZDOTDIR", "")
			if test.shell == "" {
				test.shell = "/bin/zsh"
			}
			t.Setenv("SHELL", test.shell)
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

func TestShellFromEnv(t *testing.T) {
	tests := []struct {
		name  string
		env   string
		want  string
		known bool
	}{
		{name: "bash", env: "/bin/bash", want: "bash", known: true},
		{name: "zsh", env: "/opt/homebrew/bin/zsh", want: "zsh", known: true},
		{name: "a shell holt has no function for", env: "/usr/bin/fish"},
		{name: "unset", env: ""},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("SHELL", test.env)

			got, known := shellFromEnv()

			if got != test.want || known != test.known {
				t.Fatalf("got %q, %v, want %q, %v", got, known, test.want, test.known)
			}
		})
	}
}

// A fragment rather than the whole line, so rewording a message does not fail.
func TestDoctorMergeList(t *testing.T) {
	main := testutil.NewRepo(t)
	writeMergeList(t, "CHANGELOG.md\ndocs/*.md\nnotes.txt\n")
	t.Chdir(main)

	out, _ := runHolt(t, "doctor")

	wantCheck(t, out, "merged paths", statusOK, "CHANGELOG.md, docs/*.md")
	// The rejected line names itself and the file to fix it in.
	wantCheck(t, out, "merge list", statusWarn, "line 3")
}

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

func TestDoctorKeepsReportingWhenHookPathIsAFile(t *testing.T) {
	main := testutil.NewRepo(t)
	// A hooks directory that is not one: holt cannot look inside, and what it already
	// worked out is still what the user came for.
	inTheWay := filepath.Join(t.TempDir(), "hooksfile")
	testutil.WriteFile(t, inTheWay, "not a directory\n")
	testutil.Git(t, main, "config", "core.hooksPath", inTheWay)
	t.Chdir(main)

	root, stdout, _ := holtCommand(t, "doctor")
	err := root.Execute()

	if err == nil {
		t.Error("a hooks directory holt cannot look into left doctor reporting success")
	}
	wantCheck(t, stdout.String(), "main checkout", statusOK, main)
	wantCheck(t, stdout.String(), "post-checkout hook", statusFail, "not a directory")
}
