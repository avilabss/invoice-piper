package email

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/avilabss/invoice-piper/internal/config"
	"github.com/avilabss/invoice-piper/internal/logger"
	"github.com/avilabss/invoice-piper/internal/output"
	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

func requireBrowserTests(t *testing.T) {
	t.Helper()
	if os.Getenv("INVP_RUN_BROWSER_TESTS") != "1" {
		t.Skip("browser-dependent test; set INVP_RUN_BROWSER_TESTS=1 to run")
	}
}

func TestLooksLikeInvoice(t *testing.T) {
	tests := []struct {
		name     string
		subject  string
		htmlBody string
		want     bool
	}{
		// Positive cases
		{"invoice in subject", "Your Invoice #123", "<html>body</html>", true},
		{"receipt in subject", "Payment Receipt", "<html>body</html>", true},
		{"payment in subject", "Payment Confirmation", "<html>body</html>", true},
		{"bill in subject", "Your Monthly Bill", "<html>body</html>", true},
		{"statement in subject", "Account Statement", "<html>body</html>", true},
		{"order confirmation in subject", "Order Confirmation #456", "<html>body</html>", true},
		{"purchase in subject", "Purchase Summary", "<html>body</html>", true},
		{"transaction in subject", "Transaction Details", "<html>body</html>", true},
		{"subscription in subject", "Subscription Renewed", "<html>body</html>", true},
		{"charge in subject", "New Charge on Your Card", "<html>body</html>", true},
		{"billing in subject", "Billing Update", "<html>body</html>", true},

		// Keyword in body
		{"invoice in body only", "Hello", "<html>Your invoice is attached</html>", true},
		{"receipt in body only", "Thanks", "<html>Here is your receipt</html>", true},

		// Case insensitive
		{"uppercase INVOICE", "YOUR INVOICE", "<html>body</html>", true},
		{"mixed case Receipt", "Your Receipt", "<html>body</html>", true},

		// Negative cases
		{"newsletter", "Weekly Newsletter", "<html>Check out our latest posts</html>", false},
		{"promotion", "50% Off Sale!", "<html>Shop now!</html>", false},
		{"generic email", "Hello there", "<html>How are you?</html>", false},
		{"empty strings", "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := looksLikeInvoice(tt.subject, tt.htmlBody)
			if got != tt.want {
				t.Errorf("looksLikeInvoice(%q, %q) = %v, want %v", tt.subject, tt.htmlBody, got, tt.want)
			}
		})
	}
}

// mockWriter implements output.FileWriter for testing.
type mockWriter struct {
	attachments   []writtenAttachment
	passwordHints []writtenHint
	writeErr      error // if set, WriteAttachment returns this error
	hintErr       error // if set, WritePasswordHint returns this error
}

type writtenAttachment struct {
	year, month int
	provider    string
	filename    string
	data        []byte
}

type writtenHint struct {
	provider    string
	pdfFilename string
	hint        string
}

func (m *mockWriter) WriteAttachment(year, month int, provider string, emailDate time.Time, originalFilename string, data []byte) (string, error) {
	if m.writeErr != nil {
		return "", m.writeErr
	}
	m.attachments = append(m.attachments, writtenAttachment{
		year:     year,
		month:    month,
		provider: provider,
		filename: originalFilename,
		data:     data,
	})
	return fmt.Sprintf("/out/%d/%02d/%s/%s", year, month, provider, originalFilename), nil
}

func (m *mockWriter) WritePasswordHint(year, month int, provider string, pdfFilename string, hint string, emailSubject string, emailDate time.Time) error {
	if m.hintErr != nil {
		return m.hintErr
	}
	m.passwordHints = append(m.passwordHints, writtenHint{
		provider:    provider,
		pdfFilename: pdfFilename,
		hint:        hint,
	})
	return nil
}

