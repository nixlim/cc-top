package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nixlim/cc-top/internal/config"
	"github.com/nixlim/cc-top/internal/state"
	"github.com/nixlim/cc-top/internal/tui"
)

func main() {
	setupFlag := flag.Bool("setup", false, "Configure Claude Code telemetry settings and exit")
	flag.Parse()

	// Handle --setup: run non-interactive settings merge and exit.
	if *setupFlag {
		RunSetup()
		return // RunSetup calls os.Exit; this is defensive.
	}

	// Load configuration.
	loadResult, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "cc-top: config error: %v\n", err)
		os.Exit(1)
	}
	cfg := loadResult.Config

	// Print any config warnings.
	for _, w := range loadResult.Warnings {
		fmt.Fprintf(os.Stderr, "cc-top: config warning: %s\n", w)
	}

	// Create the state store.
	store := state.NewMemoryStore()

	// Create the shutdown manager.
	shutdownMgr := tui.NewShutdownManager()

	// Set up signal handling for SIGINT/SIGTERM.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// TODO: Start OTLP receivers (Agent 4).
	// TODO: Start process scanner (Agent 3).

	// Create the TUI model.
	model := tui.NewModel(cfg,
		tui.WithStateProvider(store),
		tui.WithStartView(tui.ViewStartup),
		tui.WithOnShutdown(func() {
			_ = shutdownMgr.Shutdown()
		}),
	)

	// Create and run the Bubble Tea program.
	p := tea.NewProgram(model,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	// Handle OS signals in a goroutine.
	go func() {
		select {
		case <-sigCh:
			_ = shutdownMgr.Shutdown()
			p.Quit()
		case <-ctx.Done():
			return
		}
	}()

	// Run the TUI (blocks until quit).
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "cc-top: %v\n", err)
		os.Exit(1)
	}
}
