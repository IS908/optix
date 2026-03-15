package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newServerCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "server",
		Short: "Start the optix backend server (IB connection + gRPC)",
		RunE: func(cmd *cobra.Command, args []string) error {
			// TODO: Implement server startup (wires IB client + gRPC + SQLite)
			fmt.Println("Starting optix server...")
			fmt.Printf("IB TWS: %s:%d\n", ibHost, ibPort)
			fmt.Printf("DB: %s\n", dbPath)
			fmt.Println("(Not yet implemented - coming soon)")
			return nil
		},
	}
}
