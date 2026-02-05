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
         "lingti-bot": {
           "command": "/usr/local/bin/lingti-bot",
           "args": ["serve"]
         }
       }
     }

  2. Voice Assistant (talk to AI in your terminal):
     lingti-bot setup         # Install voice dependencies (first time)
     lingti-bot talk          # Start voice conversation

  3. Cloud Relay (connect to Lingti cloud for Feishu/Slack bots):
     lingti-bot relay         # Connect to cloud relay service

  4. Message Router (self-hosted Slack/Feishu/Telegram bots):
     export ANTHROPIC_API_KEY="your-key"
     lingti-bot router

For more information:
  lingti-bot help                       # Show all commands
  lingti-bot <command> --help           # Help for specific command
  https://cli.lingti.com/bot            # Documentation
  https://github.com/ruilisi/lingti-bot # Source code

`)
	},
}

func init() {
	rootCmd.AddCommand(quickstartCmd)
}
