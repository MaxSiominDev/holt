package forge

import (
	"bytes"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// Open shows an address in whatever the desktop opens addresses with.
func Open(target string) error {
	opener := "xdg-open"
	if runtime.GOOS == "darwin" {
		opener = "open"
	}

	command := exec.Command(opener, target)
	var stderr bytes.Buffer
	command.Stderr = &stderr

	if err := command.Run(); err != nil {
		// "exit status 1" on its own names no cause, so the opener's reason wins.
		if detail := strings.TrimSpace(stderr.String()); detail != "" {
			return fmt.Errorf("opening %s with %s: %s", target, opener, detail)
		}
		return fmt.Errorf("opening %s with %s: %w", target, opener, err)
	}
	return nil
}
