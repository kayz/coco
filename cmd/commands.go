package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var commandsCmd = &cobra.Command{
	Use:   "commands",
	Short: "List built-in bot commands",
	Long:  `List all built-in commands that users can send to the bot.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Built-in Bot Commands:")
		fmt.Println()
		fmt.Println("  /whoami, 我是谁, 我的id")
		fmt.Println("      Show your user information (ID, username, platform, channel)")
		fmt.Println()
		fmt.Println("  /help, 帮助")
		fmt.Println("      Show available commands")
		fmt.Println()
		fmt.Println("Any other message will be processed by Claude AI.")
	},
}

func init() {
	rootCmd.AddCommand(commandsCmd)
}
