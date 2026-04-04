package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/dHarshMakwana/lfess/internal/model"
	"github.com/dHarshMakwana/lfess/internal/store"
	"github.com/stretchr/testify/require"
)

func TestNew_DefaultsAndTimeouts(t *testing.T) {
	svc, err := New(Config{DataDir: t.TempDir()})
	require.NoError(t, err)
	require.Equal(t, "127.0.0.1:7777", svc.HTTPServer().Addr)
	require.Equal(t, 10*time.Second, svc.HTTPServer().ReadTimeout)
	require.Equal(t, 10*time.Second, svc.HTTPServer().WriteTimeout)
}

func TestNew_HostAndPortConfigurable(t *testing.T) {
	svc, err := New(Config{DataDir: t.TempDir(), Host: "0.0.0.0", Port: 8899})
	require.NoError(t, err)
	require.Equal(t, "0.0.0.0:8899", svc.HTTPServer().Addr)
}

func TestHealth_Endpoint(t *testing.T) {
	svc, err := New(Config{DataDir: t.TempDir()})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()
	svc.Handler().ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	require.Equal(t, "ok", rr.Body.String())
}

func TestDemo_Endpoint(t *testing.T) {
	svc, err := New(Config{DataDir: t.TempDir()})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/demo", nil)
	rr := httptest.NewRecorder()
	svc.Handler().ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	require.Contains(t, rr.Header().Get("Content-Type"), "text/html")
	require.Contains(t, rr.Body.String(), "LFESS Local-First Sync Demo")
}

func TestNotes_CRUDLifecycle(t *testing.T) {
	svc, err := New(Config{DataDir: t.TempDir()})
	require.NoError(t, err)

	createReq := httptest.NewRequest(http.MethodPost, "/notes", bytes.NewReader([]byte(`{"content":"hello"}`)))
	createRR := httptest.NewRecorder()
	svc.Handler().ServeHTTP(createRR, createReq)
	require.Equal(t, http.StatusCreated, createRR.Code)

	var created map[string]model.Item
	require.NoError(t, json.Unmarshal(createRR.Body.Bytes(), &created))
	createdItem := created["item"]
	require.NotEmpty(t, createdItem.ID)
	require.Equal(t, "hello", createdItem.Content)
	require.False(t, createdItem.Deleted)

	getReq := httptest.NewRequest(http.MethodGet, "/notes", nil)
	getRR := httptest.NewRecorder()
	svc.Handler().ServeHTTP(getRR, getReq)
	require.Equal(t, http.StatusOK, getRR.Code)

	var notes []model.Item
	require.NoError(t, json.Unmarshal(getRR.Body.Bytes(), &notes))
	require.Len(t, notes, 1)
	require.Equal(t, createdItem.ID, notes[0].ID)
	require.Equal(t, "hello", notes[0].Content)
	require.False(t, notes[0].Deleted)

	updateReq := httptest.NewRequest(http.MethodPut, "/notes/"+createdItem.ID, bytes.NewReader([]byte(`{"content":"hello edited"}`)))
	updateRR := httptest.NewRecorder()
	svc.Handler().ServeHTTP(updateRR, updateReq)
	require.Equal(t, http.StatusOK, updateRR.Code)

	var updated map[string]model.Item
	require.NoError(t, json.Unmarshal(updateRR.Body.Bytes(), &updated))
	require.Equal(t, createdItem.ID, updated["item"].ID)
	require.Equal(t, "hello edited", updated["item"].Content)
	require.False(t, updated["item"].Deleted)

	deleteReq := httptest.NewRequest(http.MethodDelete, "/notes/"+createdItem.ID, nil)
	deleteRR := httptest.NewRecorder()
	svc.Handler().ServeHTTP(deleteRR, deleteReq)
	require.Equal(t, http.StatusOK, deleteRR.Code)

	var deleted map[string]model.Item
	require.NoError(t, json.Unmarshal(deleteRR.Body.Bytes(), &deleted))
	require.Equal(t, createdItem.ID, deleted["item"].ID)
	require.True(t, deleted["item"].Deleted)

	getRR2 := httptest.NewRecorder()
	svc.Handler().ServeHTTP(getRR2, getReq)
	require.Equal(t, http.StatusOK, getRR2.Code)
	require.NoError(t, json.Unmarshal(getRR2.Body.Bytes(), &notes))
	require.Len(t, notes, 1)
	require.Equal(t, createdItem.ID, notes[0].ID)
	require.True(t, notes[0].Deleted)
}

