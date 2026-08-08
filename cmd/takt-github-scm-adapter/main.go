package main

import (
	"context"
	"os"
	"takt/reference/githubscm"
)

func main() {
	os.Exit((githubscm.Adapter{}).Serve(context.Background(), os.Stdin, os.Stdout, os.Stderr))
}
