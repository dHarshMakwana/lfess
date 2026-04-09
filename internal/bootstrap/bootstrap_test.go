package bootstrap

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"filippo.io/age"

	"github.com/dHarshMakwana/lfess/internal/crypto"
	"github.com/stretchr/testify/require"
)

func TestStartSession_ReturnsPairCodeAndExpiry(t *testing.T) {
	dir := t.TempDir()
	now := time.Unix(100, 0).UTC()
	mgr, err := NewManager(dir, WithNowFunc(func() time.Time { return now }))
	require.NoError(t, err)

	res, err := mgr.StartSession(2 * time.Minute)
	require.NoError(t, err)
	require.Equal(t, now.Add(2*time.Minute), res.ExpiresAt)
	require.NotEmpty(t, res.SessionID)
	require.Contains(t, res.PairCode, ".")

	sessionID, codeSecret, err := ParsePairCode(res.PairCode)
	require.NoError(t, err)
	require.Equal(t, res.SessionID, sessionID)
	require.NotEmpty(t, codeSecret)

	mgr.mu.Lock()
	require.Len(t, mgr.sessions, 1)
	require.Contains(t, mgr.sessions, res.SessionID)
	mgr.mu.Unlock()
}

func TestExchange_SuccessIsSingleUseAndEncryptsForRecipient(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(dir)
	require.NoError(t, err)

	res, err := mgr.StartSession(2 * time.Minute)
	require.NoError(t, err)
	sessionID, codeSecret, err := ParsePairCode(res.PairCode)
	require.NoError(t, err)

	proof, err := ComputeProof(sessionID, codeSecret)
	require.NoError(t, err)

	ephemeralID, err := age.GenerateX25519Identity()
	require.NoError(t, err)

	ciphertext, err := mgr.Exchange(ExchangeRequest{
		SessionID:          sessionID,
		Proof:              proof,
		EphemeralRecipient: ephemeralID.Recipient().String(),
	})
	require.NoError(t, err)
	require.NotEmpty(t, ciphertext)

	r, err := age.Decrypt(bytes.NewReader(ciphertext), ephemeralID)
	require.NoError(t, err)
	plaintext, err := ioReadAll(r)
	require.NoError(t, err)

	expectedKey, err := os.ReadFile(filepath.Join(dir, crypto.KeyFileName))
	require.NoError(t, err)
	require.Equal(t, string(bytes.TrimSpace(expectedKey)), string(bytes.TrimSpace(plaintext)))

	_, err = mgr.Exchange(ExchangeRequest{
		SessionID:          sessionID,
		Proof:              proof,
		EphemeralRecipient: ephemeralID.Recipient().String(),
	})
	require.ErrorIs(t, err, ErrSessionUsed)

	otherID, err := age.GenerateX25519Identity()
	require.NoError(t, err)
	_, err = age.Decrypt(bytes.NewReader(ciphertext), otherID)
	require.Error(t, err)
}

func TestExchange_ExpiredSessionRejected(t *testing.T) {
	dir := t.TempDir()
	now := time.Unix(1000, 0).UTC()
	mgr, err := NewManager(dir, WithNowFunc(func() time.Time { return now }))
	require.NoError(t, err)

	res, err := mgr.StartSession(30 * time.Second)
	require.NoError(t, err)
	sessionID, codeSecret, err := ParsePairCode(res.PairCode)
	require.NoError(t, err)
	proof, err := ComputeProof(sessionID, codeSecret)
	require.NoError(t, err)

	now = now.Add(31 * time.Second)
	ephemeralID, err := age.GenerateX25519Identity()
	require.NoError(t, err)

	_, err = mgr.Exchange(ExchangeRequest{
		SessionID:          sessionID,
		Proof:              proof,
		EphemeralRecipient: ephemeralID.Recipient().String(),
	})
	require.ErrorIs(t, err, ErrSessionExpired)
}

func TestExchange_InvalidProofRateLimited(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(dir, WithMaxFailedAttempts(2))
	require.NoError(t, err)

	res, err := mgr.StartSession(2 * time.Minute)
	require.NoError(t, err)
	sessionID, _, err := ParsePairCode(res.PairCode)
	require.NoError(t, err)
	ephemeralID, err := age.GenerateX25519Identity()
	require.NoError(t, err)

	_, err = mgr.Exchange(ExchangeRequest{
		SessionID:          sessionID,
		Proof:              "bad-proof-1",
		EphemeralRecipient: ephemeralID.Recipient().String(),
	})
	require.ErrorIs(t, err, ErrInvalidProof)

	_, err = mgr.Exchange(ExchangeRequest{
		SessionID:          sessionID,
		Proof:              "bad-proof-2",
		EphemeralRecipient: ephemeralID.Recipient().String(),
	})
	require.ErrorIs(t, err, ErrTooManyAttempts)

	_, err = mgr.Exchange(ExchangeRequest{
		SessionID:          sessionID,
		Proof:              "still-bad",
		EphemeralRecipient: ephemeralID.Recipient().String(),
	})
	require.ErrorIs(t, err, ErrTooManyAttempts)
}

func TestStartSession_InvalidTTLRejected(t *testing.T) {
	mgr, err := NewManager(t.TempDir())
	require.NoError(t, err)

	_, err = mgr.StartSession(0)
	require.ErrorIs(t, err, ErrInvalidTTL)

	_, err = mgr.StartSession(45 * time.Minute)
	require.ErrorIs(t, err, ErrInvalidTTL)
}

func TestParsePairCode_Validation(t *testing.T) {
	_, _, err := ParsePairCode("")
	require.ErrorIs(t, err, ErrInvalidPairCode)

	_, _, err = ParsePairCode("missing-dot")
	require.ErrorIs(t, err, ErrInvalidPairCode)

	sessionID, codeSecret, err := ParsePairCode("abc.def")
	require.NoError(t, err)
	require.Equal(t, "abc", sessionID)
	require.Equal(t, "def", codeSecret)
}

func TestComputeProof_DeterministicAndValidatesInputs(t *testing.T) {
	proof1, err := ComputeProof("sid", "secret")
	require.NoError(t, err)
	proof2, err := ComputeProof("sid", "secret")
	require.NoError(t, err)
	require.Equal(t, proof1, proof2)

	_, err = ComputeProof("", "secret")
	require.ErrorIs(t, err, ErrInvalidPairCode)
}

func TestInstallKeyAtomically_Writes0600AndValidatesPayload(t *testing.T) {
	dir := t.TempDir()
	id, err := age.GenerateX25519Identity()
	require.NoError(t, err)

	payload := []byte(id.String() + "\n")
	require.NoError(t, InstallKeyAtomically(dir, payload))

	keyPath := filepath.Join(dir, crypto.KeyFileName)
	st, err := os.Stat(keyPath)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), st.Mode().Perm())

	written, err := os.ReadFile(keyPath)
	require.NoError(t, err)
	require.Equal(t, id.String(), string(bytes.TrimSpace(written)))

	err = InstallKeyAtomically(dir, []byte("not-an-age-key"))
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrInvalidKeyPayload))
}

func TestManager_RandomHexUsesConfiguredReader(t *testing.T) {
	fake := bytes.NewReader(bytes.Repeat([]byte{0xab}, 32))
	mgr, err := NewManager(t.TempDir(), WithRandReader(fake))
	require.NoError(t, err)

	h, err := mgr.randomHex(8)
	require.NoError(t, err)
	require.Equal(t, "abababababababab", h)

	_, err = mgr.randomHex(32)
	require.Error(t, err)
}

func ioReadAll(r io.Reader) ([]byte, error) {
	return io.ReadAll(r)
}