func TestNotes_OptionsPreflight(t *testing.T) {
	svc, err := New(Config{DataDir: t.TempDir()})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodOptions, "/notes", nil)
	rr := httptest.NewRecorder()
	svc.Handler().ServeHTTP(rr, req)

	require.Equal(t, http.StatusNoContent, rr.Code)
	require.Equal(t, "*", rr.Header().Get("Access-Control-Allow-Origin"))
	require.Equal(t, "GET, POST, OPTIONS", rr.Header().Get("Access-Control-Allow-Methods"))
	require.Equal(t, "Content-Type", rr.Header().Get("Access-Control-Allow-Headers"))
}

func TestNoteByID_OptionsPreflight(t *testing.T) {
	svc, err := New(Config{DataDir: t.TempDir()})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodOptions, "/notes/example", nil)
	rr := httptest.NewRecorder()
	svc.Handler().ServeHTTP(rr, req)

	require.Equal(t, http.StatusNoContent, rr.Code)
	require.Equal(t, "*", rr.Header().Get("Access-Control-Allow-Origin"))
	require.Equal(t, "GET, PUT, DELETE, OPTIONS", rr.Header().Get("Access-Control-Allow-Methods"))
	require.Equal(t, "Content-Type", rr.Header().Get("Access-Control-Allow-Headers"))
}

func TestIdentity_Endpoint(t *testing.T) {
	svc, err := New(Config{DataDir: t.TempDir()})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/identity", nil)
	rr := httptest.NewRecorder()
	svc.Handler().ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	var resp map[string]string
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	require.NotEmpty(t, resp["identity"])
}

func TestConnect_OptionsPreflight(t *testing.T) {
	svc, err := New(Config{DataDir: t.TempDir()})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodOptions, "/connect", nil)
	rr := httptest.NewRecorder()
	svc.Handler().ServeHTTP(rr, req)

	require.Equal(t, http.StatusNoContent, rr.Code)
	require.Equal(t, "*", rr.Header().Get("Access-Control-Allow-Origin"))
	require.Equal(t, "POST, OPTIONS", rr.Header().Get("Access-Control-Allow-Methods"))
	require.Equal(t, "Content-Type", rr.Header().Get("Access-Control-Allow-Headers"))
}

func TestConnect_PairsNodesAutomatically(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()

	svcA, err := New(Config{DataDir: dirA})
	require.NoError(t, err)
	svcB, err := New(Config{DataDir: dirB})
	require.NoError(t, err)

	tsA := httptest.NewServer(svcA.Handler())
	defer tsA.Close()
	tsB := httptest.NewServer(svcB.Handler())
	defer tsB.Close()

	createNoteViaHTTP(t, tsA.URL, "hello from A")
	createNoteViaHTTP(t, tsB.URL, "hello from B")

	connectPeerViaHTTP(t, tsA.URL, tsB.URL)
	connectPeerViaHTTP(t, tsB.URL, tsA.URL)

	statusA, importedA, _ := checklistPostImport(t, svcA.Handler(), checklistReadLines(t, dirB))
	require.Equal(t, http.StatusOK, statusA)
	require.Equal(t, 1, importedA)

	statusB, importedB, _ := checklistPostImport(t, svcB.Handler(), checklistReadLines(t, dirA))
	require.Equal(t, http.StatusOK, statusB)
	require.Equal(t, 1, importedB)

	itemsA := getNotesViaHTTP(t, tsA.URL)
	itemsB := getNotesViaHTTP(t, tsB.URL)
	require.Len(t, itemsA, 2)
	require.Len(t, itemsB, 2)
}

