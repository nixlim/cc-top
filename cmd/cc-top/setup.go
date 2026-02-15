package main

import (
	"fmt"
	"os"

	"github.com/nixlim/cc-top/internal/config"
	"github.com/nixlim/cc-top/internal/settings"
)

// RunSetup performs non-interactive settings merge and prints the result.
// It loads the cc-top config to determine the gRPC port, then merges the
// required OTel environment variables into ~/.claude/settings.json.
//
// Exit codes:
//   - 0: success or already configured
//   - 1: error
func RunSetup() {
	// Load config to get the gRPC port.
	loadResult, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	// Print any config warnings.
	for _, w := range loadResult.Warnings {
		fmt.Fprintf(os.Stderr, "Config warning: %s\n", w)
	}

	grpcPort := loadResult.Config.Receiver.GRPCPort

	// Run the settings merge in non-interactive mode.
	output := settings.Merge(settings.MergeOptions{
		Interactive: false,
		GRPCPort:    grpcPort,
	})

	// Print messages.
	for _, msg := range output.Messages {
		fmt.Println(msg)
	}

	// Print warnings.
	for _, w := range output.Warnings {
		fmt.Fprintln(os.Stderr, w)
	}

	switch output.Result {
	case settings.MergeSuccess:
		fmt.Println("Settings updated. Restart your Claude Code sessions to apply.")
		os.Exit(0)
	case settings.MergeAlreadyConfigured:
		fmt.Println("Already configured. No changes needed.")
		os.Exit(0)
	case settings.MergeError:
		fmt.Fprintf(os.Stderr, "Error: %v\n", output.Err)
		os.Exit(1)
	default:
		// Should not happen in non-interactive mode.
		fmt.Fprintf(os.Stderr, "Unexpected result: %v\n", output.Result)
		os.Exit(1)
	}
}
