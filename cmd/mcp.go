package cmd

import (
	"github.com/aeon022/timectl/internal/mcpserver"
	"github.com/spf13/cobra"
)

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Start the MCP server (stdio transport)",
	RunE: func(cmd *cobra.Command, args []string) error {
		return mcpserver.Serve()
	},
}
