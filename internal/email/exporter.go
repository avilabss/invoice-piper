package email

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/avilabss/invoice-piper/internal/config"
	"github.com/avilabss/invoice-piper/internal/logger"
	"github.com/avilabss/invoice-piper/internal/output"
	"github.com/avilabss/invoice-piper/internal/pdfutil"
	"github.com/avilabss/invoice-piper/internal/resolver"
)

// DefaultConcurrency is the default number of accounts processed in parallel.
const DefaultConcurrency = 3

// ExportResult holds statistics from an export operation.
type ExportResult struct {
	AccountName      string
	TotalEmails      int
	TotalAttachments int
	TotalBodyPDFs    int
	PasswordLocked   int
	Errors           int
}

// accountResult pairs an ExportResult with its original index for ordered output.
type accountResult struct {
	index  int
	result ExportResult
	err    error
}

var exportAccountFn = exportAccount

// Export runs the email export pipeline for all configured accounts.
// Accounts are processed concurrently up to the given concurrency limit.
// If concurrency <= 0, DefaultConcurrency is used.
func Export(ctx context.Context, cfg *config.Config, year, month, concurrency int) ([]ExportResult, error) {
	writer := output.NewWriter(cfg.OutputDir)
	resolver.SetAliases(cfg.ProviderAliases)

	// Create a single Chrome allocator shared across all HTML→PDF conversions.
	chromeCtx, chromeCancel := NewChromeAllocator(ctx)
	defer chromeCancel()

	if concurrency <= 0 {
		concurrency = DefaultConcurrency
	}

	accounts := cfg.Email.Accounts
	results := make([]ExportResult, len(accounts))
	var accountsWithErrors int

	if len(accounts) == 1 {
		// Fast path: no goroutine overhead for single account
		fmt.Fprintf(os.Stderr, "Processing account 1/1: %s\n", accounts[0].Name)
		slog.Info("Processing account", "name", accounts[0].Name, "username", accounts[0].Username)

		result, err := exportAccountFn(chromeCtx, accounts[0], writer, year, month)
		result.AccountName = accounts[0].Name
		if err != nil {
			fmt.Fprintf(os.Stderr, "  Done with errors: %d emails, %d attachments saved (%v)\n", result.TotalEmails, result.TotalAttachments, err)
			slog.Error("Account export failed", "account", accounts[0].Name, "error", err)
		} else if result.Errors > 0 {
			fmt.Fprintf(os.Stderr, "  Done with errors: %d emails, %d attachments saved (%d error(s))\n", result.TotalEmails, result.TotalAttachments, result.Errors)
		} else {
			fmt.Fprintf(os.Stderr, "  Done: %d emails, %d attachments saved\n", result.TotalEmails, result.TotalAttachments)
		}
		if err != nil || result.Errors > 0 {
			accountsWithErrors++
		}
		results[0] = result
	} else {
		// Parallel path: use semaphore to limit concurrency
		sem := make(chan struct{}, concurrency)
		resultCh := make(chan accountResult, len(accounts))
		completed := make([]bool, len(accounts))

		recordResult := func(ar accountResult) {
			if ar.err != nil {
				fmt.Fprintf(os.Stderr, "  %s: Done with errors: %d emails, %d attachments saved (%v)\n",
					ar.result.AccountName, ar.result.TotalEmails, ar.result.TotalAttachments, ar.err)
				slog.Error("Account export failed", "account", ar.result.AccountName, "error", ar.err)
			} else if ar.result.Errors > 0 {
				fmt.Fprintf(os.Stderr, "  %s: Done with errors: %d emails, %d attachments saved (%d error(s))\n",
					ar.result.AccountName, ar.result.TotalEmails, ar.result.TotalAttachments, ar.result.Errors)
			} else {
				fmt.Fprintf(os.Stderr, "  %s: Done: %d emails, %d attachments saved\n",
					ar.result.AccountName, ar.result.TotalEmails, ar.result.TotalAttachments)
			}
			if ar.err != nil || ar.result.Errors > 0 {
				accountsWithErrors++
			}
			results[ar.index] = ar.result
			completed[ar.index] = true
		}

		var wg sync.WaitGroup
		for i, account := range accounts {
			wg.Add(1)
			go func(idx int, acc config.IMAPAccount) {
				defer wg.Done()

				sem <- struct{}{}        // acquire
				defer func() { <-sem }() // release

				fmt.Fprintf(os.Stderr, "Processing account %d/%d: %s\n", idx+1, len(accounts), acc.Name)
				slog.Info("Processing account", "name", acc.Name, "username", acc.Username)

				result, err := exportAccountFn(chromeCtx, acc, writer, year, month)
				result.AccountName = acc.Name
				resultCh <- accountResult{index: idx, result: result, err: err}
			}(i, account)
		}

		// Close channel once all goroutines finish
		go func() {
			wg.Wait()
			close(resultCh)
		}()

		received := 0
		for received < len(accounts) {
			select {
			case ar, ok := <-resultCh:
				if !ok {
					received = len(accounts)
					continue
				}
				recordResult(ar)
				received++
			case <-ctx.Done():
				for {
					select {
					case ar, ok := <-resultCh:
						if !ok {
							return completedResults(results, completed), ctx.Err()
						}
						recordResult(ar)
					default:
						return completedResults(results, completed), ctx.Err()
					}
				}
			}
		}
	}

	if accountsWithErrors > 0 {
		return results, fmt.Errorf("export completed with errors in %d of %d account(s)", accountsWithErrors, len(accounts))
	}

	return results, nil
}

