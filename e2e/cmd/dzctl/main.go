package main

import (
	"os"

	devnetcmd "github.com/malbeclabs/doublezero/e2e/internal/devnet/cmd"
)

func main() {
	os.Exit(int(devnetcmd.Run()))
}