func TestOps_GetReturnsCiphertextsInAppendOrder(t *testing.T) {
	dir := t.TempDir()
	log, err := store.NewOpsLog(dir)
	require.NoError(t, err)

	deviceID, err := store.EnsureDeviceID(dir)
	require.NoError(t, err)

	op1, err := model.NewAddOperation(deviceID, "item-1", "hello", time.Unix(10, 0).UTC())
	require.NoError(t, err)
	op2, err := model.NewUpdateOperation(deviceID, "item-1", "world", time.Unix(11, 0).UTC())
	require.NoError(t, err)
	require.NoError(t, log.Append(op1))
	require.NoError(t, log.Append(op2))

	expectedLines, err := log.ReadEncryptedLines()
	require.NoError(t, err)
	require.Len(t, expectedLines, 2)

	svc, err := New(Config{DataDir: dir})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/ops", nil)
	rr := httptest.NewRecorder()
	svc.Handler().ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	var got []string
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &got))
	require.Equal(t, expectedLines, got)
}

func TestOps_GetMissingLogReturnsEmptyArray(t *testing.T) {
	svc, err := New(Config{DataDir: t.TempDir()})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/ops", nil)
	rr := httptest.NewRecorder()
	svc.Handler().ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	var got []string
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &got))
	require.Len(t, got, 0)
}

func TestOps_MethodNotAllowed(t *testing.T) {
	svc, err := New(Config{DataDir: t.TempDir()})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPut, "/ops", nil)
	rr := httptest.NewRecorder()
	svc.Handler().ServeHTTP(rr, req)

	require.Equal(t, http.StatusMethodNotAllowed, rr.Code)
	require.Equal(t, "GET, POST", rr.Header().Get("Allow"))
}

func TestOps_OptionsPreflight(t *testing.T) {
	svc, err := New(Config{DataDir: t.TempDir()})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodOptions, "/ops", nil)
	rr := httptest.NewRecorder()
	svc.Handler().ServeHTTP(rr, req)

	require.Equal(t, http.StatusNoContent, rr.Code)
	require.Equal(t, "*", rr.Header().Get("Access-Control-Allow-Origin"))
	require.Equal(t, "GET, POST, OPTIONS", rr.Header().Get("Access-Control-Allow-Methods"))
	require.Equal(t, "Content-Type", rr.Header().Get("Access-Control-Allow-Headers"))
}

func TestOps_PostImportsViaMergeFlow(t *testing.T) {
	localDir := t.TempDir()
	localLog, err := store.NewOpsLog(localDir)
	require.NoError(t, err)
	deviceID, err := store.EnsureDeviceID(localDir)
	require.NoError(t, err)

	localAdd, err := model.NewAddOperation(deviceID, "item-1", "v1", time.Unix(20, 0).UTC())
	require.NoError(t, err)
	require.NoError(t, localLog.Append(localAdd))

	remoteDir := t.TempDir()
	copySharedKey(t, localDir, remoteDir)
	remoteLog, err := store.NewOpsLog(remoteDir)
	require.NoError(t, err)
	require.NoError(t, remoteLog.Append(localAdd)) // duplicate operation id
	remoteUpdate, err := model.NewUpdateOperation(deviceID, "item-1", "v2", time.Unix(21, 0).UTC())
	require.NoError(t, err)
	require.NoError(t, remoteLog.Append(remoteUpdate))

	payloadLines, err := remoteLog.ReadEncryptedLines()
	require.NoError(t, err)

	// Ensure crypto points to local data dir before server import handling.
	svc, err := New(Config{DataDir: localDir})
	require.NoError(t, err)

	body, err := json.Marshal(payloadLines)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/ops", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	svc.Handler().ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)
	require.Equal(t, "*", rr.Header().Get("Access-Control-Allow-Origin"))

	var resp map[string]int
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	require.Equal(t, 1, resp["imported"])

	ops, err := localLog.ReadAll()
	require.NoError(t, err)
	require.Len(t, ops, 2)
	require.Equal(t, localAdd.OperationID, ops[0].OperationID)
	require.Equal(t, remoteUpdate.OperationID, ops[1].OperationID)

	// Re-importing same payload should be idempotent.
	req2 := httptest.NewRequest(http.MethodPost, "/ops", bytes.NewReader(body))
	rr2 := httptest.NewRecorder()
	svc.Handler().ServeHTTP(rr2, req2)
	require.Equal(t, http.StatusOK, rr2.Code)
	var resp2 map[string]int
	require.NoError(t, json.Unmarshal(rr2.Body.Bytes(), &resp2))
	require.Equal(t, 0, resp2["imported"])
}

