package cmd

import (
	"fmt"
	"os"
	"os/user"

	"github.com/spf13/cobra"
)

var whoamiCmd = &cobra.Command{
	Use:   "whoami",
	Short: "Show current user information",
	Long:  `Display the current system user information.`,
	Run: func(cmd *cobra.Command, args []string) {
		currentUser, err := user.Current()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting user info: %v\n", err)
			os.Exit(1)
		}

		hostname, _ := os.Hostname()

		fmt.Println("User Information:")
		fmt.Printf("  Username: %s\n", currentUser.Username)
		fmt.Printf("  UID:      %s\n", currentUser.Uid)
		fmt.Printf("  GID:      %s\n", currentUser.Gid)
		fmt.Printf("  Name:     %s\n", currentUser.Name)
		fmt.Printf("  Home:     %s\n", currentUser.HomeDir)
		fmt.Printf("  Hostname: %s\n", hostname)
	},
}

func init() {
	rootCmd.AddCommand(whoamiCmd)
}
