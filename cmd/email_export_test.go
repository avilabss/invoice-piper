package cmd

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/avilabss/invoice-piper/internal/config"
	"github.com/avilabss/invoice-piper/internal/email"
)

func TestEmailExportRunE_PrintsSummaryOnError(t *testing.T) {
	origRunEmailExport := runEmailExport
	origCfg := cfg
	origTimeout := emailExportWorkflowTimeout
	origDrainGrace := workflowTimeoutDrainGrace
	t.Cleanup(func() {
		runEmailExport = origRunEmailExport
		cfg = origCfg
		emailExportWorkflowTimeout = origTimeout
		workflowTimeoutDrainGrace = origDrainGrace
	})

	cfg = &config.Config{}
	exportErr := errors.New("partial export failure")
	runEmailExport = func(ctx context.Context, cfg *config.Config, year, month, concurrency int) ([]email.ExportResult, error) {
		return []email.ExportResult{
			{
				AccountName:      "primary",
				TotalEmails:      2,
				TotalAttachments: 1,
				Errors:           1,
			},
		}, exportErr
	}

	var runErr error
	stdout := captureStdout(t, func() {
		runErr = emailExportCmd.RunE(emailExportCmd, nil)
	})

	if !errors.Is(runErr, exportErr) {
		t.Fatalf("expected export error %v, got %v", exportErr, runErr)
	}
	if !strings.Contains(stdout, "--- Summary ---") {
		t.Fatalf("expected summary header in stdout, got: %q", stdout)
	}
	if !strings.Contains(stdout, "Account: primary") {
		t.Fatalf("expected account summary in stdout, got: %q", stdout)
	}
}

func TestEmailExportRunE_PassesWorkflowTimeoutContext(t *testing.T) {
	origRunEmailExport := runEmailExport
	origCfg := cfg
	origTimeout := emailExportWorkflowTimeout
	t.Cleanup(func() {
		runEmailExport = origRunEmailExport
		cfg = origCfg
		emailExportWorkflowTimeout = origTimeout
	})

	cfg = &config.Config{}
	emailExportWorkflowTimeout = 2 * time.Second

	var (
		sawDeadline bool
		remaining   time.Duration
	)

	runEmailExport = func(ctx context.Context, cfg *config.Config, year, month, concurrency int) ([]email.ExportResult, error) {
		deadline, ok := ctx.Deadline()
		sawDeadline = ok
		if ok {
			remaining = time.Until(deadline)
		}

		return nil, nil
	}

	if err := emailExportCmd.RunE(emailExportCmd, nil); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !sawDeadline {
		t.Fatal("expected workflow context deadline")
	}
	if remaining <= 0 {
		t.Fatalf("expected positive remaining timeout, got %s", remaining)
	}
	if remaining > emailExportWorkflowTimeout {
		t.Fatalf("remaining timeout %s exceeds configured timeout %s", remaining, emailExportWorkflowTimeout)
	}
}

func TestEmailExportRunE_ReturnsTimeoutErrorWhenWorkflowDeadlineExpires(t *testing.T) {
	origRunEmailExport := runEmailExport
	origCfg := cfg
	origTimeout := emailExportWorkflowTimeout
	origDrainGrace := workflowTimeoutDrainGrace
	t.Cleanup(func() {
		runEmailExport = origRunEmailExport
		cfg = origCfg
		emailExportWorkflowTimeout = origTimeout
		workflowTimeoutDrainGrace = origDrainGrace
	})

	cfg = &config.Config{}
	emailExportWorkflowTimeout = 15 * time.Millisecond
	workflowTimeoutDrainGrace = 10 * time.Millisecond

	runEmailExport = func(ctx context.Context, cfg *config.Config, year, month, concurrency int) ([]email.ExportResult, error) {
		<-ctx.Done()
		return []email.ExportResult{{
			AccountName:      "primary",
			TotalEmails:      2,
			TotalAttachments: 1,
		}}, errors.New("export completed with errors in 1 of 1 account(s)")
	}

	var runErr error
	stdout := captureStdout(t, func() {
		runErr = emailExportCmd.RunE(emailExportCmd, nil)
	})

	if runErr == nil {
		t.Fatal("expected timeout error")
	}
	if !errors.Is(runErr, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded error, got %v", runErr)
	}
	if !strings.Contains(runErr.Error(), "email export timed out after") {
		t.Fatalf("expected actionable timeout error, got %v", runErr)
	}
	if !strings.Contains(stdout, "Account: primary") {
		t.Fatalf("expected partial summary output, got: %q", stdout)
	}
}

func TestEmailExportRunE_DoesNotHangWhenExporterStalls(t *testing.T) {
	origRunEmailExport := runEmailExport
	origCfg := cfg
	origTimeout := emailExportWorkflowTimeout
	origDrainGrace := workflowTimeoutDrainGrace
	t.Cleanup(func() {
		runEmailExport = origRunEmailExport
		cfg = origCfg
		emailExportWorkflowTimeout = origTimeout
		workflowTimeoutDrainGrace = origDrainGrace
	})

	cfg = &config.Config{}
	emailExportWorkflowTimeout = 20 * time.Millisecond
	workflowTimeoutDrainGrace = 10 * time.Millisecond

	block := make(chan struct{})
	finished := make(chan struct{})
	runEmailExport = func(ctx context.Context, cfg *config.Config, year, month, concurrency int) ([]email.ExportResult, error) {
		defer close(finished)
		<-block
		return nil, nil
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- emailExportCmd.RunE(emailExportCmd, nil)
	}()

	select {
	case err := <-errCh:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected deadline exceeded error, got %v", err)
		}
		if !strings.Contains(err.Error(), "email export timed out after") {
			t.Fatalf("expected actionable timeout error, got %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("email export command hung instead of timing out")
	}

	close(block)
	select {
	case <-finished:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("stalled exporter did not exit after unblocking")
	}
}
