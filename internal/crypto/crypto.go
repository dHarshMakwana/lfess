package crypto

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"filippo.io/age"
)

// Package crypto provides small helpers for encrypting and decrypting bytes.
//
// Phase 2 contract: ops are JSON-encoded and then encrypted; the store is
// responsible for base64 encoding and per-line framing.

// Encrypt encrypts plaintext with age using the given recipients.
func Encrypt(plaintext []byte, recipients ...age.Recipient) ([]byte, error) {
	if len(recipients) == 0 {
		return nil, fmt.Errorf("encrypt: no recipients provided")
	}

	var buf bytes.Buffer
	w, err := age.Encrypt(&buf, recipients...)
	if err != nil {
		return nil, fmt.Errorf("encrypt: %w", err)
	}
	if _, err := w.Write(plaintext); err != nil {
		_ = w.Close()
		return nil, fmt.Errorf("encrypt write: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("encrypt close: %w", err)
	}
	return buf.Bytes(), nil
}

// Decrypt decrypts ciphertext with age using the given identities.
func Decrypt(ciphertext []byte, identities ...age.Identity) ([]byte, error) {
	if len(identities) == 0 {
		return nil, fmt.Errorf("decrypt: no identities provided")
	}

	r, err := age.Decrypt(bytes.NewReader(ciphertext), identities...)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}
	pt, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("decrypt read: %w", err)
	}
	return pt, nil
}

const KeyFileName = "key.age"

// EnsureX25519Identity loads an age X25519 identity from <dataDir>/key.age, or creates
// it if it doesn't exist. The file is created with mode 0600.
//
// This is the missing piece to make ops.log readable across process runs and
// between devices that share/copy the data directory.
func EnsureX25519Identity(dataDir string) (*age.X25519Identity, error) {
	if strings.TrimSpace(dataDir) == "" {
		return nil, fmt.Errorf("key: data dir is required")
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("key: create data dir: %w", err)
	}

	p := filepath.Join(dataDir, KeyFileName)
	b, err := os.ReadFile(p)
	if err == nil {
		s := strings.TrimSpace(string(b))
		id, err := age.ParseX25519Identity(s)
		if err != nil {
			return nil, fmt.Errorf("key: parse identity: %w", err)
		}
		return id, nil
	}
	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("key: read identity: %w", err)
	}

	id, err := age.GenerateX25519Identity()
	if err != nil {
		return nil, fmt.Errorf("key: generate identity: %w", err)
	}

	// Best-effort exclusive create to avoid races.
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if os.IsExist(err) {
			b2, err2 := os.ReadFile(p)
			if err2 != nil {
				return nil, fmt.Errorf("key: read identity after exist race: %w", err2)
			}
			s2 := strings.TrimSpace(string(b2))
			id2, err2 := age.ParseX25519Identity(s2)
			if err2 != nil {
				return nil, fmt.Errorf("key: parse identity after exist race: %w", err2)
			}
			return id2, nil
		}
		return nil, fmt.Errorf("key: create identity file: %w", err)
	}
	defer f.Close()

	if _, err := io.WriteString(f, id.String()+"\n"); err != nil {
		return nil, fmt.Errorf("key: write identity: %w", err)
	}
	if err := f.Sync(); err != nil {
		return nil, fmt.Errorf("key: fsync identity: %w", err)
	}

	return id, nil
}
