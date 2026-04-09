package cmd

import (
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	lfcrypto "github.com/dHarshMakwana/lfess/internal/crypto"
	"github.com/dHarshMakwana/lfess/internal/engine"
	"github.com/dHarshMakwana/lfess/internal/model"
	"github.com/dHarshMakwana/lfess/internal/store"
	"github.com/dHarshMakwana/lfess/server"
	"github.com/stretchr/testify/require"
)

func TestPhase9_CLIBootstrapThenSync_StrictConvergenceAndIdempotency(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()

	addAOut, err := runRootCommand(t, dirA, "add", "task-from-a")
	require.NoError(t, err)
	itemA := strings.TrimSpace(addAOut)
	require.NotEmpty(t, itemA)

	svcA, err := server.New(server.Config{DataDir: dirA})
	require.NoError(t, err)
	tsA := httptest.NewServer(svcA.Handler())
	defer tsA.Close()

	pairStartOut, err := runRootCommand(t, dirA, "pair", "start", "--server", tsA.URL, "--ttl", "2m")
	require.NoError(t, err)
	pairCode := extractPairCode(pairStartOut)
	require.NotEmpty(t, pairCode)

	_, err = os.Stat(filepath.Join(dirB, lfcrypto.KeyFileName))
	require.True(t, errors.Is(err, os.ErrNotExist))

	joinOut, err := runRootCommand(t, dirB, "pair", "join", tsA.URL, "--code", pairCode)
	require.NoError(t, err)
	require.Contains(t, joinOut, "bootstrap completed")

	keyA, err := os.ReadFile(filepath.Join(dirA, lfcrypto.KeyFileName))
	require.NoError(t, err)
	keyB, err := os.ReadFile(filepath.Join(dirB, lfcrypto.KeyFileName))
	require.NoError(t, err)
	require.Equal(t, strings.TrimSpace(string(keyA)), strings.TrimSpace(string(keyB)))

	st, err := os.Stat(filepath.Join(dirB, lfcrypto.KeyFileName))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), st.Mode().Perm())

	addBOut, err := runRootCommand(t, dirB, "add", "task-from-b")
	require.NoError(t, err)
	itemB := strings.TrimSpace(addBOut)
	require.NotEmpty(t, itemB)
	require.NotEqual(t, itemA, itemB)

	svcB, err := server.New(server.Config{DataDir: dirB})
	require.NoError(t, err)
	tsB := httptest.NewServer(svcB.Handler())
	defer tsB.Close()

	syncB1Out, err := runRootCommand(t, dirB, "sync", tsA.URL)
	require.NoError(t, err)
	require.Contains(t, syncB1Out, "imported 1 operations")

	syncA1Out, err := runRootCommand(t, dirA, "sync", tsB.URL)
	require.NoError(t, err)
	require.Contains(t, syncA1Out, "imported 1 operations")

	listA1, err := runRootCommand(t, dirA, "list")
	require.NoError(t, err)
	listB1, err := runRootCommand(t, dirB, "list")
	require.NoError(t, err)
	require.Contains(t, listA1, itemA+"\ttask-from-a")
	require.Contains(t, listA1, itemB+"\ttask-from-b")
	require.Contains(t, listB1, itemA+"\ttask-from-a")
	require.Contains(t, listB1, itemB+"\ttask-from-b")

	logABeforeNoop := mustReadOpsLogBytes(t, dirA)
	logBBeforeNoop := mustReadOpsLogBytes(t, dirB)

	syncB2Out, err := runRootCommand(t, dirB, "sync", tsA.URL)
	require.NoError(t, err)
	require.Contains(t, syncB2Out, "imported 0 operations")

	syncA2Out, err := runRootCommand(t, dirA, "sync", tsB.URL)
	require.NoError(t, err)
	require.Contains(t, syncA2Out, "imported 0 operations")

	require.Equal(t, logABeforeNoop, mustReadOpsLogBytes(t, dirA))
	require.Equal(t, logBBeforeNoop, mustReadOpsLogBytes(t, dirB))

	_, err = runRootCommand(t, dirA, "delete", itemA)
	require.NoError(t, err)
	_, err = runRootCommand(t, dirB, "update", itemA, "updated-on-b")
	require.NoError(t, err)

	syncB3Out, err := runRootCommand(t, dirB, "sync", tsA.URL)
	require.NoError(t, err)
	require.Contains(t, syncB3Out, "imported 1 operations")

	syncA3Out, err := runRootCommand(t, dirA, "sync", tsB.URL)
	require.NoError(t, err)
	require.Contains(t, syncA3Out, "imported 1 operations")

	listA2, err := runRootCommand(t, dirA, "list")
	require.NoError(t, err)
	listB2, err := runRootCommand(t, dirB, "list")
	require.NoError(t, err)
	require.NotContains(t, listA2, itemA+"\t")
	require.NotContains(t, listB2, itemA+"\t")
	require.Contains(t, listA2, itemB+"\ttask-from-b")
	require.Contains(t, listB2, itemB+"\ttask-from-b")

	stateA := mustReplayState(t, dirA)
	stateB := mustReplayState(t, dirB)
	require.Equal(t, stateA, stateB)
	require.Contains(t, stateA, itemA)
	require.True(t, stateA[itemA].Deleted)
	require.Empty(t, stateA[itemA].Content)
}

func extractPairCode(out string) string {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "Pair code:") {
			continue
		}
		return strings.TrimSpace(strings.TrimPrefix(line, "Pair code:"))
	}
	return ""
}

func mustReadOpsLogBytes(t *testing.T, dataDir string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dataDir, "ops.log"))
	require.NoError(t, err)
	return b
}

func mustReplayState(t *testing.T, dataDir string) map[string]model.Item {
	t.Helper()
	log, err := store.NewOpsLog(dataDir)
	require.NoError(t, err)
	ops, err := log.ReadAll()
	require.NoError(t, err)
	return engine.Replay(ops)
}
