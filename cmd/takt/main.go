package main

import (
	"fmt"
	"os"

	"takt/internal/cli"
)

func main() {
	args := os.Args[1:]
	if err := cli.Run(args); err != nil {
		if cli.WantsJSON(args) {
			_ = cli.PrintErrorJSON(err)
		} else {
			fmt.Fprintln(os.Stderr, "error:", err)
		}
		os.Exit(1)
	}
}
