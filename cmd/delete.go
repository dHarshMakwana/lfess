package cmd

import (
    "time"

    "github.com/dHarshMakwana/lfess/internal/model"
    "github.com/dHarshMakwana/lfess/internal/store"
    "github.com/spf13/cobra"
)

func newDeleteCmd() *cobra.Command {
    cmd := &cobra.Command{
        Use:   "delete <id>",
        Short: "Delete an item",
        Args:  cobra.ExactArgs(1),
        RunE: func(cmd *cobra.Command, args []string) error {
            deviceID, err := store.EnsureDeviceID(dataDir)
            if err != nil {
                return err
            }
            log, err := store.NewOpsLog(dataDir)
            if err != nil {
                return err
            }

            op, err := model.NewDeleteOperation(deviceID, args[0], time.Now().UTC())
            if err != nil {
                return err
            }
            return log.Append(op)
        },
    }
    return cmd
}
