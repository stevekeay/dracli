package main

import (
	"os"

	"github.com/stevekeay/dracli/internal/command"
)

func main() {
	os.Exit(command.RunCLI(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, os.Getenv))
}
