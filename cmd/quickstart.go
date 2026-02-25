package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var quickstartCmd = &cobra.Command{
	Use:   "quickstart",
	Short: "Show usage modes and getting started guide",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Print(`
Usage modes:

  1. MCP Server (for Claude Desktop / Cursor / Windsurf):
     Add to your MCP config (claude_desktop_config.json):

     {
       "mcpServers": {
         "coco": {
           "command": "/usr/local/bin/coco",
           "args": ["serve"]
         }
       }
     }

  2. Voice Assistant (talk to AI in your terminal):
     coco setup         # Install voice dependencies (first time)
     coco talk          # Start voice conversation

  3. Cloud Relay (connect to Lingti cloud for Feishu/Slack bots):
     coco relay         # Connect to cloud relay service

  4. Message Router (self-hosted Slack/Feishu/Telegram bots):
     export ANTHROPIC_API_KEY="your-key"
     coco router

For more information:
  coco help                       # Show all commands
  coco <command> --help           # Help for specific command
  https://coco.kayz.com/bot            # Documentation
  https://github.com/ruilisi/coco # Source code

`)
	},
}

func init() {
	rootCmd.AddCommand(quickstartCmd)
}