func TestProcessMessages_Attachments(t *testing.T) {
	writer := &mockWriter{}
	messages := []Message{
		{
			Subject: "Your Invoice",
			From:    "noreply@uber.com",
			Date:    time.Date(2025, 1, 15, 14, 30, 0, 0, time.UTC),
			Attachments: []Attachment{
				{Filename: "invoice.pdf", ContentType: "application/pdf", Data: []byte("fake pdf")},
			},
		},
		{
			Subject: "Receipt",
			From:    "noreply@amazon.in",
			Date:    time.Date(2025, 1, 20, 10, 0, 0, 0, time.UTC),
			Attachments: []Attachment{
				{Filename: "receipt.pdf", ContentType: "application/pdf", Data: []byte("fake pdf 2")},
				{Filename: "details.csv", ContentType: "text/csv", Data: []byte("a,b,c")},
			},
		},
	}

	result := processMessages(messages, writer, 2025, 1, nil)

	if result.TotalEmails != 2 {
		t.Errorf("TotalEmails = %d, want 2", result.TotalEmails)
	}
	if result.TotalAttachments != 3 {
		t.Errorf("TotalAttachments = %d, want 3", result.TotalAttachments)
	}
	if result.Errors != 0 {
		t.Errorf("Errors = %d, want 0", result.Errors)
	}
	if len(writer.attachments) != 3 {
		t.Errorf("written attachments = %d, want 3", len(writer.attachments))
	}
}

func TestProcessMessages_WriteError(t *testing.T) {
	writer := &mockWriter{writeErr: fmt.Errorf("disk full")}
	messages := []Message{
		{
			Subject: "Invoice",
			From:    "noreply@uber.com",
			Date:    time.Now(),
			Attachments: []Attachment{
				{Filename: "invoice.pdf", Data: []byte("data")},
			},
		},
	}

	result := processMessages(messages, writer, 2025, 1, nil)

	if result.Errors != 1 {
		t.Errorf("Errors = %d, want 1", result.Errors)
	}
	if result.TotalAttachments != 0 {
		t.Errorf("TotalAttachments = %d, want 0", result.TotalAttachments)
	}
}

func TestProcessMessages_NoAttachmentsNoInvoice(t *testing.T) {
	writer := &mockWriter{}
	messages := []Message{
		{
			Subject:  "Weekly Newsletter",
			From:     "news@company.com",
			Date:     time.Now(),
			HTMLBody: "<html>No invoice keywords here</html>",
		},
	}

	result := processMessages(messages, writer, 2025, 1, nil)

	if result.TotalEmails != 1 {
		t.Errorf("TotalEmails = %d, want 1", result.TotalEmails)
	}
	if result.TotalAttachments != 0 {
		t.Errorf("TotalAttachments = %d, want 0", result.TotalAttachments)
	}
	if result.TotalBodyPDFs != 0 {
		t.Errorf("TotalBodyPDFs = %d, want 0", result.TotalBodyPDFs)
	}
	if len(writer.attachments) != 0 {
		t.Errorf("should not have written any files")
	}
}

// createMinimalPDF generates a minimal valid (unencrypted) PDF in memory.
func createMinimalPDF(t *testing.T) []byte {
	t.Helper()
	conf := model.NewDefaultConfiguration()
	ctx, err := pdfcpu.CreateContextWithXRefTable(conf, types.PaperSize["A4"])
	if err != nil {
		t.Fatalf("creating PDF context: %v", err)
	}
	var buf bytes.Buffer
	if err := api.WriteContext(ctx, &buf); err != nil {
		t.Fatalf("writing PDF: %v", err)
	}
	return buf.Bytes()
}

// createEncryptedPDF generates a minimal password-protected PDF in memory.
func createEncryptedPDF(t *testing.T, password string) []byte {
	t.Helper()
	plainPDF := createMinimalPDF(t)

	encConf := model.NewAESConfiguration(password, password, 256)
	reader := bytes.NewReader(plainPDF)
	var out bytes.Buffer
	if err := api.Encrypt(reader, &out, encConf); err != nil {
		t.Fatalf("encrypting PDF: %v", err)
	}
	return out.Bytes()
}

