// optix-server is a convenience binary that starts the Optix web UI server.
// Running it with no subcommand is equivalent to "optix server".
//
// Usage:
//
//	optix-server [--web-addr=127.0.0.1:8080] [--ib-host=127.0.0.1] [flags...]
package main

import (
	"os"

	"github.com/IS908/optix/internal/cli"
)

func main() {
	// If no subcommand is given (or the first arg looks like a flag), inject
	// "server" so the binary defaults to starting the web UI without the user
	// having to type it explicitly.
	if len(os.Args) == 1 || (len(os.Args) > 1 && os.Args[1][0] == '-') {
		os.Args = append([]string{os.Args[0], "server"}, os.Args[1:]...)
	}
	cli.Execute()
}
