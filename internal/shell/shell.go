// Package shell installs the function that lets holt change the caller's directory.
package shell

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/MaxSiominDev/holt/internal/textblock"
)

// EnvVar is exported by the snippet, which is how doctor knows it is loaded.
const EnvVar = "HOLT_SHELL_INIT"

const (
	blockBegin = "# >>> holt >>>"
	blockEnd   = "# <<< holt <<<"
)

var ErrUnsupported = errors.New("holt has no shell function for that shell")

// Supported lists the shells the snippet is written for. fish is absent: its
// function syntax differs and the code would be untested.
var Supported = []string{"bash", "zsh"}

// A child process cannot change its parent's directory, so the commands that
// resolve one print it and this function performs the cd.
const Snippet = `export ` + EnvVar + `=1

holt() {
  # "${1-}" rather than "$1" so the bare "holt" still works under set -u.
  case "${1-}" in
    new|cd|home)
      local target argument

      # Help is text for a human, not a path to enter. Each argument is
      # examined on its own so that a non-default IFS cannot hide the flag.
      for argument in "$@"; do
        case "$argument" in
          -h|--help) command holt "$@"; return ;;
        esac
      done

      target="$(command holt "$@")" || return
      if [ -n "$target" ]; then
        cd -- "$target"
      fi
      ;;
    *)
      command holt "$@"
      ;;
  esac
}
`

// ConfigFile is the startup file the snippet belongs in.
func ConfigFile(shell string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	switch shell {
	case "zsh":
		// zsh reads $ZDOTDIR/.zshrc when that is set, and only then ~/.zshrc.
		if dir := os.Getenv("ZDOTDIR"); dir != "" {
			return filepath.Join(dir, ".zshrc"), nil
		}
		return filepath.Join(home, ".zshrc"), nil
	case "bash":
		return filepath.Join(home, ".bashrc"), nil
	default:
		return "", fmt.Errorf("%w: %s, only %s", ErrUnsupported, shell, strings.Join(Supported, " and "))
	}
}

// Install reports whether the file changed. The block goes at the end, since it
// needs holt on PATH and a startup file sets that up above.
func Install(shell, path string) (bool, error) {
	// The eval line rather than a copy of the function, so "brew upgrade holt"
	// upgrades the function too. The guard is for the day holt is uninstalled,
	// when every new shell would otherwise say "command not found: holt".
	return textblock.ReplaceInFile(path, blockBegin, blockEnd, []string{
		"if command -v holt >/dev/null 2>&1; then",
		fmt.Sprintf(`  eval "$(holt shell-init %s)"`, shell),
		"fi",
	})
}

// Installed says the block is in the file, not that the file was read.
func Installed(path string) (bool, error) {
	return textblock.PresentInFile(path, blockBegin)
}