func TestProcessMessages_PasswordProtectedPDF(t *testing.T) {
	encryptedPDF := createEncryptedPDF(t, "secret123")

	writer := &mockWriter{}
	messages := []Message{
		{
			Subject:  "Your credit card statement",
			From:     "noreply@hdfcbank.com",
			Date:     time.Date(2025, 3, 10, 8, 0, 0, 0, time.UTC),
			TextBody: "Your statement is attached. The password to open the PDF is your date of birth in DDMMYYYY format.",
			Attachments: []Attachment{
				{Filename: "statement.pdf", ContentType: "application/pdf", Data: encryptedPDF},
			},
		},
	}

	result := processMessages(messages, writer, 2025, 3, nil)

	if result.TotalAttachments != 1 {
		t.Errorf("TotalAttachments = %d, want 1", result.TotalAttachments)
	}
	if result.PasswordLocked != 1 {
		t.Errorf("PasswordLocked = %d, want 1", result.PasswordLocked)
	}
	if len(writer.passwordHints) != 1 {
		t.Fatalf("password hints written = %d, want 1", len(writer.passwordHints))
	}
	if writer.passwordHints[0].provider != "hdfcbank" {
		t.Errorf("hint provider = %q, want %q", writer.passwordHints[0].provider, "hdfcbank")
	}
}

func TestProcessMessages_PasswordHintWriteFailureCountsAsError(t *testing.T) {
	encryptedPDF := createEncryptedPDF(t, "secret123")

	writer := &mockWriter{hintErr: errors.New("read-only filesystem")}
	messages := []Message{
		{
			Subject:  "Your credit card statement",
			From:     "noreply@hdfcbank.com",
			Date:     time.Date(2025, 3, 10, 8, 0, 0, 0, time.UTC),
			TextBody: "Your statement is attached. The password to open the PDF is your date of birth in DDMMYYYY format.",
			Attachments: []Attachment{
				{Filename: "statement.pdf", ContentType: "application/pdf", Data: encryptedPDF},
			},
		},
	}

	result := processMessages(messages, writer, 2025, 3, nil)

	if result.TotalAttachments != 1 {
		t.Errorf("TotalAttachments = %d, want 1", result.TotalAttachments)
	}
	if result.PasswordLocked != 1 {
		t.Errorf("PasswordLocked = %d, want 1", result.PasswordLocked)
	}
	if result.Errors != 1 {
		t.Errorf("Errors = %d, want 1", result.Errors)
	}
	if len(writer.passwordHints) != 0 {
		t.Fatalf("password hints written = %d, want 0", len(writer.passwordHints))
	}
}

func TestProcessMessages_DoesNotLogPasswordHintContents(t *testing.T) {
	encryptedPDF := createEncryptedPDF(t, "secret123")
	const sensitiveToken = "ULTRA-SENSITIVE-12345"

	var logBuf bytes.Buffer
	prevLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: logger.LevelTrace})))
	t.Cleanup(func() {
		slog.SetDefault(prevLogger)
	})

	writer := &mockWriter{}
	messages := []Message{
		{
			Subject:  "Your statement",
			From:     "noreply@hdfcbank.com",
			Date:     time.Date(2025, 3, 10, 8, 0, 0, 0, time.UTC),
			TextBody: "Password is " + sensitiveToken,
			Attachments: []Attachment{
				{Filename: "statement.pdf", ContentType: "application/pdf", Data: encryptedPDF},
			},
		},
	}

	result := processMessages(messages, writer, 2025, 3, nil)

	if result.PasswordLocked != 1 {
		t.Fatalf("PasswordLocked = %d, want 1", result.PasswordLocked)
	}
	if len(writer.passwordHints) != 1 {
		t.Fatalf("password hints written = %d, want 1", len(writer.passwordHints))
	}
	if !strings.Contains(writer.passwordHints[0].hint, sensitiveToken) {
		t.Fatalf("written password hint = %q, want to contain %q", writer.passwordHints[0].hint, sensitiveToken)
	}
	if strings.Contains(logBuf.String(), sensitiveToken) {
		t.Fatalf("sensitive password hint leaked in logs: %s", logBuf.String())
	}
}

