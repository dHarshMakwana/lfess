package cmd

import (
	"time"

	"github.com/dHarshMakwana/lfess/internal/model"
	"github.com/dHarshMakwana/lfess/internal/store"
	"github.com/spf13/cobra"
)

func newUpdateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update <id> <content>",
		Short: "Update an existing item",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			deviceID, err := store.EnsureDeviceID(dataDir)
			if err != nil {
				return err
			}
			log, err := store.NewOpsLog(dataDir)
			if err != nil {
				return err
			}

			op, err := model.NewUpdateOperation(deviceID, args[0], args[1], time.Now().UTC())
			if err != nil {
				return err
			}
			return log.Append(op)
		},
	}
	return cmd
}
