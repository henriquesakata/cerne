package main

import (
	"os"

	"gitlab.com/rockt/cerne/internal/cli"
)

func main() {
	if err := cli.Root().Execute(); err != nil {
		os.Exit(1)
	}
}
