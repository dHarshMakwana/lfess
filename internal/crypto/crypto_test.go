package crypto_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dHarshMakwana/lfess/internal/crypto"
	"github.com/stretchr/testify/require"
)

func TestEncryptDecrypt_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	crypto.SetDataDir(dir)

	plaintext := []byte("hello, lfess")
	ct, err := crypto.Encrypt(plaintext)
	require.NoError(t, err)
	require.NotEmpty(t, ct)
	require.NotEqual(t, plaintext, ct)

	pt, err := crypto.Decrypt(ct)
	require.NoError(t, err)
	require.Equal(t, plaintext, pt)
}

func TestEncryptDecrypt_EmptyPlaintext_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	crypto.SetDataDir(dir)

	ct, err := crypto.Encrypt([]byte{})
	require.NoError(t, err)
	require.NotEmpty(t, ct)

	pt, err := crypto.Decrypt(ct)
	require.NoError(t, err)
	require.Equal(t, []byte{}, pt)
}

func TestKeyPersistence_CreatesAndReusesKeyWith0600(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, crypto.KeyFileName)

	// First use creates the key.
	crypto.SetDataDir(dir)
	_, err := crypto.Encrypt([]byte("first"))
	require.NoError(t, err)

	st1, err := os.Stat(keyPath)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), st1.Mode().Perm())

	key1, err := os.ReadFile(keyPath)
	require.NoError(t, err)
	require.NotEmpty(t, key1)

	// Second use reuses the same key (file contents unchanged).
	_, err = crypto.Encrypt([]byte("second"))
	require.NoError(t, err)

	key2, err := os.ReadFile(keyPath)
	require.NoError(t, err)
	require.Equal(t, key1, key2)
}

func TestWrongKeyFails(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()

	crypto.SetDataDir(dirA)
	ct, err := crypto.Encrypt([]byte("secret"))
	require.NoError(t, err)

	crypto.SetDataDir(dirB)
	_, err = crypto.Decrypt(ct)
	require.Error(t, err)
}
