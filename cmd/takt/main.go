package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"takt/internal/cli"
)

func main() {
	args := os.Args[1:]
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := cli.RunContext(ctx, args); err != nil {
		if cli.WantsJSON(args) {
			_ = cli.PrintErrorJSON(err)
		} else {
			fmt.Fprintln(os.Stderr, "error:", err)
		}
		os.Exit(1)
	}
}
