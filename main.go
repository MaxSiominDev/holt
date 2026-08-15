package main

import (
	"os"

	"github.com/MaxSiominDev/holt/cmd"
)

// Replaced at build time with -ldflags "-X main.version=<tag>".
var version = "dev"

func main() {
	if err := cmd.Execute(version); err != nil {
		os.Exit(1)
	}
}
