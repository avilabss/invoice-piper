package cmd

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/avilabss/invoice-piper/internal/config"
	"github.com/spf13/cobra"
)

type stubMailboxClient struct {
	mailboxes []string
	err       error
	listFn    func(ctx context.Context) ([]string, error)
}

func (s stubMailboxClient) ListMailboxes(ctx context.Context) ([]string, error) {
	if s.listFn != nil {
		return s.listFn(ctx)
	}
	return s.mailboxes, s.err
}

func TestEmailMailboxesRunE_ReturnsErrorOnPartialFailure(t *testing.T) {
	origCfg := cfg
	origNewMailboxClient := newMailboxClient
	t.Cleanup(func() {
		cfg = origCfg
		newMailboxClient = origNewMailboxClient
	})

	cfg = &config.Config{
		Email: config.EmailConfig{Accounts: []config.IMAPAccount{
			{Name: "ok", Username: "ok@example.com"},
			{Name: "bad", Username: "bad@example.com"},
		}},
	}

	newMailboxClient = func(account config.IMAPAccount) mailboxLister {
		if account.Name == "bad" {
			return stubMailboxClient{mailboxes: []string{"Recovered"}, err: errors.New("imap unavailable")}
		}
		return stubMailboxClient{mailboxes: []string{"INBOX"}}
	}

	var runErr error
	stdout := captureStdout(t, func() {
		runErr = emailMailboxesCmd.RunE(emailMailboxesCmd, nil)
	})

	if runErr == nil {
		t.Fatal("expected error when one account fails")
	}
	if !strings.Contains(runErr.Error(), "1 of 2") {
		t.Fatalf("unexpected error: %v", runErr)
	}
	if !strings.Contains(stdout, "Account: ok") || !strings.Contains(stdout, "- INBOX") {
		t.Fatalf("expected successful mailbox output, got: %q", stdout)
	}
	if !strings.Contains(stdout, "Account: bad") || !strings.Contains(stdout, "- Recovered") {
		t.Fatalf("expected partial mailbox output for failing account, got: %q", stdout)
	}
	if !strings.Contains(stdout, "error: imap unavailable") {
		t.Fatalf("expected account error output, got: %q", stdout)
	}
}

func TestEmailMailboxesRunE_SucceedsWhenAllAccountsSucceed(t *testing.T) {
	origCfg := cfg
	origNewMailboxClient := newMailboxClient
	t.Cleanup(func() {
		cfg = origCfg
		newMailboxClient = origNewMailboxClient
	})

	cfg = &config.Config{
		Email: config.EmailConfig{Accounts: []config.IMAPAccount{
			{Name: "ok", Username: "ok@example.com"},
		}},
	}

	newMailboxClient = func(account config.IMAPAccount) mailboxLister {
		return stubMailboxClient{mailboxes: []string{"INBOX", "Archive"}}
	}

	runErr := emailMailboxesCmd.RunE(emailMailboxesCmd, nil)
	if runErr != nil {
		t.Fatalf("expected nil error, got %v", runErr)
	}
}

func TestEmailMailboxesRunE_PassesWorkflowTimeoutContext(t *testing.T) {
	origCfg := cfg
	origNewMailboxClient := newMailboxClient
	origTimeout := emailMailboxesWorkflowTimeout
	t.Cleanup(func() {
		cfg = origCfg
		newMailboxClient = origNewMailboxClient
		emailMailboxesWorkflowTimeout = origTimeout
	})

	cfg = &config.Config{
		Email: config.EmailConfig{Accounts: []config.IMAPAccount{{
			Name:     "ok",
			Username: "ok@example.com",
		}}},
	}
	emailMailboxesWorkflowTimeout = 2 * time.Second

	var (
		sawDeadline bool
		remaining   time.Duration
	)

	newMailboxClient = func(account config.IMAPAccount) mailboxLister {
		return stubMailboxClient{listFn: func(ctx context.Context) ([]string, error) {
			deadline, ok := ctx.Deadline()
			sawDeadline = ok
			if ok {
				remaining = time.Until(deadline)
			}
			return []string{"INBOX"}, nil
		}}
	}

	if err := emailMailboxesCmd.RunE(emailMailboxesCmd, nil); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !sawDeadline {
		t.Fatal("expected workflow context deadline")
	}
	if remaining <= 0 {
		t.Fatalf("expected positive remaining timeout, got %s", remaining)
	}
	if remaining > emailMailboxesWorkflowTimeout {
		t.Fatalf("remaining timeout %s exceeds configured timeout %s", remaining, emailMailboxesWorkflowTimeout)
	}
}

