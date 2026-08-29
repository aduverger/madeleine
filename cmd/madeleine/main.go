package main

import (
	"os"

	"github.com/aduverger/madeleine/internal/cli"
)

var (
	version = "dev"
	commit  = "unknown"
)

func main() {
	os.Exit(cli.Main(cli.BuildInfo{Version: version, Commit: commit}))
}
