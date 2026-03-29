package store

import (
    "crypto/rand"
    "encoding/hex"
    "errors"
    "fmt"
    "os"
    "path/filepath"
    "strings"
)

const deviceIDFileName = "device.id"

// EnsureDeviceID returns the persisted device ID, generating and writing it on first run.
// The ID is stored in <dataDir>/device.id with mode 0600.
func EnsureDeviceID(dataDir string) (string, error) {
    if strings.TrimSpace(dataDir) == "" {
        return "", errors.New("data dir is required")
    }

    if err := os.MkdirAll(dataDir, 0o700); err != nil {
        return "", fmt.Errorf("create data dir: %w", err)
    }

    p := filepath.Join(dataDir, deviceIDFileName)
    b, err := os.ReadFile(p)
    if err == nil {
        id := strings.TrimSpace(string(b))
        if id == "" {
            return "", fmt.Errorf("device id file is empty: %s", p)
        }
        return id, nil
    }
    if !errors.Is(err, os.ErrNotExist) {
        return "", fmt.Errorf("read device id: %w", err)
    }

    id, err := newDeviceID()
    if err != nil {
        return "", err
    }

    // Create exclusively to avoid races between two concurrent first-runs.
    f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
    if err != nil {
        if errors.Is(err, os.ErrExist) {
            // Another process won. Read it.
            b2, err2 := os.ReadFile(p)
            if err2 != nil {
                return "", fmt.Errorf("read device id after exist race: %w", err2)
            }
            id2 := strings.TrimSpace(string(b2))
            if id2 == "" {
                return "", fmt.Errorf("device id file is empty after exist race: %s", p)
            }
            return id2, nil
        }
        return "", fmt.Errorf("create device id file: %w", err)
    }
    defer f.Close()

    if _, err := f.WriteString(id + "\n"); err != nil {
        return "", fmt.Errorf("write device id: %w", err)
    }
    if err := f.Sync(); err != nil {
        return "", fmt.Errorf("fsync device id: %w", err)
    }

    return id, nil
}

func newDeviceID() (string, error) {
    // 16 random bytes -> 32 hex chars. Short, portable, collision-resistant enough for MVP.
    var buf [16]byte
    if _, err := rand.Read(buf[:]); err != nil {
        return "", fmt.Errorf("rand: %w", err)
    }
    return hex.EncodeToString(buf[:]), nil
}
