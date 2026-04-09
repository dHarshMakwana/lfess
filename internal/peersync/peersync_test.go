package peersync

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dHarshMakwana/lfess/internal/crypto"
	"github.com/dHarshMakwana/lfess/internal/engine"
	"github.com/dHarshMakwana/lfess/internal/model"
	"github.com/dHarshMakwana/lfess/internal/store"
	"github.com/dHarshMakwana/lfess/server"
	"github.com/stretchr/testify/require"
)

func TestNormalizePeerAddress(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
		err   bool
	}{
		{name: "valid", input: "http://127.0.0.1:7777", want: "http://127.0.0.1:7777"},
		{name: "trim-and-trailing-slash", input: "  http://10.0.0.5:9000/  ", want: "http://10.0.0.5:9000"},
		{name: "missing-scheme", input: "127.0.0.1:7777", err: true},
		{name: "unsupported-scheme", input: "https://127.0.0.1:7777", err: true},
		{name: "missing-host", input: "http://:7777", err: true},
		{name: "path-not-allowed", input: "http://127.0.0.1:7777/ops", err: true},
		{name: "query-not-allowed", input: "http://127.0.0.1:7777/?x=1", err: true},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := NormalizePeerAddress(tc.input)
			if tc.err {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestPullAndImport_SuccessAndIdempotent(t *testing.T) {
	remoteDir := t.TempDir()
	remoteLog, err := store.NewOpsLog(remoteDir)
	require.NoError(t, err)
	remoteDeviceID, err := store.EnsureDeviceID(remoteDir)
	require.NoError(t, err)

	remoteOp, err := model.NewAddOperation(remoteDeviceID, "item-1", "from-remote", time.Unix(10, 0).UTC())
	require.NoError(t, err)
	require.NoError(t, remoteLog.Append(remoteOp))

	localDir := t.TempDir()
	copySharedKey(t, remoteDir, localDir)
	localLog, err := store.NewOpsLog(localDir)
	require.NoError(t, err)

	svc, err := server.New(server.Config{DataDir: remoteDir})
	require.NoError(t, err)
	ts := httptest.NewServer(svc.Handler())
	defer ts.Close()

	client := New(nil)
	imported, err := client.PullAndImport(context.Background(), ts.URL, localLog)
	require.NoError(t, err)
	require.Equal(t, 1, imported)

	imported, err = client.PullAndImport(context.Background(), ts.URL, localLog)
	require.NoError(t, err)
	require.Equal(t, 0, imported)

	ops, err := localLog.ReadAll()
	require.NoError(t, err)
	require.Len(t, ops, 1)
	require.Equal(t, remoteOp.OperationID, ops[0].OperationID)
}

func TestPullAndImport_RejectsTrailingJSON(t *testing.T) {
	remoteDir := t.TempDir()
	remoteLog, err := store.NewOpsLog(remoteDir)
	require.NoError(t, err)
	remoteDeviceID, err := store.EnsureDeviceID(remoteDir)
	require.NoError(t, err)
	remoteOp, err := model.NewAddOperation(remoteDeviceID, "item-1", "from-remote", time.Unix(10, 0).UTC())
	require.NoError(t, err)
	require.NoError(t, remoteLog.Append(remoteOp))

	lines, err := remoteLog.ReadEncryptedLines()
	require.NoError(t, err)
	payload, err := json.Marshal(lines)
	require.NoError(t, err)
	body := append(append([]byte{}, payload...), []byte(` {"extra":true}`)...)

	localDir := t.TempDir()
	copySharedKey(t, remoteDir, localDir)
	localLog, err := store.NewOpsLog(localDir)
	require.NoError(t, err)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	defer ts.Close()

	client := New(nil)
	imported, err := client.PullAndImport(context.Background(), ts.URL, localLog)
	require.Error(t, err)
	require.Zero(t, imported)

	ops, err := localLog.ReadAll()
	require.NoError(t, err)
	require.Len(t, ops, 0)
}

func TestPullAndImport_TwoDeviceConvergenceScenario1(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	dirA := t.TempDir()
	logA, err := store.NewOpsLog(dirA)
	require.NoError(t, err)
	devA, err := store.EnsureDeviceID(dirA)
	require.NoError(t, err)
	opA, err := model.NewAddOperation(devA, "item-a", "Task A", t0)
	require.NoError(t, err)
	require.NoError(t, logA.Append(opA))

	dirB := t.TempDir()
	copySharedKey(t, dirA, dirB)
	logB, err := store.NewOpsLog(dirB)
	require.NoError(t, err)
	devB, err := store.EnsureDeviceID(dirB)
	require.NoError(t, err)
	opB, err := model.NewAddOperation(devB, "item-b", "Task B", t0.Add(time.Second))
	require.NoError(t, err)
	require.NoError(t, logB.Append(opB))

	svcA, err := server.New(server.Config{DataDir: dirA, Host: "127.0.0.1", Port: 7777})
	require.NoError(t, err)
	tsA := httptest.NewServer(svcA.Handler())
	defer tsA.Close()

	svcB, err := server.New(server.Config{DataDir: dirB, Host: "127.0.0.1", Port: 7778})
	require.NoError(t, err)
	tsB := httptest.NewServer(svcB.Handler())
	defer tsB.Close()

	client := New(nil)

	importedB1, err := client.PullAndImport(context.Background(), tsA.URL, logB)
	require.NoError(t, err)
	require.Equal(t, 1, importedB1)
	importedA1, err := client.PullAndImport(context.Background(), tsB.URL, logA)
	require.NoError(t, err)
	require.Equal(t, 1, importedA1)

	importedB2, err := client.PullAndImport(context.Background(), tsA.URL, logB)
	require.NoError(t, err)
	require.Equal(t, 0, importedB2)
	importedA2, err := client.PullAndImport(context.Background(), tsB.URL, logA)
	require.NoError(t, err)
	require.Equal(t, 0, importedA2)

	opsA, err := logA.ReadAll()
	require.NoError(t, err)
	opsB, err := logB.ReadAll()
	require.NoError(t, err)

	stateA := engine.Replay(opsA)
	stateB := engine.Replay(opsB)
	require.Equal(t, stateA, stateB)
	require.Len(t, stateA, 2)
	require.False(t, stateA["item-a"].Deleted)
	require.False(t, stateA["item-b"].Deleted)
}

func TestPullAndImport_DeleteWinsScenario2(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	add := model.Operation{
		OperationID: "op-add",
		ItemID:      "item-x",
		DeviceID:    "shared",
		Type:        model.OperationAdd,
		Payload:     map[string]any{"content": "base"},
		Timestamp:   t0,
	}
	del := model.Operation{
		OperationID: "op-del",
		ItemID:      "item-x",
		DeviceID:    "dev-a",
		Type:        model.OperationDelete,
		Payload:     map[string]any{"deleted": true},
		Timestamp:   t0.Add(time.Second),
	}
	upd := model.Operation{
		OperationID: "op-upd",
		ItemID:      "item-x",
		DeviceID:    "dev-b",
		Type:        model.OperationUpdate,
		Payload:     map[string]any{"content": "from-b"},
		Timestamp:   t0.Add(2 * time.Second),
	}

	dirA := t.TempDir()
	logA, err := store.NewOpsLog(dirA)
	require.NoError(t, err)
	require.NoError(t, logA.Append(add))
	require.NoError(t, logA.Append(del))

	dirB := t.TempDir()
	copySharedKey(t, dirA, dirB)
	logB, err := store.NewOpsLog(dirB)
	require.NoError(t, err)
	require.NoError(t, logB.Append(add))
	require.NoError(t, logB.Append(upd))

	svcA, err := server.New(server.Config{DataDir: dirA})
	require.NoError(t, err)
	tsA := httptest.NewServer(svcA.Handler())
	defer tsA.Close()

	svcB, err := server.New(server.Config{DataDir: dirB})
	require.NoError(t, err)
	tsB := httptest.NewServer(svcB.Handler())
	defer tsB.Close()

	client := New(nil)
	_, err = client.PullAndImport(context.Background(), tsA.URL, logB)
	require.NoError(t, err)
	_, err = client.PullAndImport(context.Background(), tsB.URL, logA)
	require.NoError(t, err)

	stateA := engine.Replay(mustReadAll(t, logA))
	stateB := engine.Replay(mustReadAll(t, logB))
	require.True(t, stateA["item-x"].Deleted)
	require.True(t, stateB["item-x"].Deleted)
}

func TestPullAndImport_UnreachablePeerReturnsError(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	require.NoError(t, ln.Close())

	localLog, err := store.NewOpsLog(t.TempDir())
	require.NoError(t, err)

	client := New(nil)
	_, err = client.PullAndImport(context.Background(), fmt.Sprintf("http://%s", addr), localLog)
	require.Error(t, err)
}

func TestPullAndImport_WrongKeyReturnsErrorAndNoMutation(t *testing.T) {
	remoteDir := t.TempDir()
	remoteLog, err := store.NewOpsLog(remoteDir)
	require.NoError(t, err)
	remoteDeviceID, err := store.EnsureDeviceID(remoteDir)
	require.NoError(t, err)
	remoteOp, err := model.NewAddOperation(remoteDeviceID, "item-remote", "remote", time.Unix(50, 0).UTC())
	require.NoError(t, err)
	require.NoError(t, remoteLog.Append(remoteOp))

	localDir := t.TempDir()
	localLog, err := store.NewOpsLog(localDir)
	require.NoError(t, err)
	localDeviceID, err := store.EnsureDeviceID(localDir)
	require.NoError(t, err)
	localSeed, err := model.NewAddOperation(localDeviceID, "item-local", "local", time.Unix(51, 0).UTC())
	require.NoError(t, err)
	require.NoError(t, localLog.Append(localSeed))

	beforeLines, err := localLog.ReadEncryptedLines()
	require.NoError(t, err)

	svc, err := server.New(server.Config{DataDir: remoteDir})
	require.NoError(t, err)
	ts := httptest.NewServer(svc.Handler())
	defer ts.Close()

	client := New(nil)
	_, err = client.PullAndImport(context.Background(), ts.URL, localLog)
	require.Error(t, err)

	afterLines, err := localLog.ReadEncryptedLines()
	require.NoError(t, err)
	require.Equal(t, beforeLines, afterLines)
}

func mustReadAll(t *testing.T, log *store.OpsLog) []model.Operation {
	t.Helper()
	ops, err := log.ReadAll()
	require.NoError(t, err)
	return ops
}

func copySharedKey(t *testing.T, fromDataDir, toDataDir string) {
	t.Helper()
	keyBytes, err := os.ReadFile(filepath.Join(fromDataDir, crypto.KeyFileName))
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(toDataDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(toDataDir, crypto.KeyFileName), keyBytes, 0o600))
}
