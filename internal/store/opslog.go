package store

import (
    "bufio"
    "encoding/base64"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "os"
    "path/filepath"
    "strings"

    "github.com/dHarshMakwana/lfess/internal/crypto"
    "github.com/dHarshMakwana/lfess/internal/model"

    "filippo.io/age"
)

const opsLogFileName = "ops.log"

// OpsLog appends and reads newline-delimited JSON operations.
// Phase 2: each line is base64(age ciphertext) for a single JSON operation.
type OpsLog struct {
    path      string
    recipient age.Recipient
    identity  age.Identity
}

func NewOpsLog(dataDir string) (*OpsLog, error) {
    if strings.TrimSpace(dataDir) == "" {
        return nil, errors.New("data dir is required")
    }
    if err := os.MkdirAll(dataDir, 0o700); err != nil {
        return nil, fmt.Errorf("create data dir: %w", err)
    }
    id, err := crypto.EnsureX25519Identity(dataDir)
    if err != nil {
        return nil, err
    }
    return &OpsLog{path: filepath.Join(dataDir, opsLogFileName), recipient: id.Recipient(), identity: id}, nil
}

// Append writes exactly one operation as a single line and fsyncs.
func (l *OpsLog) Append(op model.Operation) error {
    if err := op.ValidateBasic(); err != nil {
        return err
    }

    f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
    if err != nil {
        return fmt.Errorf("open ops log: %w", err)
    }
    defer f.Close()

    plaintext, err := json.Marshal(op)
    if err != nil {
        return fmt.Errorf("marshal op: %w", err)
    }

    ciphertext, err := crypto.Encrypt(plaintext, l.recipient)
    if err != nil {
        return fmt.Errorf("encrypt op: %w", err)
    }
    encoded := base64.StdEncoding.EncodeToString(ciphertext)

    if _, err := io.WriteString(f, encoded+"\n"); err != nil {
        return fmt.Errorf("append op: %w", err)
    }
    if err := f.Sync(); err != nil {
        return fmt.Errorf("fsync ops log: %w", err)
    }
    return nil
}

// ReadAll returns operations in file order, skipping corrupted/unreadable lines.
// Any skipped line prints a warning to stderr containing the 1-based line number.
func (l *OpsLog) ReadAll() ([]model.Operation, error) {
    ops, err := l.readDecoded()
    if err != nil {
        return nil, err
    }
    return ops, nil
}

// ReadEncryptedLines returns base64 ciphertext lines, exactly as stored (no empty lines).
func (l *OpsLog) ReadEncryptedLines() ([]string, error) {
    f, err := os.Open(l.path)
    if err != nil {
        if errors.Is(err, os.ErrNotExist) {
            return []string{}, nil
        }
        return nil, fmt.Errorf("open ops log: %w", err)
    }
    defer f.Close()
    var out []string
    s := bufio.NewScanner(f)
    // Encrypted lines are bigger than plaintext. Allow up to 10MB per line in MVP.
    s.Buffer(make([]byte, 0, 128*1024), 10*1024*1024)

    for s.Scan() {
        line := strings.TrimSpace(s.Text())
        if line == "" {
            continue
        }
        out = append(out, line)
    }
    if err := s.Err(); err != nil {
        return nil, fmt.Errorf("scan ops log: %w", err)
    }
    return out, nil
}

func (l *OpsLog) readDecoded() ([]model.Operation, error) {
    f, err := os.Open(l.path)
    if err != nil {
        if errors.Is(err, os.ErrNotExist) {
            return []model.Operation{}, nil
        }
        return nil, fmt.Errorf("open ops log: %w", err)
    }
    defer f.Close()

    var out []model.Operation
    s := bufio.NewScanner(f)
    // Encrypted lines are bigger than plaintext. Allow up to 10MB per line in MVP.
    s.Buffer(make([]byte, 0, 128*1024), 10*1024*1024)

    lineNo := 0
    for s.Scan() {
        lineNo++
        line := strings.TrimSpace(s.Text())
        if line == "" {
            fmt.Fprintf(os.Stderr, "warning: ops.log line %d is empty; skipping\n", lineNo)
            continue
        }

        ciphertext, err := base64.StdEncoding.DecodeString(line)
        if err != nil {
            fmt.Fprintf(os.Stderr, "warning: ops.log line %d base64 decode failed: %v; skipping\n", lineNo, err)
            continue
        }
        plaintext, err := crypto.Decrypt(ciphertext, l.identity)
        if err != nil {
            fmt.Fprintf(os.Stderr, "warning: ops.log line %d decrypt failed: %v; skipping\n", lineNo, err)
            continue
        }

        var op model.Operation
        if err := json.Unmarshal(plaintext, &op); err != nil {
            fmt.Fprintf(os.Stderr, "warning: ops.log line %d json unmarshal failed: %v; skipping\n", lineNo, err)
            continue
        }
        out = append(out, op)
    }
    if err := s.Err(); err != nil {
        return nil, fmt.Errorf("scan ops log: %w", err)
    }
    return out, nil
}

// Path returns the on-disk file path.
func (l *OpsLog) Path() string { return l.path }
