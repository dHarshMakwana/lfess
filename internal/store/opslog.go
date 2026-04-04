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
	"sync"

	"github.com/dHarshMakwana/lfess/internal/crypto"
	"github.com/dHarshMakwana/lfess/internal/engine"
	"github.com/dHarshMakwana/lfess/internal/model"
)

const opsLogFileName = "ops.log"

var ErrInvalidImportPayload = errors.New("invalid import payload")
var ErrOperationIDCollision = errors.New("operation id collision")

// OpsLog appends and reads newline-delimited JSON operations.
// Phase 2: each line is base64(age ciphertext) for a single JSON operation.
type OpsLog struct {
	path string
	mu   sync.Mutex
}

func NewOpsLog(dataDir string) (*OpsLog, error) {
	if strings.TrimSpace(dataDir) == "" {
		return nil, errors.New("data dir is required")
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	crypto.SetDataDir(dataDir)
	return &OpsLog{path: filepath.Join(dataDir, opsLogFileName)}, nil
}

// Append writes exactly one operation as a single line and fsyncs.
func (l *OpsLog) Append(op model.Operation) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.appendUnlocked(op)
}

func (l *OpsLog) appendUnlocked(op model.Operation) error {
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

	ciphertext, err := crypto.Encrypt(plaintext)
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

// ImportEncryptedLines validates and imports encrypted peer operations through
// the Merge flow. It appends only operations that are missing locally and
// returns how many were appended.
func (l *OpsLog) ImportEncryptedLines(lines []string) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	remoteOps, err := decodeEncryptedLines(lines)
	if err != nil {
		return 0, err
	}

	localOps, err := l.readDecoded()
	if err != nil {
		return 0, fmt.Errorf("read local ops: %w", err)
	}

	fingerprints := make(map[string]string, len(localOps)+len(remoteOps))
	for _, op := range localOps {
		fp, err := operationFingerprint(op)
		if err != nil {
			return 0, fmt.Errorf("fingerprint local op %q: %w", op.OperationID, err)
		}
		if existing, ok := fingerprints[op.OperationID]; ok && existing != fp {
			return 0, fmt.Errorf("%w: operation id %q has conflicting payload", ErrOperationIDCollision, op.OperationID)
		}
		fingerprints[op.OperationID] = fp
	}
	for _, op := range remoteOps {
		fp, err := operationFingerprint(op)
		if err != nil {
			return 0, fmt.Errorf("fingerprint remote op %q: %w", op.OperationID, err)
		}
		if existing, ok := fingerprints[op.OperationID]; ok {
			if existing != fp {
				return 0, fmt.Errorf("%w: operation id %q has conflicting payload", ErrOperationIDCollision, op.OperationID)
			}
			continue
		}
		fingerprints[op.OperationID] = fp
	}

	merged := engine.Merge(localOps, remoteOps)
	seen := make(map[string]struct{}, len(localOps))
	for _, op := range localOps {
		if _, ok := seen[op.OperationID]; ok {
			continue
		}
		seen[op.OperationID] = struct{}{}
	}

	imported := 0
	for _, op := range merged {
		if _, ok := seen[op.OperationID]; ok {
			continue
		}
		if err := l.appendUnlocked(op); err != nil {
			return imported, fmt.Errorf("append imported op %q: %w", op.OperationID, err)
		}
		seen[op.OperationID] = struct{}{}
		imported++
	}

	return imported, nil
}

func decodeEncryptedLines(lines []string) ([]model.Operation, error) {
	ops := make([]model.Operation, 0, len(lines))
	for idx, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			return nil, fmt.Errorf("%w: line %d is empty", ErrInvalidImportPayload, idx+1)
		}

		ciphertext, err := base64.StdEncoding.DecodeString(line)
		if err != nil {
			return nil, fmt.Errorf("%w: line %d base64 decode failed: %v", ErrInvalidImportPayload, idx+1, err)
		}

		plaintext, err := crypto.Decrypt(ciphertext)
		if err != nil {
			return nil, fmt.Errorf("%w: line %d decrypt failed: %v", ErrInvalidImportPayload, idx+1, err)
		}

		var op model.Operation
		if err := json.Unmarshal(plaintext, &op); err != nil {
			return nil, fmt.Errorf("%w: line %d json unmarshal failed: %v", ErrInvalidImportPayload, idx+1, err)
		}
		if err := op.ValidateBasic(); err != nil {
			return nil, fmt.Errorf("%w: line %d operation invalid: %v", ErrInvalidImportPayload, idx+1, err)
		}
		ops = append(ops, op)
	}

	return ops, nil
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
		plaintext, err := crypto.Decrypt(ciphertext)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: ops.log line %d decrypt failed: %v; skipping\n", lineNo, err)
			continue
		}

		var op model.Operation
		if err := json.Unmarshal(plaintext, &op); err != nil {
			fmt.Fprintf(os.Stderr, "warning: ops.log line %d json unmarshal failed: %v; skipping\n", lineNo, err)
			continue
		}
		if err := op.ValidateBasic(); err != nil {
			fmt.Fprintf(os.Stderr, "warning: ops.log line %d operation invalid: %v; skipping\n", lineNo, err)
			continue
		}
		out = append(out, op)
	}
	if err := s.Err(); err != nil {
		return nil, fmt.Errorf("scan ops log: %w", err)
	}
	return out, nil
}

func operationFingerprint(op model.Operation) (string, error) {
	b, err := json.Marshal(op)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// Path returns the on-disk file path.
func (l *OpsLog) Path() string { return l.path }
