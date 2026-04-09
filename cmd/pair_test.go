package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
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

func TestPairStartCommand_Success(t *testing.T) {
	svc, err := server.New(server.Config{DataDir: t.TempDir()})
	require.NoError(t, err)
	ts := httptest.NewServer(svc.Handler())
	defer ts.Close()

	cmd := newPairStartCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--server", ts.URL, "--ttl", "2m"})

	err = cmd.Execute()
	require.NoError(t, err)

	text := out.String()
	require.Contains(t, text, "Pair code:")
	require.Contains(t, text, "Session ID:")
	require.Contains(t, text, "Expires at:")
}

func TestPairStartCommand_InvalidServerAddress(t *testing.T) {
	cmd := newPairStartCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--server", "127.0.0.1:7777", "--ttl", "2m"})

	err := cmd.Execute()
	require.Error(t, err)
}

func TestPairJoinCommand_SuccessInstallsKey0600(t *testing.T) {
	trustedDir := t.TempDir()
	svc, err := server.New(server.Config{DataDir: trustedDir})
	require.NoError(t, err)
	ts := httptest.NewServer(svc.Handler())
	defer ts.Close()

	pairCode := mustStartPairSession(t, ts.URL, 2*time.Minute)

	joinDir := t.TempDir()
	oldDataDir := dataDir
	dataDir = joinDir
	t.Cleanup(func() { dataDir = oldDataDir })

	cmd := newPairJoinCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{ts.URL, "--code", pairCode})

	err = cmd.Execute()
	require.NoError(t, err)
	require.Contains(t, out.String(), "bootstrap completed")

	trustedKey, err := os.ReadFile(filepath.Join(trustedDir, crypto.KeyFileName))
	require.NoError(t, err)
	joinedKey, err := os.ReadFile(filepath.Join(joinDir, crypto.KeyFileName))
	require.NoError(t, err)
	require.Equal(t, string(bytes.TrimSpace(trustedKey)), string(bytes.TrimSpace(joinedKey)))

	st, err := os.Stat(filepath.Join(joinDir, crypto.KeyFileName))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), st.Mode().Perm())
}

func TestPairJoinCommand_InvalidCodeFails(t *testing.T) {
	oldDataDir := dataDir
	dataDir = t.TempDir()
	t.Cleanup(func() { dataDir = oldDataDir })

	cmd := newPairJoinCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"http://127.0.0.1:7777", "--code", "invalid"})

	err := cmd.Execute()
	require.Error(t, err)
}

func TestPairJoinCommand_RejectsNonExplicitPeerAddress(t *testing.T) {
	oldDataDir := dataDir
	dataDir = t.TempDir()
	t.Cleanup(func() { dataDir = oldDataDir })

	cmd := newPairJoinCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"127.0.0.1:7777", "--code", "a.b"})

	err := cmd.Execute()
	require.Error(t, err)
}

func TestPairJoinThenSync_WorksWithoutManualKeyCopy(t *testing.T) {
	trustedDir := t.TempDir()
	trustedLog, err := store.NewOpsLog(trustedDir)
	require.NoError(t, err)
	trustedDeviceID, err := store.EnsureDeviceID(trustedDir)
	require.NoError(t, err)
	op, err := model.NewAddOperation(trustedDeviceID, "item-1", "from-trusted", time.Unix(200, 0).UTC())
	require.NoError(t, err)
	require.NoError(t, trustedLog.Append(op))

	svc, err := server.New(server.Config{DataDir: trustedDir})
	require.NoError(t, err)
	ts := httptest.NewServer(svc.Handler())
	defer ts.Close()

	pairCode := mustStartPairSession(t, ts.URL, 2*time.Minute)

	joinDir := t.TempDir()
	oldDataDir := dataDir
	dataDir = joinDir
	t.Cleanup(func() { dataDir = oldDataDir })

	joinCmd := newPairJoinCmd()
	joinCmd.SetOut(&bytes.Buffer{})
	joinCmd.SetErr(&bytes.Buffer{})
	joinCmd.SetArgs([]string{ts.URL, "--code", pairCode})
	require.NoError(t, joinCmd.Execute())

	syncCmd := newSyncCmd()
	syncOut := &bytes.Buffer{}
	syncCmd.SetOut(syncOut)
	syncCmd.SetErr(&bytes.Buffer{})
	syncCmd.SetArgs([]string{ts.URL})
	require.NoError(t, syncCmd.Execute())
	require.Contains(t, syncOut.String(), "imported 1 operations")

	joinedLog, err := store.NewOpsLog(joinDir)
	require.NoError(t, err)
	ops, err := joinedLog.ReadAll()
	require.NoError(t, err)
	require.Len(t, ops, 1)
	require.Equal(t, op.OperationID, ops[0].OperationID)
}

func mustStartPairSession(t *testing.T, baseURL string, ttl time.Duration) string {
	t.Helper()

	payload, err := json.Marshal(map[string]int64{"ttl_seconds": int64(ttl / time.Second)})
	require.NoError(t, err)
	resp, err := http.Post(baseURL+"/bootstrap/session", "application/json", bytes.NewReader(payload))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var out struct {
		PairCode string `json:"pair_code"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	require.NotEmpty(t, out.PairCode)
	return out.PairCode
}