func TestEmailMailboxesRunE_ReturnsTimeoutErrorWhenWorkflowDeadlineExpires(t *testing.T) {
	origCfg := cfg
	origNewMailboxClient := newMailboxClient
	origTimeout := emailMailboxesWorkflowTimeout
	origDrainGrace := workflowTimeoutDrainGrace
	t.Cleanup(func() {
		cfg = origCfg
		newMailboxClient = origNewMailboxClient
		emailMailboxesWorkflowTimeout = origTimeout
		workflowTimeoutDrainGrace = origDrainGrace
	})

	cfg = &config.Config{
		Email: config.EmailConfig{Accounts: []config.IMAPAccount{{
			Name:     "slow",
			Username: "slow@example.com",
		}}},
	}
	emailMailboxesWorkflowTimeout = 15 * time.Millisecond
	workflowTimeoutDrainGrace = 10 * time.Millisecond

	newMailboxClient = func(account config.IMAPAccount) mailboxLister {
		return stubMailboxClient{listFn: func(ctx context.Context) ([]string, error) {
			<-ctx.Done()
			return []string{"Recovered"}, errors.New("listing stalled")
		}}
	}

	var runErr error
	stdout := captureStdout(t, func() {
		runErr = emailMailboxesCmd.RunE(emailMailboxesCmd, nil)
	})

	if runErr == nil {
		t.Fatal("expected timeout error")
	}
	if !errors.Is(runErr, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded error, got %v", runErr)
	}
	if !strings.Contains(runErr.Error(), "email mailboxes timed out after") {
		t.Fatalf("expected actionable timeout error, got %v", runErr)
	}
	if !strings.Contains(stdout, "- Recovered") {
		t.Fatalf("expected partial mailbox output, got: %q", stdout)
	}
}

func TestEmailMailboxesRunE_DoesNotHangWhenMailboxListingStalls(t *testing.T) {
	origCfg := cfg
	origNewMailboxClient := newMailboxClient
	origTimeout := emailMailboxesWorkflowTimeout
	origDrainGrace := workflowTimeoutDrainGrace
	t.Cleanup(func() {
		cfg = origCfg
		newMailboxClient = origNewMailboxClient
		emailMailboxesWorkflowTimeout = origTimeout
		workflowTimeoutDrainGrace = origDrainGrace
	})

	cfg = &config.Config{
		Email: config.EmailConfig{Accounts: []config.IMAPAccount{{
			Name:     "slow",
			Username: "slow@example.com",
		}}},
	}
	emailMailboxesWorkflowTimeout = 20 * time.Millisecond
	workflowTimeoutDrainGrace = 10 * time.Millisecond

	block := make(chan struct{})
	finished := make(chan struct{})
	newMailboxClient = func(account config.IMAPAccount) mailboxLister {
		return stubMailboxClient{listFn: func(ctx context.Context) ([]string, error) {
			defer close(finished)
			<-block
			return nil, nil
		}}
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- emailMailboxesCmd.RunE(emailMailboxesCmd, nil)
	}()

	select {
	case err := <-errCh:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected deadline exceeded error, got %v", err)
		}
		if !strings.Contains(err.Error(), "email mailboxes timed out after") {
			t.Fatalf("expected actionable timeout error, got %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("email mailboxes command hung instead of timing out")
	}

	close(block)
	select {
	case <-finished:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("stalled mailbox lister did not exit after unblocking")
	}
}

func TestEmailMailboxesRunE_PropagatesCancellationWithoutCountingFailure(t *testing.T) {
	origCfg := cfg
	origNewMailboxClient := newMailboxClient
	t.Cleanup(func() {
		cfg = origCfg
		newMailboxClient = origNewMailboxClient
	})

	cfg = &config.Config{
		Email: config.EmailConfig{Accounts: []config.IMAPAccount{{
			Name:     "cancelled",
			Username: "cancelled@example.com",
		}}},
	}

	started := make(chan struct{})
	newMailboxClient = func(account config.IMAPAccount) mailboxLister {
		return stubMailboxClient{listFn: func(ctx context.Context) ([]string, error) {
			close(started)
			<-ctx.Done()
			return []string{"Recovered"}, ctx.Err()
		}}
	}

	parentCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runCmd := &cobra.Command{}
	runCmd.SetContext(parentCtx)

	errCh := make(chan error, 1)
	go func() {
		errCh <- emailMailboxesCmd.RunE(runCmd, nil)
	}()

	select {
	case <-started:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("mailbox listing did not start")
	}

	cancel()

	select {
	case runErr := <-errCh:
		if !errors.Is(runErr, context.Canceled) {
			t.Fatalf("expected context canceled error, got %v", runErr)
		}
		if strings.Contains(runErr.Error(), "failed to list mailboxes for") {
			t.Fatalf("expected cancellation error, got generic failure: %v", runErr)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("email mailboxes command did not return after cancellation")
	}
}
