package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/dHarshMakwana/lfess/internal/engine"
	"github.com/dHarshMakwana/lfess/internal/model"
	"github.com/dHarshMakwana/lfess/internal/store"
	"github.com/stretchr/testify/require"
)

func TestCommands_AddUpdateDeleteAndList_ReplayConsistent(t *testing.T) {
	dataDir := t.TempDir()

	addKeepOut, err := runRootCommand(t, dataDir, "add", "keep")
	require.NoError(t, err)
	keepID := strings.TrimSpace(addKeepOut)
	require.NotEmpty(t, keepID)

	addDropOut, err := runRootCommand(t, dataDir, "add", "drop")
	require.NoError(t, err)
	dropID := strings.TrimSpace(addDropOut)
	require.NotEmpty(t, dropID)
	require.NotEqual(t, keepID, dropID)

	_, err = runRootCommand(t, dataDir, "update", keepID, "keep-updated")
	require.NoError(t, err)

	_, err = runRootCommand(t, dataDir, "delete", dropID)
	require.NoError(t, err)

	listOut, err := runRootCommand(t, dataDir, "list")
	require.NoError(t, err)

	log, err := store.NewOpsLog(dataDir)
	require.NoError(t, err)
	ops, err := log.ReadAll()
	require.NoError(t, err)

	require.Len(t, ops, 4)
	require.Equal(t, model.OperationAdd, ops[0].Type)
	require.Equal(t, keepID, ops[0].ItemID)
	require.Equal(t, model.OperationAdd, ops[1].Type)
	require.Equal(t, dropID, ops[1].ItemID)
	require.Equal(t, model.OperationUpdate, ops[2].Type)
	require.Equal(t, keepID, ops[2].ItemID)
	require.Equal(t, model.OperationDelete, ops[3].Type)
	require.Equal(t, dropID, ops[3].ItemID)

	state := engine.Replay(ops)
	ids := make([]string, 0, len(state))
	for id, item := range state {
		if item.Deleted {
			continue
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)

	var expected strings.Builder
	for _, id := range ids {
		item := state[id]
		fmt.Fprintf(&expected, "%s\t%s\n", item.ID, item.Content)
	}

	require.Equal(t, expected.String(), listOut)
	require.Contains(t, listOut, keepID+"\tkeep-updated")
	require.NotContains(t, listOut, dropID+"\t")
}

func TestListCommand_EmptyStateProducesNoOutput(t *testing.T) {
	dataDir := t.TempDir()

	out, err := runRootCommand(t, dataDir, "list")
	require.NoError(t, err)
	require.Equal(t, "", out)
}

func TestCommands_DataDirFlagIsolation_NoDefaultDirWrites(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	dataDirA := t.TempDir()
	dataDirB := t.TempDir()

	addOut, err := runRootCommand(t, dataDirA, "add", "from-a")
	require.NoError(t, err)
	itemID := strings.TrimSpace(addOut)
	require.NotEmpty(t, itemID)

	outB, err := runRootCommand(t, dataDirB, "list")
	require.NoError(t, err)
	require.Equal(t, "", outB)

	outA, err := runRootCommand(t, dataDirA, "list")
	require.NoError(t, err)
	require.Contains(t, outA, itemID+"\tfrom-a")

	_, err = os.Stat(filepath.Join(homeDir, ".lfess", "ops.log"))
	require.True(t, errors.Is(err, os.ErrNotExist))

	_, err = os.Stat(filepath.Join(dataDirA, "ops.log"))
	require.NoError(t, err)
}

func runRootCommand(t *testing.T, dataDir string, args ...string) (string, error) {
	t.Helper()

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs(append([]string{"--data-dir", dataDir}, args...))

	err := cmd.Execute()
	return out.String(), err
}