func TestOps_PostInvalidPayloadReturnsBadRequest(t *testing.T) {
	svc, err := New(Config{DataDir: t.TempDir()})
	require.NoError(t, err)

	t.Run("invalid-json-shape", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/ops", bytes.NewBufferString(`{"ops":[]}`))
		rr := httptest.NewRecorder()
		svc.Handler().ServeHTTP(rr, req)
		require.Equal(t, http.StatusBadRequest, rr.Code)
	})

	t.Run("invalid-encrypted-line", func(t *testing.T) {
		payload, err := json.Marshal([]string{"not-base64"})
		require.NoError(t, err)
		req := httptest.NewRequest(http.MethodPost, "/ops", bytes.NewReader(payload))
		rr := httptest.NewRecorder()
		svc.Handler().ServeHTTP(rr, req)
		require.Equal(t, http.StatusBadRequest, rr.Code)
	})
}

func TestOps_ConcurrentRequests(t *testing.T) {
	localDir := t.TempDir()
	localLog, err := store.NewOpsLog(localDir)
	require.NoError(t, err)
	deviceID, err := store.EnsureDeviceID(localDir)
	require.NoError(t, err)

	seedOp, err := model.NewAddOperation(deviceID, "seed", "seed", time.Unix(30, 0).UTC())
	require.NoError(t, err)
	require.NoError(t, localLog.Append(seedOp))

	remoteDir := t.TempDir()
	copySharedKey(t, localDir, remoteDir)
	remoteLog, err := store.NewOpsLog(remoteDir)
	require.NoError(t, err)
	remoteOp, err := model.NewAddOperation(deviceID, "remote", "from-peer", time.Unix(31, 0).UTC())
	require.NoError(t, err)
	require.NoError(t, remoteLog.Append(remoteOp))
	lines, err := remoteLog.ReadEncryptedLines()
	require.NoError(t, err)
	payload, err := json.Marshal(lines)
	require.NoError(t, err)

	svc, err := New(Config{DataDir: localDir})
	require.NoError(t, err)

	ts := httptest.NewServer(svc.Handler())
	defer ts.Close()

	const workers = 30
	var wg sync.WaitGroup
	errCh := make(chan error, workers*2)

	for i := 0; i < workers; i++ {
		wg.Add(2)

		go func() {
			defer wg.Done()
			resp, err := http.Get(ts.URL + "/ops")
			if err != nil {
				errCh <- err
				return
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				errCh <- fmt.Errorf("unexpected /ops GET status: %d", resp.StatusCode)
				return
			}
			_, err = io.ReadAll(resp.Body)
			if err != nil {
				errCh <- err
			}
		}()

		go func() {
			defer wg.Done()
			resp, err := http.Post(ts.URL+"/ops", "application/json", bytes.NewReader(payload))
			if err != nil {
				errCh <- err
				return
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				errCh <- fmt.Errorf("unexpected /ops POST status: %d", resp.StatusCode)
				return
			}
			_, err = io.ReadAll(resp.Body)
			if err != nil {
				errCh <- err
			}
		}()
	}

	wg.Wait()
	close(errCh)
	for err := range errCh {
		require.NoError(t, err)
	}
}

