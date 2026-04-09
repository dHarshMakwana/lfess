package cmd

import (
	"bytes"
	"fmt"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dHarshMakwana/lfess/internal/crypto"
	"github.com/dHarshMakwana/lfess/internal/model"
	"github.com/dHarshMakwana/lfess/internal/store"
	"github.com/dHarshMakwana/lfess/server"
	"github.com/stretchr/testify/require"
)

func TestSyncCommand_PullsFromPeer(t *testing.T) {
	remoteDir := t.TempDir()
	remoteLog, err := store.NewOpsLog(remoteDir)
	require.NoError(t, err)
	remoteDeviceID, err := store.EnsureDeviceID(remoteDir)
	require.NoError(t, err)
	op, err := model.NewAddOperation(remoteDeviceID, "item-1", "from-peer", time.Unix(100, 0).UTC())
	require.NoError(t, err)
	require.NoError(t, remoteLog.Append(op))

	localDir := t.TempDir()
	copySharedKeyForSyncCmd(t, remoteDir, localDir)
	_, err = store.NewOpsLog(localDir)
	require.NoError(t, err)

	svc, err := server.New(server.Config{DataDir: remoteDir})
	require.NoError(t, err)
	ts := httptest.NewServer(svc.Handler())
	defer ts.Close()

	oldDataDir := dataDir
	dataDir = localDir
	t.Cleanup(func() { dataDir = oldDataDir })

	cmd := newSyncCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{ts.URL})

	err = cmd.Execute()
	require.NoError(t, err)
	require.Contains(t, out.String(), "imported 1 operations")

	localLog, err := store.NewOpsLog(localDir)
	require.NoError(t, err)
	ops, err := localLog.ReadAll()
	require.NoError(t, err)
	require.Len(t, ops, 1)
	require.Equal(t, op.OperationID, ops[0].OperationID)
}

func TestSyncCommand_RejectsNonExplicitAddress(t *testing.T) {
	oldDataDir := dataDir
	dataDir = t.TempDir()
	t.Cleanup(func() { dataDir = oldDataDir })

	cmd := newSyncCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"127.0.0.1:7777"})

	err := cmd.Execute()
	require.Error(t, err)
}

func TestSyncCommand_UnreachablePeerReturnsError(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	require.NoError(t, ln.Close())

	oldDataDir := dataDir
	dataDir = t.TempDir()
	t.Cleanup(func() { dataDir = oldDataDir })

	cmd := newSyncCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{fmt.Sprintf("http://%s", addr)})

	err = cmd.Execute()
	require.Error(t, err)
}

func copySharedKeyForSyncCmd(t *testing.T, fromDataDir, toDataDir string) {
	t.Helper()
	keyBytes, err := os.ReadFile(filepath.Join(fromDataDir, crypto.KeyFileName))
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(toDataDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(toDataDir, crypto.KeyFileName), keyBytes, 0o600))
}
