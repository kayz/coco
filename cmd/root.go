package cmd

import (
	"fmt"
	"os"

	"github.com/pltanton/lingti-bot/internal/config"
	"github.com/pltanton/lingti-bot/internal/logger"
	"github.com/pltanton/lingti-bot/internal/mcp"
	"github.com/spf13/cobra"
)

var (
	logLevel         string
	autoApprove      bool
	disableFileTools bool
	metasoAPIKey     string
	tavilyAPIKey     string
	primaryEngine    string
	autoSearch       bool
)

var rootCmd = &cobra.Command{
	Use:   "lingti-bot",
	Short: "MCP server for system resources",
	Long: `lingti-bot is an MCP (Model Context Protocol) server that exposes
computer system resources to AI assistants.

It provides tools for:
  - File operations (read, write, list, search)
  - Shell command execution
  - System information (CPU, memory, disk)
  - Process management
  - Network information`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// Parse and set log level
		level, err := logger.ParseLevel(logLevel)
		if err != nil {
			return err
		}
		logger.SetLevel(level)
		return nil
	},
}

func init() {
	rootCmd.PersistentFlags().StringVar(&logLevel, "log", "info",
		"Log level: trace, debug, info, warn, error, fatal, panic")
	rootCmd.PersistentFlags().BoolVarP(&autoApprove, "yes", "y", false,
		"Automatically approve all operations without prompting (skip security checks)")
	rootCmd.PersistentFlags().BoolVar(&disableFileTools, "no-files", false,
		"Disable all file operation tools")
	rootCmd.PersistentFlags().StringVar(&metasoAPIKey, "metaso-api-key", "",
		"Metaso search API key")
	rootCmd.PersistentFlags().StringVar(&tavilyAPIKey, "tavily-api-key", "",
		"Tavily search API key")
	rootCmd.PersistentFlags().StringVar(&primaryEngine, "search-engine", "metaso",
		"Primary search engine: metaso, tavily")
	rootCmd.PersistentFlags().BoolVar(&autoSearch, "auto-search", true,
		"Enable automatic search for uncertain queries")
}

// IsAutoApprove returns true if auto-approve mode is enabled globally
func IsAutoApprove() bool {
	return autoApprove
}

// loadAllowedPaths returns security allowed_paths from config file.
func loadAllowedPaths() []string {
	if cfg, err := config.Load(); err == nil {
		return cfg.Security.AllowedPaths
	}
	return nil
}

// loadDisableFileTools returns true if file tools are disabled via flag or config.
func loadDisableFileTools() bool {
	if disableFileTools {
		return true
	}
	if cfg, err := config.Load(); err == nil {
		return cfg.Security.DisableFileTools
	}
	return false
}

// loadSecurityOptions returns MCP security options from config file.
func loadSecurityOptions() mcp.SecurityOptions {
	cfg, err := config.Load()
	if err != nil {
		return mcp.SecurityOptions{}
	}
	return mcp.SecurityOptions{
		AllowedPaths:     cfg.Security.AllowedPaths,
		DisableFileTools: cfg.Security.DisableFileTools,
	}
}

// updateSearchConfig updates the search configuration in the config file
func updateSearchConfig() {
	cfg, err := config.Load()
	if err != nil {
		cfg = config.DefaultConfig()
	}

	// Priority: command line flag > environment variable > config file

	// Update primary engine
	if primaryEngine != "" {
		cfg.Search.PrimaryEngine = primaryEngine
	} else if envPrimaryEngine := os.Getenv("SEARCH_ENGINE"); envPrimaryEngine != "" {
		cfg.Search.PrimaryEngine = envPrimaryEngine
	}

	// Update auto search
	if rootCmd.Flags().Changed("auto-search") {
		cfg.Search.AutoSearch = autoSearch
	} else if envAutoSearch := os.Getenv("AUTO_SEARCH"); envAutoSearch != "" {
		cfg.Search.AutoSearch = envAutoSearch == "true" || envAutoSearch == "1"
	}

	// Update Metaso API key
	if metasoAPIKey != "" {
		for i := range cfg.Search.Engines {
			if cfg.Search.Engines[i].Name == "metaso" {
				cfg.Search.Engines[i].APIKey = metasoAPIKey
			}
		}
	} else if envMetasoKey := os.Getenv("METASO_API_KEY"); envMetasoKey != "" {
		for i := range cfg.Search.Engines {
			if cfg.Search.Engines[i].Name == "metaso" {
				cfg.Search.Engines[i].APIKey = envMetasoKey
			}
		}
	}

	// Update Tavily API key
	if tavilyAPIKey != "" {
		for i := range cfg.Search.Engines {
			if cfg.Search.Engines[i].Name == "tavily" {
				cfg.Search.Engines[i].APIKey = tavilyAPIKey
			}
		}
	} else if envTavilyKey := os.Getenv("TAVILY_API_KEY"); envTavilyKey != "" {
		for i := range cfg.Search.Engines {
			if cfg.Search.Engines[i].Name == "tavily" {
				cfg.Search.Engines[i].APIKey = envTavilyKey
			}
		}
	}

	// Save config
	if err := cfg.Save(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Failed to save config: %v\n", err)
	}
}

func Execute() {
	// Update search config from command line flags
	updateSearchConfig()

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
