package main

import (
	"fmt"
	"os"

	"github.com/mogmog-0110/mitiru-cli/internal/commands"
)

func main() {
	restoreConsoleCP := setConsoleUTF8()
	if err := commands.NewRootCommand().Execute(); err != nil {
		restoreConsoleCP()
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	restoreConsoleCP()
}
