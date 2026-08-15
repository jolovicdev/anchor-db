package main

import (
	"context"
	"os"

	"github.com/jolovicdev/anchor-db/internal/cli"
)

func main() {
	os.Exit(cli.Run(context.Background(), os.Stdout, os.Stderr, os.Args[1:], ""))
}
