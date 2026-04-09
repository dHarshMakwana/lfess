package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	dataDir string
)

func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "lfess",
		Short: "LFESS - Local-First Encrypted Sync System (MVP)",
	}

	root.PersistentFlags().StringVar(&dataDir, "data-dir", defaultDataDir(), "data directory")

	root.AddCommand(newAddCmd())
	root.AddCommand(newUpdateCmd())
	root.AddCommand(newDeleteCmd())
	root.AddCommand(newListCmd())
	root.AddCommand(newPairCmd())
	root.AddCommand(newSyncCmd())
	root.AddCommand(newDiscoverCmd())
	root.AddCommand(newServeCmd())

	return root
}

func Execute() {
	if err := NewRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func defaultDataDir() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return ".lfess"
	}
	return h + string(os.PathSeparator) + ".lfess"
}