func completedResults(results []ExportResult, completed []bool) []ExportResult {
	finalResults := make([]ExportResult, 0, len(results))
	for i, result := range results {
		if completed[i] {
			finalResults = append(finalResults, result)
		}
	}
	return finalResults
}

func exportAccount(ctx context.Context, account config.IMAPAccount, writer output.FileWriter, year, month int) (ExportResult, error) {
	client := NewClient(account)
	messages, fetchErr := client.FetchMessages(ctx, year, month)

	slog.Info("Emails found", "count", len(messages), "account", account.Name)
	result := processMessages(messages, writer, year, month, ctx)

	if fetchErr != nil {
		result.Errors++
		return result, fmt.Errorf("fetching messages: %w", fetchErr)
	}

	return result, nil
}

// processMessages handles the core logic of processing fetched messages:
// writing attachments, detecting password-protected PDFs, and converting HTML bodies.
// The ctx is used for HTML→PDF conversion; pass nil if no HTML conversion is needed.
func processMessages(messages []Message, writer output.FileWriter, year, month int, ctx context.Context) ExportResult {
	var result ExportResult
	result.TotalEmails = len(messages)

	for _, msg := range messages {
		provider := resolver.Resolve(msg.From)
		slog.Debug("Processing email", "subject", msg.Subject, "from", msg.From, "provider", provider)

		if len(msg.Attachments) > 0 {
			logger.Trace("Email has attachments", "count", len(msg.Attachments), "subject", msg.Subject)

			for _, att := range msg.Attachments {
				path, err := writer.WriteAttachment(year, month, provider, msg.Date, att.Filename, att.Data)
				if err != nil {
					slog.Warn("Failed to write attachment", "filename", att.Filename, "error", err)
					result.Errors++
					continue
				}
				result.TotalAttachments++
				slog.Info("Saved attachment", "path", path)

				// Check for password-protected PDFs
				if pdfutil.IsPDF(att.Data) && pdfutil.IsPasswordProtected(att.Data) {
					hint := ExtractPasswordHint(msg.TextBody)
					savedFilename := filepath.Base(path)
					slog.Info("Password-protected PDF detected", "file", savedFilename, "provider", provider)
					if err := writer.WritePasswordHint(year, month, provider, savedFilename, hint, msg.Subject, msg.Date); err != nil {
						slog.Warn("Failed to write password hint", "error", err)
						result.Errors++
					}
					result.PasswordLocked++
				}
			}
		} else if ctx != nil && msg.HTMLBody != "" && looksLikeInvoice(msg.Subject, msg.HTMLBody) {
			slog.Debug("Converting HTML body to PDF", "subject", msg.Subject, "provider", provider)

			pdfData, err := HTMLToPDF(ctx, msg.HTMLBody)
			if err != nil {
				slog.Warn("HTML to PDF conversion failed", "subject", msg.Subject, "error", err)
				result.Errors++
				continue
			}

			path, err := writer.WriteAttachment(year, month, provider, msg.Date, "email_body.pdf", pdfData)
			if err != nil {
				slog.Warn("Failed to write body PDF", "error", err)
				result.Errors++
				continue
			}
			result.TotalBodyPDFs++
			slog.Info("Saved body as PDF", "path", path)
		}
	}

	return result
}

// looksLikeInvoice checks if the email subject or body suggests it's an invoice/receipt.
func looksLikeInvoice(subject, htmlBody string) bool {
	keywords := []string{
		"invoice", "receipt", "payment", "bill", "statement",
		"order confirmation", "purchase", "transaction",
		"subscription", "charge", "billing",
	}

	combined := strings.ToLower(subject + " " + htmlBody)
	for _, kw := range keywords {
		if strings.Contains(combined, kw) {
			return true
		}
	}
	return false
}
