package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestVersionFlag(t *testing.T) {
	root, out, _ := rootWithCapturedOutput(t, "--version")

	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}

	if got, want := out.String(), "holt 1.2.3\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestCommandGroups(t *testing.T) {
	root := newRootCommand("test")

	for _, command := range root.Commands() {
		// cobra owns these two and files them under "Additional Commands".
		if command.Name() == "help" || command.Name() == "completion" {
			continue
		}
		if command.GroupID == "" {
			t.Errorf("%q has no group, so the help files it under Additional Commands", command.Name())
		}
	}
}

func TestHelpContents(t *testing.T) {
	root, out, _ := holtCommand([]string{"--help"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}

	help := out.String()
	for _, title := range []string{
		"Moving between worktrees:",
		"Working on the branch you are in:",
		"Setting holt up:",
	} {
		if !strings.Contains(help, title) {
			t.Errorf("the help has no %q section:\n%s", title, help)
		}
	}
	// Without it "holt cd" prints a path and nothing moves.
	if !strings.Contains(help, "holt shell-init") {
		t.Error("the help never says how to install the shell function")
	}
	// Spelled out: read back from the command tree, both sides would drop
	// together and assert nothing.
	for _, name := range []string{
		"cd", "doctor", "home", "ls", "main", "mirror", "new",
		"open", "pull", "push", "rebase", "rm", "shell-init", "status",
	} {
		if !strings.Contains(help, name) {
			t.Errorf("the help does not list %q:\n%s", name, help)
		}
	}
}

func TestVersionHasNoShorthand(t *testing.T) {
	root, _, errOut := rootWithCapturedOutput(t, "-v")

	if err := root.Execute(); err == nil {
		t.Fatalf("-v was accepted, output was %q", errOut)
	}
}

func TestUnknownArgument(t *testing.T) {
	root, _, errOut := rootWithCapturedOutput(t, "bogus")

	err := root.Execute()

	if err == nil {
		t.Fatal("an unknown argument was accepted")
	}
	if !strings.Contains(errOut.String(), "bogus") {
		t.Errorf("error output %q does not name the rejected argument", errOut)
	}
}

func rootWithCapturedOutput(t *testing.T, args ...string) (*cobra.Command, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	root := newRootCommand("1.2.3")
	var out, errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs(args)
	return root, &out, &errOut
}
