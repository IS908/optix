package main

import (
	"os"

	// Pulse view inference depends on America/New_York; embedding tzdata
	// keeps it DST-correct on containers/trimmed images without a system
	// tz database (instead of degrading to the FixedZone EST fallback).
	_ "time/tzdata"

	"github.com/IS908/optix/internal/cli"
)

// version is overridden at build time via:
//
//	go build -ldflags="-X main.version=v1.2.3"
//
// (see scripts/build-release.sh). Source builds keep "dev".
var version = "dev"

func main() {
	cli.SetVersion(version)
	if err := cli.Execute(); err != nil {
		os.Exit(cli.AsExitCode(err))
	}
}