func TestRun_GracefulShutdownOnSignal(t *testing.T) {
	port := freeTCPPort(t)
	dir := t.TempDir()
	svc, err := New(Config{DataDir: dir, Host: "127.0.0.1", Port: port, ShutdownTimeout: 2 * time.Second})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigCh := make(chan os.Signal, 1)
	done := make(chan error, 1)

	go func() {
		done <- svc.run(ctx, sigCh)
	}()

	waitForHealthy(t, fmt.Sprintf("http://127.0.0.1:%d/health", port), 3*time.Second)

	sigCh <- syscall.SIGTERM
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for graceful shutdown")
	}
}

func TestRun_GracefulShutdownWaitsForInflightRequest(t *testing.T) {
	port := freeTCPPort(t)
	dir := t.TempDir()
	svc, err := New(Config{DataDir: dir, Host: "127.0.0.1", Port: port, ShutdownTimeout: 2 * time.Second})
	require.NoError(t, err)

	originalHandler := svc.server.Handler
	requestStarted := make(chan struct{})
	releaseRequest := make(chan struct{})

	svc.server.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ops" && r.Method == http.MethodGet {
			select {
			case <-requestStarted:
			default:
				close(requestStarted)
			}
			<-releaseRequest
		}
		originalHandler.ServeHTTP(w, r)
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigCh := make(chan os.Signal, 1)
	done := make(chan error, 1)

	go func() {
		done <- svc.run(ctx, sigCh)
	}()

	waitForHealthy(t, fmt.Sprintf("http://127.0.0.1:%d/health", port), 3*time.Second)

	respCh := make(chan *http.Response, 1)
	errCh := make(chan error, 1)
	go func() {
		resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/ops", port))
		if err != nil {
			errCh <- err
			return
		}
		respCh <- resp
	}()

	select {
	case <-requestStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for in-flight request to start")
	}

	sigCh <- syscall.SIGTERM

	select {
	case err := <-done:
		require.NoError(t, err)
		t.Fatal("server exited before in-flight request completed")
	case <-time.After(100 * time.Millisecond):
		// Expected: shutdown is waiting for in-flight request completion.
	}

	close(releaseRequest)

	var resp *http.Response
	select {
	case err := <-errCh:
		require.NoError(t, err)
	case resp = <-respCh:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for in-flight request response")
	}

	require.NotNil(t, resp)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.NoError(t, resp.Body.Close())

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for graceful shutdown")
	}
}

func copySharedKey(t *testing.T, fromDataDir, toDataDir string) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(fromDataDir, "key.age"))
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(toDataDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(toDataDir, "key.age"), b, 0o600))
}

func freeTCPPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func waitForHealthy(t *testing.T, url string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("server did not become healthy at %s", url)
}

func createNoteViaHTTP(t *testing.T, baseURL, content string) model.Item {
	t.Helper()
	payload, err := json.Marshal(map[string]string{"content": content})
	require.NoError(t, err)

	resp, err := http.Post(baseURL+"/notes", "application/json", bytes.NewReader(payload))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	var out map[string]model.Item
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	return out["item"]
}

func connectPeerViaHTTP(t *testing.T, targetBaseURL, peerBaseURL string) {
	t.Helper()
	payload, err := json.Marshal(map[string]string{"peer_base_url": peerBaseURL})
	require.NoError(t, err)

	resp, err := http.Post(targetBaseURL+"/connect", "application/json", bytes.NewReader(payload))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
}

func getNotesViaHTTP(t *testing.T, baseURL string) []model.Item {
	t.Helper()
	resp, err := http.Get(baseURL + "/notes")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var items []model.Item
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&items))
	return items
}
