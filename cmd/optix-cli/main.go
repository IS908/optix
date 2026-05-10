package main

import "github.com/IS908/optix/internal/cli"

// version is overridden at build time via:
//   go build -ldflags="-X main.version=v1.2.3"
// (see scripts/build-release.sh). Source builds keep "dev".
var version = "dev"

func main() {
	cli.SetVersion(version)
	cli.Execute()
}
