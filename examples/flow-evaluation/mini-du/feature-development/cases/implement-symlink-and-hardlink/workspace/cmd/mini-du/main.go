package main

import (
	"fmt"
	"os"

	"example.test/mini-du/internal/du"
)

func main() {
	if err := du.Run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
