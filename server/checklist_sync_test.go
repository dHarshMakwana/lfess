package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"sync"
	"testing"
	"time"

	lfcrypto "github.com/dHarshMakwana/lfess/internal/crypto"
	"github.com/dHarshMakwana/lfess/internal/engine"
	"github.com/dHarshMakwana/lfess/internal/model"
	"github.com/dHarshMakwana/lfess/internal/store"
	"github.com/stretchr/testify/require"
)

type checklistSyncStateItem struct {
	ID      string `json:"id"`
	Content string `json:"content"`
	Deleted bool   `json:"deleted"`
}

type checklistSyncMetrics struct {
	StateHash     string
	OpSetHash     string
	UniqueOpCount int
	LogLineCount  int
	LogBytes      []byte
	State         map[string]model.Item
	Ops           []model.Operation
}

func checklistSyncAdd(opID, itemID, deviceID, content string, at time.Time) model.Operation {
	return model.Operation{
		OperationID: opID,
		ItemID:      itemID,
		DeviceID:    deviceID,
		Type:        model.OperationAdd,
		Payload:     map[string]any{"content": content},
		Timestamp:   at,
	}
}

func checklistSyncUpdate(opID, itemID, deviceID, content string, at time.Time) model.Operation {
	return model.Operation{
		OperationID: opID,
		ItemID:      itemID,
		DeviceID:    deviceID,
		Type:        model.OperationUpdate,
		Payload:     map[string]any{"content": content},
		Timestamp:   at,
	}
}

func checklistSyncDelete(opID, itemID, deviceID string, at time.Time) model.Operation {
	return model.Operation{
		OperationID: opID,
		ItemID:      itemID,
		DeviceID:    deviceID,
		Type:        model.OperationDelete,
		Payload:     map[string]any{"deleted": true},
		Timestamp:   at,
	}
}

