package crypto

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"filippo.io/age"
)

// Package crypto provides small helpers for encrypting and decrypting bytes.
//
// Phase 2 contract: ops are JSON-encoded and then encrypted; the store is
// responsible for base64 encoding and per-line framing.

// Phase 3 public API
//
// Encrypt / Decrypt expose a tiny, stable interface. Callers are responsible for
// JSON/base64/line-framing per the store contract.

var (
	mu      sync.RWMutex
	dataDir string
)

// SetDataDir configures where crypto persists the age identity (key.age).
//
// It must be set by wiring (CLI/server/store) before calling Encrypt/Decrypt.
func SetDataDir(dir string) {
	mu.Lock()
	defer mu.Unlock()
	dataDir = strings.TrimSpace(dir)
}

func getDataDir() (string, error) {
	mu.RLock()
	defer mu.RUnlock()
	if strings.TrimSpace(dataDir) == "" {
		return "", fmt.Errorf("crypto data dir is not configured")
	}
	return dataDir, nil
}

// Encrypt encrypts plaintext to age ciphertext bytes.
func Encrypt(plaintext []byte) ([]byte, error) {
	dir, err := getDataDir()
	if err != nil {
		return nil, err
	}
	id, err := EnsureX25519Identity(dir)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	w, err := age.Encrypt(&buf, id.Recipient())
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

// Decrypt decrypts age ciphertext bytes to plaintext.
func Decrypt(ciphertext []byte) ([]byte, error) {
	dir, err := getDataDir()
	if err != nil {
		return nil, err
	}
	id, err := EnsureX25519Identity(dir)
	if err != nil {
		return nil, err
	}

	r, err := age.Decrypt(bytes.NewReader(ciphertext), id)
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

func keyPath(dataDir string) string {
	return filepath.Join(dataDir, KeyFileName)
}

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

	p := keyPath(dataDir)
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
			for i := 0; i < 20; i++ {
				b2, err2 := os.ReadFile(p)
				if err2 != nil {
					return nil, fmt.Errorf("key: read identity after exist race: %w", err2)
				}
				s2 := strings.TrimSpace(string(b2))
				if s2 == "" {
					time.Sleep(5 * time.Millisecond)
					continue
				}
				id2, err2 := age.ParseX25519Identity(s2)
				if err2 == nil {
					return id2, nil
				}
				time.Sleep(5 * time.Millisecond)
			}
			return nil, fmt.Errorf("key: parse identity after exist race: identity file not ready")
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
