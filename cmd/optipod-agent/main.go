// optipod-agent exports cluster inventory and recommendations to the shared console.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/optipod/optipod/internal/consoleagent"
)

func main() {
	args := os.Args[1:]
	// Helm's agent StatefulSet uses the same arguments as the console CLI.
	if len(args) > 0 && args[0] == "agent" {
		args = args[1:]
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := consoleagent.RunCommand(ctx, args); err != nil && err != flag.ErrHelp {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