func checklistStateHash(state map[string]model.Item) string {
	ids := make([]string, 0, len(state))
	for id := range state {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	canonical := make([]checklistSyncStateItem, 0, len(ids))
	for _, id := range ids {
		item := state[id]
		canonical = append(canonical, checklistSyncStateItem{ID: item.ID, Content: item.Content, Deleted: item.Deleted})
	}

	b, _ := json.Marshal(canonical)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func checklistOpSetHash(ops []model.Operation) (string, int) {
	set := make(map[string]struct{}, len(ops))
	for _, op := range ops {
		set[op.OperationID] = struct{}{}
	}
	ids := make([]string, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	b, _ := json.Marshal(ids)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), len(ids)
}

func checklistCollectMetrics(t *testing.T, dataDir string) checklistSyncMetrics {
	t.Helper()

	log, err := store.NewOpsLog(dataDir)
	require.NoError(t, err)

	ops, err := log.ReadAll()
	require.NoError(t, err)
	lines, err := log.ReadEncryptedLines()
	require.NoError(t, err)

	logBytes, err := os.ReadFile(log.Path())
	if err != nil {
		require.True(t, os.IsNotExist(err))
		logBytes = nil
	}

	state := engine.Replay(ops)
	opHash, uniqueCount := checklistOpSetHash(ops)

	return checklistSyncMetrics{
		StateHash:     checklistStateHash(state),
		OpSetHash:     opHash,
		UniqueOpCount: uniqueCount,
		LogLineCount:  len(lines),
		LogBytes:      logBytes,
		State:         state,
		Ops:           ops,
	}
}

func checklistPostImport(t *testing.T, handler http.Handler, lines []string) (status int, imported int, body string) {
	t.Helper()

	payload, err := json.Marshal(lines)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/ops", bytes.NewReader(payload))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	imported = 0
	if rr.Code == http.StatusOK {
		var resp map[string]int
		require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
		imported = resp["imported"]
	}
	return rr.Code, imported, rr.Body.String()
}

func checklistReadLines(t *testing.T, dataDir string) []string {
	t.Helper()
	log, err := store.NewOpsLog(dataDir)
	require.NoError(t, err)
	lines, err := log.ReadEncryptedLines()
	require.NoError(t, err)
	return lines
}

func checklistEnsureKey(t *testing.T, dataDir string) {
	t.Helper()
	_, err := lfcrypto.EnsureX25519Identity(dataDir)
	require.NoError(t, err)
}

func checklistBuildEncryptedLinesWithSharedKey(t *testing.T, keyFromDir string, ops []model.Operation) []string {
	t.Helper()
	tmp := t.TempDir()
	copySharedKey(t, keyFromDir, tmp)
	log, err := store.NewOpsLog(tmp)
	require.NoError(t, err)
	for _, op := range ops {
		require.NoError(t, log.Append(op))
	}
	lines, err := log.ReadEncryptedLines()
	require.NoError(t, err)
	return lines
}

func checklistGenerateMixedWorkload(total int) []model.Operation {
	const uniqueCount = 7000
	baseTime := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	unique := make([]model.Operation, 0, uniqueCount)

	for i := 0; i < uniqueCount; i++ {
		opID := fmt.Sprintf("bulk-%05d", i)
		itemID := fmt.Sprintf("item-%04d", i%512)
		deviceID := "dev-A"
		if i%2 == 1 {
			deviceID = "dev-B"
		}
		ts := baseTime.Add(time.Duration((i*37)%10000) * time.Millisecond)

		switch i % 5 {
		case 0:
			unique = append(unique, checklistSyncDelete(opID, itemID, deviceID, ts))
		case 1, 2:
			unique = append(unique, checklistSyncAdd(opID, itemID, deviceID, fmt.Sprintf("add-%d", i), ts))
		default:
			unique = append(unique, checklistSyncUpdate(opID, itemID, deviceID, fmt.Sprintf("update-%d", i), ts))
		}
	}

	stream := make([]model.Operation, 0, total)
	stream = append(stream, unique...)
	for len(stream) < total {
		idx := (len(stream)*53 + 17) % len(unique)
		stream = append(stream, unique[idx])
	}

	rng := rand.New(rand.NewSource(42))
	rng.Shuffle(len(stream), func(i, j int) {
		stream[i], stream[j] = stream[j], stream[i]
	})

	return stream
}

func TestChecklistT1_BaselineDivergenceAndDoubleSync(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	dirA := t.TempDir()
	logA, err := store.NewOpsLog(dirA)
	require.NoError(t, err)
	require.NoError(t, logA.Append(checklistSyncAdd("A1", "task-a", "dev-A", "Task A", t0)))

	dirB := t.TempDir()
	copySharedKey(t, dirA, dirB)
	logB, err := store.NewOpsLog(dirB)
	require.NoError(t, err)
	require.NoError(t, logB.Append(checklistSyncAdd("B1", "task-b", "dev-B", "Task B", t0.Add(time.Second))))

	svcA, err := New(Config{DataDir: dirA})
	require.NoError(t, err)
	svcB, err := New(Config{DataDir: dirB})
	require.NoError(t, err)

	statusA1, importedA1, _ := checklistPostImport(t, svcA.Handler(), checklistReadLines(t, dirB))
	require.Equal(t, http.StatusOK, statusA1)
	require.Equal(t, 1, importedA1)

	statusB1, importedB1, _ := checklistPostImport(t, svcB.Handler(), checklistReadLines(t, dirA))
	require.Equal(t, http.StatusOK, statusB1)
	require.Equal(t, 1, importedB1)

	statusA2, importedA2, _ := checklistPostImport(t, svcA.Handler(), checklistReadLines(t, dirB))
	require.Equal(t, http.StatusOK, statusA2)
	require.Equal(t, 0, importedA2)

	statusB2, importedB2, _ := checklistPostImport(t, svcB.Handler(), checklistReadLines(t, dirA))
	require.Equal(t, http.StatusOK, statusB2)
	require.Equal(t, 0, importedB2)

	metricsA := checklistCollectMetrics(t, dirA)
	metricsB := checklistCollectMetrics(t, dirB)
	require.Equal(t, metricsA.StateHash, metricsB.StateHash)
	require.Equal(t, metricsA.OpSetHash, metricsB.OpSetHash)
	require.Equal(t, 2, metricsA.UniqueOpCount)
	require.Equal(t, 2, metricsB.UniqueOpCount)
}

func TestChecklistT2_SequentialDuplicateImportIdempotency(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	localDir := t.TempDir()
	checklistEnsureKey(t, localDir)

	svc, err := New(Config{DataDir: localDir})
	require.NoError(t, err)

	lines := checklistBuildEncryptedLinesWithSharedKey(t, localDir, []model.Operation{
		checklistSyncAdd("O1", "item-dup", "dev-remote", "single-item", t0),
	})

	for i := 0; i < 10; i++ {
		status, imported, _ := checklistPostImport(t, svc.Handler(), lines)
		require.Equal(t, http.StatusOK, status)
		if i == 0 {
			require.Equal(t, 1, imported)
		} else {
			require.Equal(t, 0, imported)
		}
	}

	metrics := checklistCollectMetrics(t, localDir)
	require.Equal(t, 1, metrics.UniqueOpCount)
	require.Equal(t, 1, metrics.LogLineCount)

	baseline := metrics.StateHash
	for i := 0; i < 10; i++ {
		restartedMetrics := checklistCollectMetrics(t, localDir)
		require.Equal(t, baseline, restartedMetrics.StateHash)
	}
}

func TestChecklistT4_DeleteVsUpdateConflictTwoPerspectives(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	o0 := checklistSyncAdd("O0", "item-x", "shared", "base", t0)
	o1 := checklistSyncDelete("O1", "item-x", "dev-A", t0.Add(time.Second))
	o2 := checklistSyncUpdate("O2", "item-x", "dev-B", "v2", t0.Add(2*time.Second))

	dirA := t.TempDir()
	logA, err := store.NewOpsLog(dirA)
	require.NoError(t, err)
	require.NoError(t, logA.Append(o0))
	require.NoError(t, logA.Append(o1))

	dirB := t.TempDir()
	copySharedKey(t, dirA, dirB)
	logB, err := store.NewOpsLog(dirB)
	require.NoError(t, err)
	require.NoError(t, logB.Append(o0))
	require.NoError(t, logB.Append(o2))

	svcA, err := New(Config{DataDir: dirA})
	require.NoError(t, err)
	svcB, err := New(Config{DataDir: dirB})
	require.NoError(t, err)

	statusA1, importedA1, _ := checklistPostImport(t, svcA.Handler(), checklistReadLines(t, dirB))
	require.Equal(t, http.StatusOK, statusA1)
	require.Equal(t, 1, importedA1)

	statusB1, importedB1, _ := checklistPostImport(t, svcB.Handler(), checklistReadLines(t, dirA))
	require.Equal(t, http.StatusOK, statusB1)
	require.Equal(t, 1, importedB1)

	statusA2, importedA2, _ := checklistPostImport(t, svcA.Handler(), checklistReadLines(t, dirB))
	require.Equal(t, http.StatusOK, statusA2)
	require.Equal(t, 0, importedA2)

	statusB2, importedB2, _ := checklistPostImport(t, svcB.Handler(), checklistReadLines(t, dirA))
	require.Equal(t, http.StatusOK, statusB2)
	require.Equal(t, 0, importedB2)

	metricsA := checklistCollectMetrics(t, dirA)
	metricsB := checklistCollectMetrics(t, dirB)
	require.Equal(t, metricsA.StateHash, metricsB.StateHash)
	require.Equal(t, metricsA.OpSetHash, metricsB.OpSetHash)
	require.True(t, metricsA.State["item-x"].Deleted)
	require.True(t, metricsB.State["item-x"].Deleted)
}

func TestChecklistT8_DuplicateSyncLoopStability(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	dirA := t.TempDir()
	logA, err := store.NewOpsLog(dirA)
	require.NoError(t, err)
	for i := 0; i < 50; i++ {
		op := checklistSyncAdd(fmt.Sprintf("A-%02d", i), fmt.Sprintf("item-A-%02d", i), "dev-A", "v", t0.Add(time.Duration(i)*time.Second))
		require.NoError(t, logA.Append(op))
	}

	dirB := t.TempDir()
	copySharedKey(t, dirA, dirB)
	logB, err := store.NewOpsLog(dirB)
	require.NoError(t, err)
	for i := 0; i < 50; i++ {
		op := checklistSyncAdd(fmt.Sprintf("B-%02d", i), fmt.Sprintf("item-B-%02d", i), "dev-B", "v", t0.Add(time.Duration(i)*time.Second))
		require.NoError(t, logB.Append(op))
	}

	svcA, err := New(Config{DataDir: dirA})
	require.NoError(t, err)
	svcB, err := New(Config{DataDir: dirB})
	require.NoError(t, err)

	var stableStateHash string
	var stableOpHash string
	for round := 1; round <= 20; round++ {
		statusA, importedA, _ := checklistPostImport(t, svcA.Handler(), checklistReadLines(t, dirB))
		require.Equal(t, http.StatusOK, statusA)
		statusB, importedB, _ := checklistPostImport(t, svcB.Handler(), checklistReadLines(t, dirA))
		require.Equal(t, http.StatusOK, statusB)

		if round == 1 {
			require.Equal(t, 50, importedA)
			require.Equal(t, 50, importedB)
		} else {
			require.Equal(t, 0, importedA)
			require.Equal(t, 0, importedB)
		}

		metricsA := checklistCollectMetrics(t, dirA)
		metricsB := checklistCollectMetrics(t, dirB)
		require.Equal(t, metricsA.StateHash, metricsB.StateHash)
		require.Equal(t, metricsA.OpSetHash, metricsB.OpSetHash)

		if round == 1 {
			stableStateHash = metricsA.StateHash
			stableOpHash = metricsA.OpSetHash
		} else {
			require.Equal(t, stableStateHash, metricsA.StateHash)
			require.Equal(t, stableStateHash, metricsB.StateHash)
			require.Equal(t, stableOpHash, metricsA.OpSetHash)
			require.Equal(t, stableOpHash, metricsB.OpSetHash)
		}
	}

	finalA := checklistCollectMetrics(t, dirA)
	finalB := checklistCollectMetrics(t, dirB)
	require.Equal(t, 100, finalA.UniqueOpCount)
	require.Equal(t, 100, finalB.UniqueOpCount)
}

func TestChecklistT9_ConcurrentDuplicateImportRace(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	localDir := t.TempDir()
	checklistEnsureKey(t, localDir)

	svc, err := New(Config{DataDir: localDir})
	require.NoError(t, err)
	ts := httptest.NewServer(svc.Handler())
	defer ts.Close()

	lines := checklistBuildEncryptedLinesWithSharedKey(t, localDir, []model.Operation{
		checklistSyncAdd("Orace", "item-race", "dev-race", "value", t0),
	})
	payload, err := json.Marshal(lines)
	require.NoError(t, err)

	const workers = 20
	var wg sync.WaitGroup
	wg.Add(workers)
	errCh := make(chan error, workers)
	importedCh := make(chan int, workers)

	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			resp, err := http.Post(ts.URL+"/ops", "application/json", bytes.NewReader(payload))
			if err != nil {
				errCh <- err
				return
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				errCh <- fmt.Errorf("unexpected status: %d", resp.StatusCode)
				return
			}
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				errCh <- err
				return
			}
			var out map[string]int
			if err := json.Unmarshal(body, &out); err != nil {
				errCh <- err
				return
			}
			importedCh <- out["imported"]
		}()
	}

	wg.Wait()
	close(errCh)
	close(importedCh)
	for err := range errCh {
		require.NoError(t, err)
	}

	totalImported := 0
	for v := range importedCh {
		totalImported += v
	}
	require.Equal(t, 1, totalImported)

	metrics := checklistCollectMetrics(t, localDir)
	require.Equal(t, 1, metrics.UniqueOpCount)
	require.Equal(t, 1, metrics.LogLineCount)
}

