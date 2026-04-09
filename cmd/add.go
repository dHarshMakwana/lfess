package cmd

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/dHarshMakwana/lfess/internal/model"
	"github.com/dHarshMakwana/lfess/internal/store"
	"github.com/spf13/cobra"
)

func newAddCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add <content>",
		Short: "Add a new item",
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

			itemID, err := newItemID()
			if err != nil {
				return err
			}

			op, err := model.NewAddOperation(deviceID, itemID, args[0], time.Now().UTC())
			if err != nil {
				return err
			}
			if err := log.Append(op); err != nil {
				return err
			}

			fmt.Fprintln(cmd.OutOrStdout(), itemID)
			return nil
		},
	}

	return cmd
}

func newItemID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
