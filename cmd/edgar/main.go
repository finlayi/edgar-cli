package main

import (
	"context"
	"os"

	"github.com/finlayi/edgar-cli/pkg/edgar"
)

func main() {
	os.Exit(edgar.Run(context.Background(), os.Args[1:]))
}
