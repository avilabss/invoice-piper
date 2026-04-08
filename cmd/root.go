package cmd

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/avilabss/invoice-piper/internal/config"
	"github.com/avilabss/invoice-piper/internal/logger"
	"github.com/spf13/cobra"
)

var (
	cfgFile string
	verbose int
	cfg     *config.Config
)

var rootCmd = &cobra.Command{
	Use:   "invp",
	Short: "Invoice Piper - centralised invoice and receipt collection",
	Long:  "Invoice Piper collects invoices, payment receipts, and statements from various sources and organises them for accounting.",
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func initLogger() {
	logger.SetVerbosity(verbose)
	logger.Trace("startup", "verbosity", verbose)
}

func loadConfig(cmd *cobra.Command, args []string) error {
	slog.Debug("Loading config", "path", cfgFile)
	var err error
	cfg, err = config.Load(cfgFile)
	if err != nil {
		return err
	}
	slog.Debug("Config loaded", "output_dir", cfg.OutputDir, "accounts", len(cfg.Email.Accounts))
	return nil
}

func init() {
	cobra.OnInitialize(initLogger)
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", fmt.Sprintf("config file path (default: %s)", config.DefaultConfigSearchPathHint()))
	rootCmd.PersistentFlags().CountVarP(&verbose, "verbose", "v", "increase verbosity (-v, -vv, -vvv)")
}
