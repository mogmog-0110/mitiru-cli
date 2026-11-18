package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/mogmog-0110/mitiru-cli/internal/commands"
)

func main() {
	restoreConsoleCP := setConsoleUTF8()
	if err := commands.NewRootCommand().Execute(); err != nil {
		restoreConsoleCP()
		var exitErr *commands.ExitCodeError
		if errors.As(err, &exitErr) {
			if exitErr.Message != "" {
				fmt.Fprintln(os.Stderr, exitErr.Message)
			}
			os.Exit(exitErr.Code)
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	restoreConsoleCP()
}
