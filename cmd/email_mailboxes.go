package cmd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/avilabss/invoice-piper/internal/config"
	"github.com/avilabss/invoice-piper/internal/email"
	"github.com/spf13/cobra"
)

type mailboxLister interface {
	ListMailboxes(ctx context.Context) ([]string, error)
}

var newMailboxClient = func(account config.IMAPAccount) mailboxLister {
	return email.NewClient(account)
}

var emailMailboxesCmd = &cobra.Command{
	Use:   "mailboxes",
	Short: "List available mailboxes for configured email accounts",
	Long:  "Lists available mailboxes for each configured account. Use these names in email.accounts[].mailboxes.",
	Example: "  invp email mailboxes\n" +
		"  invp email mailboxes --config ./config.json",
	RunE: func(cmd *cobra.Command, args []string) error {
		parentCtx := cmd.Context()
		if parentCtx == nil {
			parentCtx = context.Background()
		}

		workflowCtx, cancel := context.WithTimeout(parentCtx, emailMailboxesWorkflowTimeout)
		defer cancel()

		var failures int

		for _, account := range cfg.Email.Accounts {
			if workflowErr := workflowCtx.Err(); workflowErr != nil {
				if errors.Is(workflowErr, context.DeadlineExceeded) {
					return workflowTimeoutError("email mailboxes", emailMailboxesWorkflowTimeout, nil)
				}
				return workflowErr
			}

			slog.Debug("Fetching mailboxes", "account", account.Name)

			fmt.Printf("Account: %s (%s)\n", account.Name, account.Username)

			client := newMailboxClient(account)
			mailboxes, err := runWithWorkflowTimeout(workflowCtx, func() ([]string, error) {
				return client.ListMailboxes(workflowCtx)
			})
			for _, mbox := range mailboxes {
				fmt.Printf("  - %s\n", mbox)
			}

			if err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(workflowCtx.Err(), context.Canceled) {
					return context.Canceled
				}

				slog.Error("Failed to list mailboxes", "account", account.Name, "error", err)
				fmt.Printf("  error: %v\n", err)
				failures++

				if workflowTimedOut(workflowCtx, err) {
					return workflowTimeoutError("email mailboxes", emailMailboxesWorkflowTimeout, err)
				}
			}
			fmt.Println()
		}

		if errors.Is(workflowCtx.Err(), context.Canceled) {
			return context.Canceled
		}

		if workflowTimedOut(workflowCtx, nil) {
			return workflowTimeoutError("email mailboxes", emailMailboxesWorkflowTimeout, nil)
		}

		if failures > 0 {
			return fmt.Errorf("failed to list mailboxes for %d of %d account(s)", failures, len(cfg.Email.Accounts))
		}
		return nil
	},
}

func init() {
	emailCmd.AddCommand(emailMailboxesCmd)
}
