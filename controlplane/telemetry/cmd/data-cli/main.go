package main

import (
	"os"

	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/data/cli"
)

func main() {
	os.Exit(int(cli.Run()))
}
