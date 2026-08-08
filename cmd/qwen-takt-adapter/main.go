package main

import (
	"context"
	"os"

	"takt/reference/qwencode"
)

func main() {
	code := (qwencode.Adapter{}).Serve(context.Background(), os.Stdin, os.Stdout, os.Stderr)
	os.Exit(code)
}
