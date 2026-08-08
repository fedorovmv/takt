package main

import (
	"context"
	"os"
	"takt/reference/githubtask"
	"time"
)

func main() {
	timeout := 30 * time.Second
	os.Exit((githubtask.Adapter{Timeout: timeout}).Serve(context.Background(), os.Stdin, os.Stdout, os.Stderr))
}
