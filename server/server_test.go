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

func TestOps_PostRejectsTrailingJSON(t *testing.T) {
	localDir := t.TempDir()
	localLog, err := store.NewOpsLog(localDir)
	require.NoError(t, err)
	deviceID, err := store.EnsureDeviceID(localDir)
	require.NoError(t, err)
	op, err := model.NewAddOperation(deviceID, "item-1", "hello", time.Unix(40, 0).UTC())
	require.NoError(t, err)
	require.NoError(t, localLog.Append(op))

	lines, err := localLog.ReadEncryptedLines()
	require.NoError(t, err)
	body, err := json.Marshal(lines)
	require.NoError(t, err)
	body = append(body, []byte(` {"extra":true}`)...)

	svc, err := New(Config{DataDir: localDir})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/ops", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	svc.Handler().ServeHTTP(rr, req)

	require.Equal(t, http.StatusBadRequest, rr.Code)
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
