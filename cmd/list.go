package cmd

import (
	"fmt"
	"sort"

	"github.com/dHarshMakwana/lfess/internal/engine"
	"github.com/dHarshMakwana/lfess/internal/store"
	"github.com/spf13/cobra"
)

func newListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List non-deleted items",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			log, err := store.NewOpsLog(dataDir)
			if err != nil {
				return err
			}

			ops, err := log.ReadAll()
			if err != nil {
				return err
			}

			state := engine.Replay(ops)
			ids := make([]string, 0, len(state))
			for id, item := range state {
				if item.Deleted {
					continue
				}
				ids = append(ids, id)
			}
			sort.Strings(ids)

			for _, id := range ids {
				item := state[id]
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\n", item.ID, item.Content)
			}
			return nil
		},
	}

	return cmd
}
