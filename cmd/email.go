package cmd

import "github.com/spf13/cobra"

var emailCmd = &cobra.Command{
	Use:               "email",
	Short:             "Manage email-based invoice collection",
	PersistentPreRunE: loadConfig,
}

func init() {
	rootCmd.AddCommand(emailCmd)
}