func TestChecklistT10_PartialBatchFailureAtomicity(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	localDir := t.TempDir()
	log, err := store.NewOpsLog(localDir)
	require.NoError(t, err)
	require.NoError(t, log.Append(checklistSyncAdd("seed", "seed-item", "seed", "seed", t0)))

	svc, err := New(Config{DataDir: localDir})
	require.NoError(t, err)

	validOps := []model.Operation{
		checklistSyncAdd("O1", "item-1", "remote", "a", t0.Add(time.Second)),
		checklistSyncAdd("O2", "item-2", "remote", "b", t0.Add(2*time.Second)),
		checklistSyncAdd("O4", "item-4", "remote", "d", t0.Add(4*time.Second)),
	}
	validLines := checklistBuildEncryptedLinesWithSharedKey(t, localDir, validOps)
	payloadLines := []string{validLines[0], validLines[1], "not-base64-line", validLines[2]}

	before := checklistCollectMetrics(t, localDir)
	status, _, _ := checklistPostImport(t, svc.Handler(), payloadLines)
	require.Equal(t, http.StatusBadRequest, status)
	after := checklistCollectMetrics(t, localDir)

	require.Equal(t, before.LogBytes, after.LogBytes)
	require.Equal(t, before.LogLineCount, after.LogLineCount)
	require.Equal(t, before.UniqueOpCount, after.UniqueOpCount)

	seen := make(map[string]struct{}, len(after.Ops))
	for _, op := range after.Ops {
		seen[op.OperationID] = struct{}{}
	}
	for _, id := range []string{"O1", "O2", "O4"} {
		_, ok := seen[id]
		require.False(t, ok)
	}
}

