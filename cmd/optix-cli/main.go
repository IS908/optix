package main

import (
	"os"

	"github.com/IS908/optix/internal/cli"
)

// version is overridden at build time via:
//   go build -ldflags="-X main.version=v1.2.3"
// (see scripts/build-release.sh). Source builds keep "dev".
var version = "dev"

func main() {
	cli.SetVersion(version)
	if err := cli.Execute(); err != nil {
		os.Exit(cli.AsExitCode(err))
	}
}
