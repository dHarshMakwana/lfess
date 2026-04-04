package store_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/dHarshMakwana/lfess/internal/crypto"
	"github.com/dHarshMakwana/lfess/internal/engine"
	"github.com/dHarshMakwana/lfess/internal/model"
	"github.com/dHarshMakwana/lfess/internal/store"
	"github.com/stretchr/testify/require"
)

type checklistStoreItem struct {
	ID      string `json:"id"`
	Content string `json:"content"`
	Deleted bool   `json:"deleted"`
}

func checklistStoreStateHash(ops []model.Operation) string {
	state := engine.Replay(ops)
	ids := make([]string, 0, len(state))
	for id := range state {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	canonical := make([]checklistStoreItem, 0, len(ids))
	for _, id := range ids {
		item := state[id]
		canonical = append(canonical, checklistStoreItem{ID: item.ID, Content: item.Content, Deleted: item.Deleted})
	}

	b, _ := json.Marshal(canonical)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func checklistStoreAdd(opID, itemID, content string, at time.Time) model.Operation {
	return model.Operation{
		OperationID: opID,
		ItemID:      itemID,
		DeviceID:    "device-store",
		Type:        model.OperationAdd,
		Payload:     map[string]any{"content": content},
		Timestamp:   at,
	}
}

func checklistStoreCopyKey(t *testing.T, fromDir, toDir string) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(fromDir, crypto.KeyFileName))
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(toDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(toDir, crypto.KeyFileName), b, 0o600))
}

func checklistStoreEncryptLine(t *testing.T, dataDir string, plaintext []byte) string {
	t.Helper()
	crypto.SetDataDir(dataDir)
	ciphertext, err := crypto.Encrypt(plaintext)
	require.NoError(t, err)
	return base64.StdEncoding.EncodeToString(ciphertext)
}

func checklistStoreEncryptOp(t *testing.T, dataDir string, op model.Operation) string {
	t.Helper()
	plaintext, err := json.Marshal(op)
	require.NoError(t, err)
	return checklistStoreEncryptLine(t, dataDir, plaintext)
}

func TestChecklistT7_OperationIDCollisionWithDifferentPayload(t *testing.T) {
	localDir := t.TempDir()
	localLog, err := store.NewOpsLog(localDir)
	require.NoError(t, err)

	base := checklistStoreAdd("collision-op", "item-1", "alpha", time.Unix(10, 0).UTC())
	require.NoError(t, localLog.Append(base))

	beforeBytes, err := os.ReadFile(localLog.Path())
	require.NoError(t, err)
	beforeOps, err := localLog.ReadAll()
	require.NoError(t, err)
	beforeStateHash := checklistStoreStateHash(beforeOps)

	remoteDir := t.TempDir()
	checklistStoreCopyKey(t, localDir, remoteDir)
	remoteLog, err := store.NewOpsLog(remoteDir)
	require.NoError(t, err)

	conflicting := checklistStoreAdd("collision-op", "item-2", "beta", time.Unix(11, 0).UTC())
	require.NoError(t, remoteLog.Append(conflicting))
	lines, err := remoteLog.ReadEncryptedLines()
	require.NoError(t, err)

	imported, err := localLog.ImportEncryptedLines(lines)
	require.Error(t, err)
	require.True(t, errors.Is(err, store.ErrOperationIDCollision))
	require.Equal(t, 0, imported)

	afterBytes, err := os.ReadFile(localLog.Path())
	require.NoError(t, err)
	require.Equal(t, beforeBytes, afterBytes)

	afterOps, err := localLog.ReadAll()
	require.NoError(t, err)
	require.Equal(t, beforeStateHash, checklistStoreStateHash(afterOps))
}

func TestChecklistT12_CrashTruncationDuringWriteRecoversValidPrefix(t *testing.T) {
	dir := t.TempDir()
	log, err := store.NewOpsLog(dir)
	require.NoError(t, err)

	const n = 5
	original := make([]model.Operation, 0, n)
	for i := 0; i < n; i++ {
		op := checklistStoreAdd(
			"truncate-op-"+time.Unix(int64(i+1), 0).UTC().Format("150405"),
			"item-"+time.Unix(int64(i+1), 0).UTC().Format("150405"),
			"value",
			time.Unix(int64(i+1), 0).UTC(),
		)
		original = append(original, op)
		require.NoError(t, log.Append(op))
	}

	b, err := os.ReadFile(log.Path())
	require.NoError(t, err)
	require.NotEmpty(t, b)

	end := len(b)
	if b[end-1] == '\n' {
		end--
	}
	start := bytes.LastIndexByte(b[:end], '\n') + 1
	require.Less(t, start, end)
	mid := start + (end-start)/2
	require.NoError(t, os.WriteFile(log.Path(), append([]byte{}, b[:mid]...), 0o600))

	restarted, err := store.NewOpsLog(dir)
	require.NoError(t, err)
	recovered, err := restarted.ReadAll()
	require.NoError(t, err)
	require.Len(t, recovered, n-1)

	for i := 0; i < n-1; i++ {
		require.Equal(t, original[i].OperationID, recovered[i].OperationID)
	}

	state := engine.Replay(recovered)
	require.Len(t, state, n-1)
	for i := 0; i < n-1; i++ {
		itemID := original[i].ItemID
		require.Contains(t, state, itemID)
	}
}

func TestChecklistT13_CorruptionMatrixRecovery(t *testing.T) {
	dir := t.TempDir()
	log, err := store.NewOpsLog(dir)
	require.NoError(t, err)

	valid1 := checklistStoreAdd("valid-1", "item-a", "a", time.Unix(1, 0).UTC())
	valid2 := checklistStoreAdd("valid-2", "item-b", "b", time.Unix(2, 0).UTC())
	valid3 := checklistStoreAdd("valid-3", "item-c", "c", time.Unix(3, 0).UTC())
	baseline := []model.Operation{valid1, valid2, valid3}
	baselineHash := checklistStoreStateHash(baseline)

	lines := []string{
		checklistStoreEncryptOp(t, dir, valid1),
		"not-base64-text",
		base64.StdEncoding.EncodeToString([]byte("not-age-ciphertext")),
		checklistStoreEncryptLine(t, dir, []byte("this-is-not-json")),
		checklistStoreEncryptLine(t, dir, []byte(`{"operation_id":"missing-fields"}`)),
		checklistStoreEncryptOp(t, dir, valid2),
		checklistStoreEncryptOp(t, dir, valid3),
	}

	require.NoError(t, os.WriteFile(log.Path(), []byte(strings.Join(lines, "\n")+"\n"), 0o600))

	recovered, err := log.ReadAll()
	require.NoError(t, err)
	require.Len(t, recovered, 3)
	require.Equal(t, "valid-1", recovered[0].OperationID)
	require.Equal(t, "valid-2", recovered[1].OperationID)
	require.Equal(t, "valid-3", recovered[2].OperationID)

	recoveredHash := checklistStoreStateHash(recovered)
	require.Equal(t, baselineHash, recoveredHash)
}
