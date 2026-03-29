package store

import (
    "bufio"
    "encoding/json"
    "errors"
    "fmt"
    "os"
    "path/filepath"
    "strings"

    "github.com/dHarshMakwana/lfess/internal/model"
)

const opsLogFileName = "ops.log"

// OpsLog appends and reads newline-delimited JSON operations.
// NOTE: Phase 2 will switch this to encrypted base64-encoded ciphertext lines.
type OpsLog struct {
    path string
}

func NewOpsLog(dataDir string) (*OpsLog, error) {
    if strings.TrimSpace(dataDir) == "" {
        return nil, errors.New("data dir is required")
    }
    if err := os.MkdirAll(dataDir, 0o700); err != nil {
        return nil, fmt.Errorf("create data dir: %w", err)
    }
    return &OpsLog{path: filepath.Join(dataDir, opsLogFileName)}, nil
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

    b, err := json.Marshal(op)
    if err != nil {
        return fmt.Errorf("marshal op: %w", err)
    }
    b = append(b, '\n')

    if _, err := f.Write(b); err != nil {
        return fmt.Errorf("append op: %w", err)
    }
    if err := f.Sync(); err != nil {
        return fmt.Errorf("fsync ops log: %w", err)
    }
    return nil
}

// ReadAll returns all successfully decoded operations.
func (l *OpsLog) ReadAll() ([]model.Operation, error) {
    f, err := os.Open(l.path)
    if err != nil {
        if errors.Is(err, os.ErrNotExist) {
            return nil, nil
        }
        return nil, fmt.Errorf("open ops log: %w", err)
    }
    defer f.Close()

    var out []model.Operation
    s := bufio.NewScanner(f)
    // allow bigger lines than default (64K) just in case.
    s.Buffer(make([]byte, 0, 64*1024), 1024*1024)

    for s.Scan() {
        line := strings.TrimSpace(s.Text())
        if line == "" {
            continue
        }
        var op model.Operation
        if err := json.Unmarshal([]byte(line), &op); err != nil {
            return nil, fmt.Errorf("decode op: %w", err)
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
