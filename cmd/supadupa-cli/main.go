package main

import (
	"context"
	"os"

	"supadupa2026/internal/cli"
)

func main() {
	os.Exit(cli.Runner{}.Run(context.Background(), os.Args[1:]))
}