func TestProcessMessages_UnencryptedPDF_NoPasswordHint(t *testing.T) {
	plainPDF := createMinimalPDF(t)

	writer := &mockWriter{}
	messages := []Message{
		{
			Subject: "Your invoice",
			From:    "noreply@uber.com",
			Date:    time.Date(2025, 3, 10, 8, 0, 0, 0, time.UTC),
			Attachments: []Attachment{
				{Filename: "invoice.pdf", ContentType: "application/pdf", Data: plainPDF},
			},
		},
	}

	result := processMessages(messages, writer, 2025, 3, nil)

	if result.TotalAttachments != 1 {
		t.Errorf("TotalAttachments = %d, want 1", result.TotalAttachments)
	}
	if result.PasswordLocked != 0 {
		t.Errorf("PasswordLocked = %d, want 0 for unencrypted PDF", result.PasswordLocked)
	}
	if len(writer.passwordHints) != 0 {
		t.Errorf("should not have written password hints for unencrypted PDF")
	}
}

func TestProcessMessages_HTMLBodyInvoice(t *testing.T) {
	requireBrowserTests(t)

	writer := &mockWriter{}
	messages := []Message{
		{
			Subject:  "Your Uber Receipt",
			From:     "noreply@uber.com",
			Date:     time.Date(2025, 2, 5, 18, 30, 0, 0, time.UTC),
			HTMLBody: "<html><body><h1>Receipt</h1><p>Total: $25.00</p></body></html>",
			// No attachments — should trigger HTML→PDF conversion
		},
	}

	// Use a real Chrome allocator context for the conversion
	chromeCtx, cancel := NewChromeAllocator(context.Background())
	defer cancel()

	result := processMessages(messages, writer, 2025, 2, chromeCtx)

	if result.TotalEmails != 1 {
		t.Errorf("TotalEmails = %d, want 1", result.TotalEmails)
	}
	if result.TotalBodyPDFs != 1 {
		t.Errorf("TotalBodyPDFs = %d, want 1", result.TotalBodyPDFs)
	}
	if len(writer.attachments) != 1 {
		t.Fatalf("written attachments = %d, want 1", len(writer.attachments))
	}
	att := writer.attachments[0]
	if att.filename != "email_body.pdf" {
		t.Errorf("filename = %q, want %q", att.filename, "email_body.pdf")
	}
	if att.provider != "uber" {
		t.Errorf("provider = %q, want %q", att.provider, "uber")
	}
	if len(att.data) == 0 {
		t.Error("converted PDF data should not be empty")
	}
}

func TestProcessMessages_HTMLBodyNotInvoice_Skipped(t *testing.T) {
	requireBrowserTests(t)

	writer := &mockWriter{}
	messages := []Message{
		{
			Subject:  "Weekly Newsletter",
			From:     "news@company.com",
			Date:     time.Now(),
			HTMLBody: "<html>Check out our latest blog posts</html>",
		},
	}

	chromeCtx, cancel := NewChromeAllocator(context.Background())
	defer cancel()

	result := processMessages(messages, writer, 2025, 1, chromeCtx)

	if result.TotalBodyPDFs != 0 {
		t.Errorf("TotalBodyPDFs = %d, want 0 for non-invoice email", result.TotalBodyPDFs)
	}
	if len(writer.attachments) != 0 {
		t.Error("should not have written any files for non-invoice HTML")
	}
}

func TestExport_ReturnsErrorWhenAnyAccountHasProcessingErrors(t *testing.T) {
	origExportAccountFn := exportAccountFn
	t.Cleanup(func() {
		exportAccountFn = origExportAccountFn
	})

	exportAccountFn = func(ctx context.Context, account config.IMAPAccount, writer output.FileWriter, year, month int) (ExportResult, error) {
		if account.Name == "with-errors" {
			return ExportResult{TotalEmails: 2, TotalAttachments: 1, Errors: 2}, nil
		}
		return ExportResult{TotalEmails: 1, TotalAttachments: 1}, nil
	}

	cfg := &config.Config{
		OutputDir: t.TempDir(),
		Email: config.EmailConfig{Accounts: []config.IMAPAccount{
			{Name: "ok"},
			{Name: "with-errors"},
		}},
	}

	results, err := Export(context.Background(), cfg, 2025, 1, 1)
	if err == nil {
		t.Fatal("expected export error when any account has processing errors")
	}
	if len(results) != 2 {
		t.Fatalf("results length = %d, want 2", len(results))
	}
	if results[1].Errors != 2 {
		t.Fatalf("account with-errors errors = %d, want 2", results[1].Errors)
	}
}

