package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dHarshMakwana/lfess/internal/crypto"
)

// Store owns the data directory layout and provides access to persisted state.
//
// Phase 2 scope:
//   - device.id persistence (see EnsureDeviceID)
//   - ops.log append-only encrypted operation log (see OpsLog)
type Store struct {
	dataDir   string
}

func New(dataDir string) (*Store, error) {
	dataDir = strings.TrimSpace(dataDir)
	if dataDir == "" {
		return nil, errors.New("data dir is required")
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	crypto.SetDataDir(dataDir)

	return &Store{
		dataDir:   dataDir,
	}, nil
}

func (s *Store) DataDir() string { return s.dataDir }

func (s *Store) deviceIDPath() string { return filepath.Join(s.dataDir, deviceIDFileName) }
func (s *Store) opsLogPath() string   { return filepath.Join(s.dataDir, opsLogFileName) }

func (s *Store) OpsLog() *OpsLog {
	return &OpsLog{path: s.opsLogPath()}
}
