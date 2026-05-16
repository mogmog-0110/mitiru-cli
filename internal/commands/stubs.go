package commands

import (
	"errors"

	"github.com/spf13/cobra"
)

var errNotImplemented = errors.New("not implemented yet (Phase 2)")

func newBuildCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "build",
		Short: "Build the current project (not implemented yet)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return errNotImplemented
		},
	}
}

func newRunCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "run",
		Short: "Build and run the current project (not implemented yet)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return errNotImplemented
		},
	}
}

func newCleanCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "clean",
		Short: "Delete the build directory (not implemented yet)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return errNotImplemented
		},
	}
}