func TestExport_ReturnsResultsEvenWhenAccountReturnsError(t *testing.T) {
	origExportAccountFn := exportAccountFn
	t.Cleanup(func() {
		exportAccountFn = origExportAccountFn
	})

	exportAccountFn = func(ctx context.Context, account config.IMAPAccount, writer output.FileWriter, year, month int) (ExportResult, error) {
		return ExportResult{TotalEmails: 3, TotalAttachments: 2, Errors: 1}, fmt.Errorf("partial fetch failure")
	}

	cfg := &config.Config{
		OutputDir: t.TempDir(),
		Email: config.EmailConfig{Accounts: []config.IMAPAccount{
			{Name: "account-1"},
		}},
	}

	results, err := Export(context.Background(), cfg, 2025, 1, 1)
	if err == nil {
		t.Fatal("expected export error")
	}
	if len(results) != 1 {
		t.Fatalf("results length = %d, want 1", len(results))
	}
	if results[0].AccountName != "account-1" {
		t.Fatalf("account name = %q, want %q", results[0].AccountName, "account-1")
	}
	if results[0].TotalEmails != 3 || results[0].TotalAttachments != 2 {
		t.Fatalf("unexpected result summary: %+v", results[0])
	}
}

func TestExport_ReturnsPartialResultsPromptlyWhenCanceled(t *testing.T) {
	origExportAccountFn := exportAccountFn
	t.Cleanup(func() {
		exportAccountFn = origExportAccountFn
	})

	fastDone := make(chan struct{})
	releaseSlow := make(chan struct{})
	slowDone := make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-releaseSlow:
		default:
			close(releaseSlow)
		}
	})

	exportAccountFn = func(ctx context.Context, account config.IMAPAccount, writer output.FileWriter, year, month int) (ExportResult, error) {
		switch account.Name {
		case "fast":
			close(fastDone)
			return ExportResult{TotalEmails: 1, TotalAttachments: 1}, nil
		case "slow":
			<-releaseSlow
			close(slowDone)
			return ExportResult{TotalEmails: 2, TotalAttachments: 2}, nil
		default:
			return ExportResult{}, nil
		}
	}

	cfg := &config.Config{
		OutputDir: t.TempDir(),
		Email: config.EmailConfig{Accounts: []config.IMAPAccount{
			{Name: "fast"},
			{Name: "slow"},
		}},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	type exportOutcome struct {
		results []ExportResult
		err     error
	}
	outcomeCh := make(chan exportOutcome, 1)
	go func() {
		results, err := Export(ctx, cfg, 2025, 1, 2)
		outcomeCh <- exportOutcome{results: results, err: err}
	}()

	select {
	case <-fastDone:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("fast account did not finish")
	}

	cancel()

	var outcome exportOutcome
	select {
	case outcome = <-outcomeCh:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("export did not return promptly after cancellation")
	}

	if !errors.Is(outcome.err, context.Canceled) {
		t.Fatalf("expected context canceled error, got %v", outcome.err)
	}
	if len(outcome.results) != 1 {
		t.Fatalf("partial results length = %d, want 1", len(outcome.results))
	}
	if outcome.results[0].AccountName != "fast" {
		t.Fatalf("partial result account = %q, want %q", outcome.results[0].AccountName, "fast")
	}

	select {
	case <-releaseSlow:
	default:
		close(releaseSlow)
	}
	select {
	case <-slowDone:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("slow worker did not exit after unblocking")
	}
}
