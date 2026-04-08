package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/avilabss/invoice-piper/internal/email"
	"github.com/spf13/cobra"
)

var runEmailExport = email.Export

var emailExportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export invoice attachments from email accounts",
	Long:  "Connects to configured IMAP accounts, searches emails for the given time period, and downloads invoice attachments.",
	Example: "  invp email export\n" +
		"  invp email export --year 2025 --month 1\n" +
		"  invp email export --year 2025 --month 1 --concurrency 1 -vv",
	RunE: func(cmd *cobra.Command, args []string) error {
		year, _ := cmd.Flags().GetInt("year")
		month, _ := cmd.Flags().GetInt("month")
		concurrency, _ := cmd.Flags().GetInt("concurrency")

		now := time.Now()
		if year == 0 {
			year = now.Year()
		}
		if month == 0 {
			month = int(now.Month())
		}

		if month < 1 || month > 12 {
			return fmt.Errorf("invalid month: %d (must be 1-12)", month)
		}
		if year < 1970 || year > now.Year()+1 {
			return fmt.Errorf("invalid year: %d (must be between 1970 and %d)", year, now.Year()+1)
		}

		slog.Info("Exporting invoices", "year", year, "month", month)

		parentCtx := cmd.Context()
		if parentCtx == nil {
			parentCtx = context.Background()
		}

		workflowCtx, cancel := context.WithTimeout(parentCtx, emailExportWorkflowTimeout)
		defer cancel()

		results, err := runWithWorkflowTimeout(workflowCtx, func() ([]email.ExportResult, error) {
			return runEmailExport(workflowCtx, cfg, year, month, concurrency)
		})

		// Print summary to stdout (always visible regardless of verbosity)
		fmt.Println("\n--- Summary ---")
		for _, r := range results {
			fmt.Printf("Account: %s\n", r.AccountName)
			fmt.Printf("  Emails processed: %d\n", r.TotalEmails)
			fmt.Printf("  Attachments saved: %d\n", r.TotalAttachments)
			if r.TotalBodyPDFs > 0 {
				fmt.Printf("  Body→PDF conversions: %d\n", r.TotalBodyPDFs)
			}
			if r.PasswordLocked > 0 {
				fmt.Printf("  Password-locked PDFs: %d\n", r.PasswordLocked)
			}
			if r.Errors > 0 {
				fmt.Printf("  Errors: %d\n", r.Errors)
			}
		}

		if workflowTimedOut(workflowCtx, err) {
			return workflowTimeoutError("email export", emailExportWorkflowTimeout, err)
		}

		return err
	},
}

func init() {
	emailExportCmd.Flags().Int("year", 0, "year to export (default: current year)")
	emailExportCmd.Flags().Int("month", 0, "month to export (default: current month)")
	emailExportCmd.Flags().Int("concurrency", email.DefaultConcurrency, "max accounts to process in parallel (<=0 uses default)")
	emailCmd.AddCommand(emailExportCmd)
}
