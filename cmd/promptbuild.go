package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/kayz/coco/internal/config"
	"github.com/kayz/coco/internal/logger"
	"github.com/kayz/coco/internal/promptbuild"
	"github.com/spf13/cobra"
)

var (
	promptBuildRequestPath string
	promptBuildOutputPath  string
	promptBuildRecord      bool
	promptBuildRecordDir   string
)

var promptBuildCmd = &cobra.Command{
	Use:   "promptbuild",
	Short: "Assemble a prompt from templates, references, and SQLite history",
	RunE: func(cmd *cobra.Command, args []string) error {
		if promptBuildRequestPath == "" {
			return fmt.Errorf("--request is required")
		}

		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}
		builder := promptbuild.NewBuilder(cfg.PromptBuild)

		reqBytes, err := os.ReadFile(promptBuildRequestPath)
		if err != nil {
			return fmt.Errorf("read request: %w", err)
		}

		var req promptbuild.BuildRequest
		if err := json.Unmarshal(reqBytes, &req); err != nil {
			return fmt.Errorf("parse request: %w", err)
		}

		out, err := builder.Build(req)
		if err != nil {
			return err
		}

		if promptBuildOutputPath == "" {
			fmt.Println(out)
		} else {
			if err := os.WriteFile(promptBuildOutputPath, []byte(out), 0644); err != nil {
				return fmt.Errorf("write output: %w", err)
			}
		}

		if promptBuildRecord {
			if err := recordPromptBuild(cfg.PromptBuild, reqBytes, out); err != nil {
				logger.Warn("record promptbuild failed: %v", err)
			}
		}

		return nil
	},
}

func recordPromptBuild(cfg config.PromptBuildConfig, reqBytes []byte, out string) error {
	recordDir := promptBuildRecordDir
	if recordDir == "" {
		recordDir = "promptbuild-records"
	}
	if !filepath.IsAbs(recordDir) {
		root := cfg.RootDir
		if root == "" {
			root = "."
		}
		recordDir = filepath.Join(root, recordDir)
	}
	if err := os.MkdirAll(recordDir, 0755); err != nil {
		return err
	}

	ts := time.Now().Format("20060102-150405")
	reqPath := filepath.Join(recordDir, fmt.Sprintf("request-%s.json", ts))
	outPath := filepath.Join(recordDir, fmt.Sprintf("output-%s.txt", ts))

	if err := os.WriteFile(reqPath, reqBytes, 0644); err != nil {
		return err
	}
	if err := os.WriteFile(outPath, []byte(out), 0644); err != nil {
		return err
	}
	return nil
}

func init() {
	promptBuildCmd.Flags().StringVar(&promptBuildRequestPath, "request", "", "Path to JSON request file")
	promptBuildCmd.Flags().StringVar(&promptBuildOutputPath, "output", "", "Write output to file (default: stdout)")
	promptBuildCmd.Flags().BoolVar(&promptBuildRecord, "record", false, "Record request/output to files")
	promptBuildCmd.Flags().StringVar(&promptBuildRecordDir, "record-dir", "", "Directory to write record files (default: promptbuild-records)")
	rootCmd.AddCommand(promptBuildCmd)
}
