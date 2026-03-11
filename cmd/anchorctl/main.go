package main

import (
	"context"
	"os"

	"anchordb/internal/cli"
)

func main() {
	os.Exit(cli.Run(context.Background(), os.Stdout, os.Stderr, os.Args[1:], ""))
}
