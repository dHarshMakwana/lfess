package store_test

import (
    "os"
    "path/filepath"
    "strings"
    "testing"
    "time"

    "github.com/dHarshMakwana/lfess/internal/model"
    "github.com/dHarshMakwana/lfess/internal/store"
    "github.com/stretchr/testify/require"
)

func TestEnsureDeviceID_Persists(t *testing.T) {
    dir := t.TempDir()

    id1, err := store.EnsureDeviceID(dir)
    require.NoError(t, err)
    require.NotEmpty(t, id1)

    id2, err := store.EnsureDeviceID(dir)
    require.NoError(t, err)
    require.Equal(t, id1, id2)

    b, err := os.ReadFile(filepath.Join(dir, "device.id"))
    require.NoError(t, err)
    require.True(t, strings.Contains(string(b), id1))
}

func TestOpsLog_AppendAndReadAll(t *testing.T) {
    dir := t.TempDir()
    deviceID, err := store.EnsureDeviceID(dir)
    require.NoError(t, err)

    log, err := store.NewOpsLog(dir)
    require.NoError(t, err)

    op1, err := model.NewAddOperation(deviceID, "item1", "a", time.Unix(10, 0).UTC())
    require.NoError(t, err)
    op2, err := model.NewUpdateOperation(deviceID, "item1", "b", time.Unix(11, 0).UTC())
    require.NoError(t, err)

    require.NoError(t, log.Append(op1))
    require.NoError(t, log.Append(op2))

    ops, err := log.ReadAll()
    require.NoError(t, err)
    require.Len(t, ops, 2)
    require.Equal(t, op1.OperationID, ops[0].OperationID)
    require.Equal(t, op2.OperationID, ops[1].OperationID)
}

func TestOpsLog_IsAppendOnly_LineCountNeverDecreases(t *testing.T) {
    dir := t.TempDir()
    deviceID, err := store.EnsureDeviceID(dir)
    require.NoError(t, err)

    log, err := store.NewOpsLog(dir)
    require.NoError(t, err)

    contents := []string{"a", "b", "c", "d"}

    prevLines := 0
    for i, c := range contents {
        op, err := model.NewAddOperation(deviceID, "item"+string(rune('0'+i)), c, time.Now().UTC())
        require.NoError(t, err)
        require.NoError(t, log.Append(op))

        b, err := os.ReadFile(log.Path())
        require.NoError(t, err)
        lines := strings.Count(string(b), "\n")
        require.GreaterOrEqual(t, lines, prevLines)
        prevLines = lines
    }
}
