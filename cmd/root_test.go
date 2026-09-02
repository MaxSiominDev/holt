package cmd

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestVersionFlag(t *testing.T) {
	root, out, _ := holtCommand(t, "--version")

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
		if command.GroupID == "" {
			t.Errorf("%q has no group, so the help files it under Additional Commands", command.Name())
		}
	}
}

func TestHelpContents(t *testing.T) {
	root, out, _ := holtCommand(t, "--help")
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
	// Spelled out, or both sides drop together and assert nothing; matched as a
	// rendered row, since these names also appear in the prose above.
	for _, name := range []string{
		"cd", "doctor", "home", "ls", "main", "mirror", "new",
		"open", "pull", "push", "rebase", "rm", "shell-init", "status", "uncommit",
	} {
		if !strings.Contains(help, "\n  "+name+" ") {
			t.Errorf("the help does not list %q:\n%s", name, help)
		}
	}
}

func TestVersionHasNoShorthand(t *testing.T) {
	root, _, errOut := holtCommand(t, "-v")

	if err := root.Execute(); err == nil {
		t.Fatalf("-v was accepted, output was %q", errOut)
	}
}

func TestUnknownArgument(t *testing.T) {
	root, _, errOut := holtCommand(t, "bogus")

	err := root.Execute()

	if err == nil {
		t.Fatal("an unknown argument was accepted")
	}
	if !strings.Contains(errOut.String(), "bogus") {
		t.Errorf("error output %q does not name the rejected argument", errOut)
	}
}

func TestListSome(t *testing.T) {
	values := []string{"a", "b", "c"}

	tests := []struct {
		name  string
		limit int
		want  string
	}{
		// The limit is what still fits, not what is already too many: at the
		// boundary the whole list is there and there is nothing left to count.
		{name: "as many as the limit", limit: 3, want: "a, b, c"},
		{name: "one over", limit: 2, want: "a, b, and 1 more"},
		{name: "one shown", limit: 1, want: "a, and 2 more"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := listSome(values, test.limit); got != test.want {
				t.Errorf("got %q, want %q", got, test.want)
			}
		})
	}
}

func TestOneLine(t *testing.T) {
	// mirror sync reports one line per worktree, and a worktree can fail on
	// several patterns at once.
	joined := errors.Join(errors.New("first went wrong"), errors.New("so did the second"))

	if got, want := oneLine(joined), "first went wrong; so did the second"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func holtCommand(t *testing.T, args ...string) (*cobra.Command, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	root := newRootCommand("1.2.3")
	var out, errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs(args)
	return root, &out, &errOut
}
