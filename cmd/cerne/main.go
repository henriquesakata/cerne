package main

import (
	"os"

	"github.com/henriquesakata/cerne/internal/cli"
)

func main() {
	if err := cli.Root().Execute(); err != nil {
		cli.NoteWriteAfterError(os.Stderr)
		os.Exit(1)
	}
}