func TestChecklistT11_WrongEncryptionKeyAtomicity(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	localDir := t.TempDir()
	log, err := store.NewOpsLog(localDir)
	require.NoError(t, err)
	require.NoError(t, log.Append(checklistSyncAdd("seed", "seed-item", "seed", "seed", t0)))

	svc, err := New(Config{DataDir: localDir})
	require.NoError(t, err)

	remoteDir := t.TempDir()
	remoteLog, err := store.NewOpsLog(remoteDir)
	require.NoError(t, err)
	require.NoError(t, remoteLog.Append(checklistSyncAdd("bad-key-op", "item-k", "remote", "value", t0.Add(time.Second))))
	lines, err := remoteLog.ReadEncryptedLines()
	require.NoError(t, err)

	before := checklistCollectMetrics(t, localDir)
	status, _, _ := checklistPostImport(t, svc.Handler(), lines)
	require.Equal(t, http.StatusBadRequest, status)
	after := checklistCollectMetrics(t, localDir)

	require.Equal(t, before.LogBytes, after.LogBytes)
	require.Equal(t, before.StateHash, after.StateHash)
}

func TestChecklistT14_LargeMixedWorkloadPermutationInvariance(t *testing.T) {
	dirA := t.TempDir()
	checklistEnsureKey(t, dirA)
	dirB := t.TempDir()
	copySharedKey(t, dirA, dirB)

	svcA, err := New(Config{DataDir: dirA})
	require.NoError(t, err)
	svcB, err := New(Config{DataDir: dirB})
	require.NoError(t, err)

	stream := checklistGenerateMixedWorkload(10000)
	partA := make([]model.Operation, 0, len(stream)/2)
	partB := make([]model.Operation, 0, len(stream)/2)
	for i, op := range stream {
		if i%2 == 0 {
			partA = append(partA, op)
		} else {
			partB = append(partB, op)
		}
	}
	for i, j := 0, len(partB)-1; i < j; i, j = i+1, j-1 {
		partB[i], partB[j] = partB[j], partB[i]
	}

	statusASeed, _, _ := checklistPostImport(t, svcA.Handler(), checklistBuildEncryptedLinesWithSharedKey(t, dirA, partA))
	require.Equal(t, http.StatusOK, statusASeed)
	statusBSeed, _, _ := checklistPostImport(t, svcB.Handler(), checklistBuildEncryptedLinesWithSharedKey(t, dirA, partB))
	require.Equal(t, http.StatusOK, statusBSeed)

	statusA1, _, _ := checklistPostImport(t, svcA.Handler(), checklistReadLines(t, dirB))
	require.Equal(t, http.StatusOK, statusA1)
	statusB1, _, _ := checklistPostImport(t, svcB.Handler(), checklistReadLines(t, dirA))
	require.Equal(t, http.StatusOK, statusB1)

	statusA2, importedA2, _ := checklistPostImport(t, svcA.Handler(), checklistReadLines(t, dirB))
	require.Equal(t, http.StatusOK, statusA2)
	require.Equal(t, 0, importedA2)
	statusB2, importedB2, _ := checklistPostImport(t, svcB.Handler(), checklistReadLines(t, dirA))
	require.Equal(t, http.StatusOK, statusB2)
	require.Equal(t, 0, importedB2)

	metricsA := checklistCollectMetrics(t, dirA)
	metricsB := checklistCollectMetrics(t, dirB)
	require.Equal(t, metricsA.OpSetHash, metricsB.OpSetHash)
	require.Equal(t, metricsA.StateHash, metricsB.StateHash)
}
