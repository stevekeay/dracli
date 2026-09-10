package main

import (
	"os"

	"github.com/stevekeay/dracli/internal/command"
)

func main() {
	os.Exit(command.Run(os.Args[1:], os.Stdout, os.Stderr, os.Getenv))
}
