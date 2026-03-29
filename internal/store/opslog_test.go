package store_test

import (
    "encoding/base64"
    "os"
    "path/filepath"
    "sync"
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

    // Ensure on disk is NOT plaintext JSON.
    b, err := os.ReadFile(log.Path())
    require.NoError(t, err)
    require.NotContains(t, string(b), "operation_id")

    ops, err := log.ReadAll()
    require.NoError(t, err)
    require.Len(t, ops, 2)
    require.Equal(t, op1.OperationID, ops[0].OperationID)
    require.Equal(t, op2.OperationID, ops[1].OperationID)
}

func TestOpsLog_ReadAll_WorksAcrossInstances(t *testing.T) {
    dir := t.TempDir()
    deviceID, err := store.EnsureDeviceID(dir)
    require.NoError(t, err)

    log1, err := store.NewOpsLog(dir)
    require.NoError(t, err)

    op1, err := model.NewAddOperation(deviceID, "item1", "a", time.Unix(10, 0).UTC())
    require.NoError(t, err)
    require.NoError(t, log1.Append(op1))

    // New instance simulates a new process run reading the same directory.
    log2, err := store.NewOpsLog(dir)
    require.NoError(t, err)

    ops, err := log2.ReadAll()
    require.NoError(t, err)
    require.Len(t, ops, 1)
    require.Equal(t, op1.OperationID, ops[0].OperationID)
}

func TestOpsLog_ReadAll_MissingLogReturnsEmptySlice(t *testing.T) {
    dir := t.TempDir()
    log, err := store.NewOpsLog(dir)
    require.NoError(t, err)

    ops, err := log.ReadAll()
    require.NoError(t, err)
    require.NotNil(t, ops)
    require.Len(t, ops, 0)
}

func TestOpsLog_ReadAll_SkipsCorruptedLineAndKeepsGoing(t *testing.T) {
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
    // Append a corrupted line (not base64).
    f, err := os.OpenFile(log.Path(), os.O_APPEND|os.O_WRONLY, 0o600)
    require.NoError(t, err)
    _, _ = f.WriteString("not-base64!!!\n")
    require.NoError(t, f.Sync())
    require.NoError(t, f.Close())
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

func TestOpsLog_ConcurrentAppends_AllLinesAreBase64(t *testing.T) {
    dir := t.TempDir()
    deviceID, err := store.EnsureDeviceID(dir)
    require.NoError(t, err)

    log, err := store.NewOpsLog(dir)
    require.NoError(t, err)

    const n = 50
    var wg sync.WaitGroup
    wg.Add(n)
    for i := 0; i < n; i++ {
        i := i
        go func() {
            defer wg.Done()
            op, err := model.NewAddOperation(deviceID, "item", "v", time.Unix(int64(100+i), 0).UTC())
            require.NoError(t, err)
            require.NoError(t, log.Append(op))
        }()
    }
    wg.Wait()

    lines, err := log.ReadEncryptedLines()
    require.NoError(t, err)
    require.Len(t, lines, n)
    for _, line := range lines {
        _, err := base64.StdEncoding.DecodeString(line)
        require.NoError(t, err)
    }
}
