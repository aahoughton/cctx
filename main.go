package main

import (
	"os"

	"github.com/aahoughton/cctx/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
