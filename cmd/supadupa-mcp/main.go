package main

import (
	"context"
	"os"

	"supadupa2026/internal/mcp"
)

func main() {
	os.Exit(mcp.Runner{}.Run(context.Background(), os.Args[1:]))
}
