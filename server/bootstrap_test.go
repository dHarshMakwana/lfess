package server

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"filippo.io/age"

	"github.com/dHarshMakwana/lfess/internal/bootstrap"
	"github.com/dHarshMakwana/lfess/internal/crypto"
	"github.com/stretchr/testify/require"
)

func TestBootstrapSessionAndExchange_Success(t *testing.T) {
	dir := t.TempDir()
	svc, err := New(Config{DataDir: dir})
	require.NoError(t, err)

	startReqBody := bytes.NewBufferString(`{"ttl_seconds":120}`)
	startReq := httptest.NewRequest(http.MethodPost, "/bootstrap/session", startReqBody)
	startRR := httptest.NewRecorder()
	svc.Handler().ServeHTTP(startRR, startReq)
	require.Equal(t, http.StatusOK, startRR.Code)

	var startResp struct {
		SessionID string    `json:"session_id"`
		PairCode  string    `json:"pair_code"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	require.NoError(t, json.Unmarshal(startRR.Body.Bytes(), &startResp))
	require.NotEmpty(t, startResp.SessionID)
	require.NotEmpty(t, startResp.PairCode)
	require.True(t, startResp.ExpiresAt.After(time.Now().UTC()))

	sessionID, codeSecret, err := bootstrap.ParsePairCode(startResp.PairCode)
	require.NoError(t, err)
	require.Equal(t, startResp.SessionID, sessionID)

	proof, err := bootstrap.ComputeProof(sessionID, codeSecret)
	require.NoError(t, err)
	ephemeralID, err := age.GenerateX25519Identity()
	require.NoError(t, err)

	exchangePayload, err := json.Marshal(map[string]string{
		"session_id":          sessionID,
		"proof":               proof,
		"ephemeral_recipient": ephemeralID.Recipient().String(),
	})
	require.NoError(t, err)

	exchangeReq := httptest.NewRequest(http.MethodPost, "/bootstrap/exchange", bytes.NewReader(exchangePayload))
	exchangeRR := httptest.NewRecorder()
	svc.Handler().ServeHTTP(exchangeRR, exchangeReq)
	require.Equal(t, http.StatusOK, exchangeRR.Code)

	var exchangeResp struct {
		Payload string `json:"payload"`
	}
	require.NoError(t, json.Unmarshal(exchangeRR.Body.Bytes(), &exchangeResp))
	require.NotEmpty(t, exchangeResp.Payload)

	ciphertext, err := base64.StdEncoding.DecodeString(exchangeResp.Payload)
	require.NoError(t, err)
	r, err := age.Decrypt(bytes.NewReader(ciphertext), ephemeralID)
	require.NoError(t, err)
	plain, err := ioReadAll(r)
	require.NoError(t, err)

	storedKey, err := os.ReadFile(filepath.Join(dir, crypto.KeyFileName))
	require.NoError(t, err)
	require.Equal(t, string(bytes.TrimSpace(storedKey)), string(bytes.TrimSpace(plain)))

	replayReq := httptest.NewRequest(http.MethodPost, "/bootstrap/exchange", bytes.NewReader(exchangePayload))
	replayRR := httptest.NewRecorder()
	svc.Handler().ServeHTTP(replayRR, replayReq)
	require.Equal(t, http.StatusUnauthorized, replayRR.Code)
}

func TestBootstrapSession_InvalidTTLRejected(t *testing.T) {
	svc, err := New(Config{DataDir: t.TempDir()})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/bootstrap/session", bytes.NewBufferString(`{"ttl_seconds":0}`))
	rr := httptest.NewRecorder()
	svc.Handler().ServeHTTP(rr, req)

	require.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestBootstrapExchange_RateLimitedAfterFailedProofs(t *testing.T) {
	dir := t.TempDir()
	svc, err := New(Config{DataDir: dir})
	require.NoError(t, err)

	startReq := httptest.NewRequest(http.MethodPost, "/bootstrap/session", bytes.NewBufferString(`{"ttl_seconds":120}`))
	startRR := httptest.NewRecorder()
	svc.Handler().ServeHTTP(startRR, startReq)
	require.Equal(t, http.StatusOK, startRR.Code)

	var startResp struct {
		SessionID string `json:"session_id"`
	}
	require.NoError(t, json.Unmarshal(startRR.Body.Bytes(), &startResp))
	id, err := age.GenerateX25519Identity()
	require.NoError(t, err)

	for i := 0; i < 4; i++ {
		badPayload, err := json.Marshal(map[string]string{
			"session_id":          startResp.SessionID,
			"proof":               "bad-proof",
			"ephemeral_recipient": id.Recipient().String(),
		})
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/bootstrap/exchange", bytes.NewReader(badPayload))
		rr := httptest.NewRecorder()
		svc.Handler().ServeHTTP(rr, req)
		require.Equal(t, http.StatusUnauthorized, rr.Code)
	}

	badPayload, err := json.Marshal(map[string]string{
		"session_id":          startResp.SessionID,
		"proof":               "bad-proof",
		"ephemeral_recipient": id.Recipient().String(),
	})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/bootstrap/exchange", bytes.NewReader(badPayload))
	rr := httptest.NewRecorder()
	svc.Handler().ServeHTTP(rr, req)
	require.Equal(t, http.StatusTooManyRequests, rr.Code)
}

func TestBootstrapExchange_RejectsMalformedPayload(t *testing.T) {
	svc, err := New(Config{DataDir: t.TempDir()})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/bootstrap/exchange", bytes.NewBufferString(`{"session_id":"abc"}`))
	rr := httptest.NewRecorder()
	svc.Handler().ServeHTTP(rr, req)
	require.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestBootstrapExchange_RejectsInvalidRecipient(t *testing.T) {
	svc, err := New(Config{DataDir: t.TempDir()})
	require.NoError(t, err)

	payload := bytes.NewBufferString(`{"session_id":"abc","proof":"def","ephemeral_recipient":"not-a-recipient"}`)
	req := httptest.NewRequest(http.MethodPost, "/bootstrap/exchange", payload)
	rr := httptest.NewRecorder()
	svc.Handler().ServeHTTP(rr, req)

	require.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestBootstrapExchange_ExpiredSessionRejected(t *testing.T) {
	svc, err := New(Config{DataDir: t.TempDir()})
	require.NoError(t, err)

	startReq := httptest.NewRequest(http.MethodPost, "/bootstrap/session", bytes.NewBufferString(`{"ttl_seconds":1}`))
	startRR := httptest.NewRecorder()
	svc.Handler().ServeHTTP(startRR, startReq)
	require.Equal(t, http.StatusOK, startRR.Code)

	var startResp struct {
		PairCode string `json:"pair_code"`
	}
	require.NoError(t, json.Unmarshal(startRR.Body.Bytes(), &startResp))
	sessionID, codeSecret, err := bootstrap.ParsePairCode(startResp.PairCode)
	require.NoError(t, err)
	proof, err := bootstrap.ComputeProof(sessionID, codeSecret)
	require.NoError(t, err)
	id, err := age.GenerateX25519Identity()
	require.NoError(t, err)

	time.Sleep(1200 * time.Millisecond)

	payload, err := json.Marshal(map[string]string{
		"session_id":          sessionID,
		"proof":               proof,
		"ephemeral_recipient": id.Recipient().String(),
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/bootstrap/exchange", bytes.NewReader(payload))
	rr := httptest.NewRecorder()
	svc.Handler().ServeHTTP(rr, req)

	require.Equal(t, http.StatusUnauthorized, rr.Code)
}

func ioReadAll(r io.Reader) ([]byte, error) {
	return io.ReadAll(r)
}
