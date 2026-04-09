package cmd

import (
	"fmt"

	"github.com/dHarshMakwana/lfess/internal/peersync"
	"github.com/dHarshMakwana/lfess/internal/store"
	"github.com/spf13/cobra"
)

func newSyncCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sync <peer-addr>",
		Short: "Pull operations from a peer over HTTP",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			log, err := store.NewOpsLog(dataDir)
			if err != nil {
				return err
			}

			client := peersync.New(nil)
			imported, err := client.PullAndImport(cmd.Context(), args[0], log)
			if err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "imported %d operations\n", imported)
			return nil
		},
	}

	return cmd
}
