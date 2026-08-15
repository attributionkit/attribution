package main

import (
	"os"

	"github.com/attributionkit/attribution/internal/attribution"
)

func main() {
	os.Exit(attribution.RunCLI(os.Args[1:], os.Stdout, os.Stderr))
}
