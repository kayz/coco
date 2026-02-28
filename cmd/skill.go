package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	skillspkg "github.com/kayz/coco/internal/skills"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(newSkillCommand())
}

func newSkillCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "skill",
		Aliases: []string{"skills"},
		Short:   "Manage local skills",
	}
	cmd.AddCommand(
		newSkillSearchCommand(),
		newSkillInstallCommand(),
		newSkillListCommand(),
		newSkillDownloadCommand(),
	)
	return cmd
}

func newSkillListCommand() *cobra.Command {
	var showEligible bool
	var verbose bool
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List discovered skills",
		RunE: func(cmd *cobra.Command, args []string) error {
			report := skillspkg.BuildStatusReport(nil, nil)
			out := skillspkg.FormatList(report, skillspkg.FormatListOptions{
				JSON:     asJSON,
				Eligible: showEligible,
				Verbose:  verbose,
			})
			_, err := fmt.Fprintln(cmd.OutOrStdout(), out)
			return err
		},
	}

	cmd.Flags().BoolVar(&showEligible, "eligible", false, "Show only ready skills")
	cmd.Flags().BoolVar(&verbose, "verbose", false, "Show missing requirement details")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Render output as JSON")
	return cmd
}

func newSkillSearchCommand() *cobra.Command {
	var showEligible bool
	var verbose bool
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "search [keyword]",
		Short: "Search skills by name or description",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := ""
			if len(args) > 0 {
				query = args[0]
			}

			report := skillspkg.BuildStatusReport(nil, nil)
			report = filterSkillReport(report, query)
			out := skillspkg.FormatList(report, skillspkg.FormatListOptions{
				JSON:     asJSON,
				Eligible: showEligible,
				Verbose:  verbose,
			})
			_, err := fmt.Fprintln(cmd.OutOrStdout(), out)
			return err
		},
	}

	cmd.Flags().BoolVar(&showEligible, "eligible", false, "Show only ready skills")
	cmd.Flags().BoolVar(&verbose, "verbose", false, "Show missing requirement details")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Render output as JSON")
	return cmd
}

func newSkillInstallCommand() *cobra.Command {
	var confirm bool
	var force bool
	var overwrite bool
	var managedDir string
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "install <name>",
		Short: "Install a discovered skill into managed skills directory",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := strings.TrimSpace(args[0])
			entry, found := skillspkg.FindSkillByName(name, nil, nil)
			if !found {
				return fmt.Errorf("skill %q not found; run `coco skill search` first", name)
			}

			assessment := skillspkg.EvaluateSkillSecurity(entry)
			if asJSON {
				payload := map[string]any{
					"skill":      entry.Name,
					"assessment": assessment,
				}
				data, _ := json.MarshalIndent(payload, "", "  ")
				if _, err := fmt.Fprintln(cmd.OutOrStdout(), string(data)); err != nil {
					return err
				}
			} else {
				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Security assessment for %s: %s (score=%d)\n", entry.Name, assessment.Level, assessment.Score); err != nil {
					return err
				}
				for _, reason := range assessment.Reasons {
					if _, err := fmt.Fprintf(cmd.OutOrStdout(), "- %s\n", reason); err != nil {
						return err
					}
				}
			}

			if assessment.Level == skillspkg.SecurityDangerous && !force {
				return fmt.Errorf("skill %q is rated dangerous; re-run with --force to override", entry.Name)
			}
			if !confirm && !IsAutoApprove() {
				return fmt.Errorf("installation requires explicit confirmation; re-run with --yes")
			}

			result, err := skillspkg.InstallSkillEntry(entry, skillspkg.InstallOptions{
				ManagedDir: managedDir,
				Overwrite:  overwrite,
			})
			if err != nil {
				return err
			}

			if asJSON {
				payload := map[string]any{
					"skill":      entry.Name,
					"installed":  !result.AlreadyExists,
					"result":     result,
					"assessment": result.Assessment,
				}
				data, _ := json.MarshalIndent(payload, "", "  ")
				_, err = fmt.Fprintln(cmd.OutOrStdout(), string(data))
				return err
			}

			if result.AlreadyExists {
				_, err = fmt.Fprintf(cmd.OutOrStdout(), "Skill %s already installed at %s\n", entry.Name, result.InstalledPath)
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Installed %s to %s\n", entry.Name, result.InstalledPath)
			return err
		},
	}

	cmd.Flags().BoolVarP(&confirm, "yes", "y", false, "Confirm installation")
	cmd.Flags().BoolVar(&force, "force", false, "Allow install even when assessment is dangerous")
	cmd.Flags().BoolVar(&overwrite, "overwrite", false, "Overwrite existing managed skill directory")
	cmd.Flags().StringVar(&managedDir, "dest", "", "Managed skills directory override")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Render output as JSON")
	return cmd
}

func newSkillDownloadCommand() *cobra.Command {
	var managedDir string

	cmd := &cobra.Command{
		Use:   "download",
		Short: "Download bundled skills from GitHub into managed directory",
		RunE: func(cmd *cobra.Command, args []string) error {
			count, err := skillspkg.DownloadBundledSkills(managedDir)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Downloaded %d skills\n", count)
			return err
		},
	}
	cmd.Flags().StringVar(&managedDir, "dest", "", "Managed skills directory override")
	return cmd
}

func filterSkillReport(report skillspkg.StatusReport, query string) skillspkg.StatusReport {
	query = strings.TrimSpace(strings.ToLower(query))
	if query == "" {
		return report
	}

	filtered := make([]skillspkg.SkillStatus, 0, len(report.Skills))
	for _, skill := range report.Skills {
		name := strings.ToLower(skill.Name)
		desc := strings.ToLower(skill.Description)
		if strings.Contains(name, query) || strings.Contains(desc, query) {
			filtered = append(filtered, skill)
		}
	}
	report.Skills = filtered
	return report
}
