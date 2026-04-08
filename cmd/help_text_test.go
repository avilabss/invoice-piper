package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func captureHelp(t *testing.T, command *cobra.Command) string {
	t.Helper()

	oldOut := command.OutOrStdout()
	oldErr := command.ErrOrStderr()
	t.Cleanup(func() {
		command.SetOut(oldOut)
		command.SetErr(oldErr)
	})

	var buf bytes.Buffer
	command.SetOut(&buf)
	command.SetErr(&buf)

	if err := command.Help(); err != nil {
		t.Fatalf("rendering help: %v", err)
	}

	return buf.String()
}

func TestEmailExportHelp_IncludesExamplesAndConcurrencyFallback(t *testing.T) {
	help := captureHelp(t, emailExportCmd)

	for _, want := range []string{
		"Examples:",
		"invp email export --year 2025 --month 1",
		"<=0 uses default",
	} {
		if !strings.Contains(help, want) {
			t.Fatalf("help output missing %q:\n%s", want, help)
		}
	}
}

func TestEmailMailboxesHelp_IncludesExamples(t *testing.T) {
	help := captureHelp(t, emailMailboxesCmd)

	for _, want := range []string{
		"Examples:",
		"invp email mailboxes --config ./config.json",
	} {
		if !strings.Contains(help, want) {
			t.Fatalf("help output missing %q:\n%s", want, help)
		}
	}
}
