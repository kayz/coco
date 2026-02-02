package cmd

import (
	"context"
	"fmt"
	"os"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcontact "github.com/larksuite/oapi-sdk-go/v3/service/contact/v3"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	"github.com/spf13/cobra"
)

var feishuCmd = &cobra.Command{
	Use:   "feishu",
	Short: "Feishu related commands",
	Long:  `Commands for interacting with Feishu/Lark platform.`,
}

var feishuInfoCmd = &cobra.Command{
	Use:   "info",
	Short: "Show Feishu bot information",
	Long:  `Connect to Feishu and display bot information.`,
	Run:   runFeishuInfo,
}

var feishuUserCmd = &cobra.Command{
	Use:   "user <open_id>",
	Short: "Look up a Feishu user by open_id",
	Long:  `Look up user information by their Feishu open_id.`,
	Args:  cobra.ExactArgs(1),
	Run:   runFeishuUser,
}

func init() {
	rootCmd.AddCommand(feishuCmd)
	feishuCmd.AddCommand(feishuInfoCmd)
	feishuCmd.AddCommand(feishuUserCmd)
}

func getFeishuClient() (*lark.Client, error) {
	appID := os.Getenv("FEISHU_APP_ID")
	appSecret := os.Getenv("FEISHU_APP_SECRET")

	if appID == "" || appSecret == "" {
		return nil, fmt.Errorf("FEISHU_APP_ID and FEISHU_APP_SECRET environment variables are required")
	}

	return lark.NewClient(appID, appSecret), nil
}

func runFeishuInfo(cmd *cobra.Command, args []string) {
	client, err := getFeishuClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	ctx := context.Background()

	// List chats to verify connection
	req := larkim.NewListChatReqBuilder().
		PageSize(5).
		Build()

	result, err := client.Im.Chat.List(ctx, req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error connecting to Feishu: %v\n", err)
		os.Exit(1)
	}

	if !result.Success() {
		fmt.Fprintf(os.Stderr, "Feishu API error: code=%d, msg=%s\n", result.Code, result.Msg)
		os.Exit(1)
	}

	fmt.Println("Feishu Bot Information:")
	fmt.Println("  Status: Connected")
	fmt.Printf("  App ID: %s\n", os.Getenv("FEISHU_APP_ID"))

	if result.Data != nil && result.Data.Items != nil {
		fmt.Printf("  Chats: %d\n", len(result.Data.Items))
		fmt.Println("\nRecent Chats:")
		for _, chat := range result.Data.Items {
			name := ""
			if chat.Name != nil {
				name = *chat.Name
			}
			chatID := ""
			if chat.ChatId != nil {
				chatID = *chat.ChatId
			}
			fmt.Printf("  - %s (ID: %s)\n", name, chatID)
		}
	}

	fmt.Println("\nTo get your Feishu user ID, send /whoami to the bot in Feishu.")
}

func runFeishuUser(cmd *cobra.Command, args []string) {
	client, err := getFeishuClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	openID := args[0]
	ctx := context.Background()

	req := larkcontact.NewGetUserReqBuilder().
		UserId(openID).
		UserIdType(larkcontact.UserIdTypeOpenId).
		Build()

	result, err := client.Contact.User.Get(ctx, req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error looking up user: %v\n", err)
		os.Exit(1)
	}

	if !result.Success() {
		fmt.Fprintf(os.Stderr, "Feishu API error: code=%d, msg=%s\n", result.Code, result.Msg)
		os.Exit(1)
	}

	if result.Data != nil && result.Data.User != nil {
		user := result.Data.User
		fmt.Println("Feishu User Information:")
		if user.Name != nil {
			fmt.Printf("  Name:     %s\n", *user.Name)
		}
		if user.OpenId != nil {
			fmt.Printf("  Open ID:  %s\n", *user.OpenId)
		}
		if user.UserId != nil {
			fmt.Printf("  User ID:  %s\n", *user.UserId)
		}
		if user.Mobile != nil {
			fmt.Printf("  Mobile:   %s\n", *user.Mobile)
		}
		if user.Email != nil {
			fmt.Printf("  Email:    %s\n", *user.Email)
		}
	} else {
		fmt.Println("User not found or no permission to view.")
	}
}
