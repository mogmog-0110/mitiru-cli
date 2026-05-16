package main

import (
	"fmt"
	"os"

	"github.com/mogmog-0110/mitiru-cli/internal/commands"
)

func main() {
	if err := commands.NewRootCommand().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
