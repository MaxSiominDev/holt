package forge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testAddress = "https://github.com/MaxSiominDev/holt/pull/7"

func TestOpen(t *testing.T) {
	log := filepath.Join(t.TempDir(), "argv")
	stubOpener(t, "echo \"$@\" > "+log, 0)

	if err := Open(testAddress); err != nil {
		t.Fatal(err)
	}

	recorded, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(recorded)); got != testAddress {
		t.Fatalf("the opener was called with %q, want %q", got, testAddress)
	}
}

func TestOpenFails(t *testing.T) {
	stubOpener(t, "echo 'Unable to find application named' >&2", 1)

	err := Open(testAddress)

	if err == nil {
		t.Fatal("got no error, want the opener's failure")
	}
	// Silence here would leave the user with nothing but "exit status 1".
	if !strings.Contains(err.Error(), "Unable to find application named") {
		t.Fatalf("the error is %q, want the opener's own reason", err)
	}
}

func TestOpenFailsSilently(t *testing.T) {
	stubOpener(t, "", 1)

	err := Open(testAddress)

	if err == nil {
		t.Fatal("got no error, want the opener's failure")
	}
	if !strings.Contains(err.Error(), "exit status 1") {
		t.Fatalf("the error is %q, want the exit status where there is no reason", err)
	}
}

// Both names, since which one Open runs depends on the platform.
func stubOpener(t *testing.T, body string, status int) {
	t.Helper()
	stubCommands(t, body, status, "open", "xdg-open")
}
