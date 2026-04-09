package bootstrap

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"filippo.io/age"

	"github.com/dHarshMakwana/lfess/internal/crypto"
)

const (
	defaultMaxFailedAttempts = 5
	maxSessionTTL            = 30 * time.Minute
)

var (
	ErrInvalidTTL             = errors.New("bootstrap: invalid session ttl")
	ErrInvalidSession         = errors.New("bootstrap: session not found")
	ErrSessionExpired         = errors.New("bootstrap: session expired")
	ErrSessionUsed            = errors.New("bootstrap: session already used")
	ErrInvalidProof           = errors.New("bootstrap: invalid pairing proof")
	ErrTooManyAttempts        = errors.New("bootstrap: too many invalid pairing attempts")
	ErrInvalidRecipient       = errors.New("bootstrap: invalid ephemeral recipient")
	ErrInvalidPairCode        = errors.New("bootstrap: invalid pair code")
	ErrInvalidKeyPayload      = errors.New("bootstrap: invalid key payload")
	ErrInvalidExchangeRequest = errors.New("bootstrap: invalid exchange request")
)

type Option func(*Manager)

func WithNowFunc(now func() time.Time) Option {
	return func(m *Manager) {
		if now != nil {
			m.now = now
		}
	}
}

func WithRandReader(r io.Reader) Option {
	return func(m *Manager) {
		if r != nil {
			m.rand = r
		}
	}
}

func WithMaxFailedAttempts(n int) Option {
	return func(m *Manager) {
		if n > 0 {
			m.maxFailedAttempts = n
		}
	}
}

type session struct {
	sessionID      string
	codeSecret     string
	expiresAt      time.Time
	used           bool
	failedAttempts int
}

type Manager struct {
	dataDir           string
	now               func() time.Time
	rand              io.Reader
	maxFailedAttempts int

	mu       sync.Mutex
	sessions map[string]*session
}

type StartSessionResult struct {
	SessionID string
	PairCode  string
	ExpiresAt time.Time
}

type ExchangeRequest struct {
	SessionID          string
	Proof              string
	EphemeralRecipient string
}

func NewManager(dataDir string, opts ...Option) (*Manager, error) {
	dataDir = strings.TrimSpace(dataDir)
	if dataDir == "" {
		return nil, errors.New("bootstrap: data dir is required")
	}

	m := &Manager{
		dataDir:           dataDir,
		now:               time.Now,
		rand:              rand.Reader,
		maxFailedAttempts: defaultMaxFailedAttempts,
		sessions:          make(map[string]*session),
	}

	for _, opt := range opts {
		opt(m)
	}

	return m, nil
}

func (m *Manager) StartSession(ttl time.Duration) (StartSessionResult, error) {
	if ttl <= 0 || ttl > maxSessionTTL {
		return StartSessionResult{}, ErrInvalidTTL
	}

	now := m.now().UTC()
	expiresAt := now.Add(ttl)

	sessionID, err := m.randomHex(12)
	if err != nil {
		return StartSessionResult{}, fmt.Errorf("bootstrap: generate session id: %w", err)
	}
	codeSecret, err := m.randomHex(16)
	if err != nil {
		return StartSessionResult{}, fmt.Errorf("bootstrap: generate pair code secret: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.pruneExpiredLocked(now)

	for {
		if _, exists := m.sessions[sessionID]; !exists {
			break
		}
		sessionID, err = m.randomHex(12)
		if err != nil {
			return StartSessionResult{}, fmt.Errorf("bootstrap: generate unique session id: %w", err)
		}
	}

	m.sessions[sessionID] = &session{
		sessionID:  sessionID,
		codeSecret: codeSecret,
		expiresAt:  expiresAt,
	}

	return StartSessionResult{
		SessionID: sessionID,
		PairCode:  sessionID + "." + codeSecret,
		ExpiresAt: expiresAt,
	}, nil
}

func (m *Manager) Exchange(req ExchangeRequest) ([]byte, error) {
	sessionID := strings.TrimSpace(req.SessionID)
	proof := strings.TrimSpace(req.Proof)
	recipientRaw := strings.TrimSpace(req.EphemeralRecipient)
	if sessionID == "" || proof == "" || recipientRaw == "" {
		return nil, ErrInvalidExchangeRequest
	}

	recipient, err := age.ParseX25519Recipient(recipientRaw)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidRecipient, err)
	}

	now := m.now().UTC()
	m.mu.Lock()
	m.pruneExpiredExceptLocked(now, sessionID)

	s, ok := m.sessions[sessionID]
	if !ok {
		m.mu.Unlock()
		return nil, ErrInvalidSession
	}
	if s.used {
		m.mu.Unlock()
		return nil, ErrSessionUsed
	}
	if now.After(s.expiresAt) {
		delete(m.sessions, sessionID)
		m.mu.Unlock()
		return nil, ErrSessionExpired
	}
	if s.failedAttempts >= m.maxFailedAttempts {
		m.mu.Unlock()
		return nil, ErrTooManyAttempts
	}

	expectedProof, err := ComputeProof(s.sessionID, s.codeSecret)
	if err != nil {
		m.mu.Unlock()
		return nil, err
	}

	if subtle.ConstantTimeCompare([]byte(proof), []byte(expectedProof)) != 1 {
		s.failedAttempts++
		if s.failedAttempts >= m.maxFailedAttempts {
			m.mu.Unlock()
			return nil, ErrTooManyAttempts
		}
		m.mu.Unlock()
		return nil, ErrInvalidProof
	}

	// Single-use guarantee: mark consumed as soon as auth passes.
	s.used = true
	m.mu.Unlock()

	keyPayload, err := readLocalKeyPayload(m.dataDir)
	if err != nil {
		return nil, err
	}

	ciphertext, err := encryptToRecipient(keyPayload, recipient)
	if err != nil {
		return nil, err
	}
	return ciphertext, nil
}

func ParsePairCode(code string) (sessionID, codeSecret string, err error) {
	code = strings.TrimSpace(code)
	parts := strings.Split(code, ".")
	if len(parts) != 2 {
		return "", "", ErrInvalidPairCode
	}

	sessionID = strings.TrimSpace(parts[0])
	codeSecret = strings.TrimSpace(parts[1])
	if sessionID == "" || codeSecret == "" {
		return "", "", ErrInvalidPairCode
	}
	return sessionID, codeSecret, nil
}

func ComputeProof(sessionID, codeSecret string) (string, error) {
	sessionID = strings.TrimSpace(sessionID)
	codeSecret = strings.TrimSpace(codeSecret)
	if sessionID == "" || codeSecret == "" {
		return "", ErrInvalidPairCode
	}

	mac := hmac.New(sha256.New, []byte(codeSecret))
	if _, err := mac.Write([]byte(sessionID)); err != nil {
		return "", fmt.Errorf("bootstrap: compute proof: %w", err)
	}
	return hex.EncodeToString(mac.Sum(nil)), nil
}

func InstallKeyAtomically(dataDir string, keyPayload []byte) error {
	dataDir = strings.TrimSpace(dataDir)
	if dataDir == "" {
		return errors.New("bootstrap: data dir is required")
	}
	if len(bytes.TrimSpace(keyPayload)) == 0 {
		return ErrInvalidKeyPayload
	}

	identity := strings.TrimSpace(string(keyPayload))
	if _, err := age.ParseX25519Identity(identity); err != nil {
		return fmt.Errorf("%w: parse age identity: %v", ErrInvalidKeyPayload, err)
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return fmt.Errorf("bootstrap: create data dir: %w", err)
	}

	tmpFile, err := os.CreateTemp(dataDir, crypto.KeyFileName+".tmp-*")
	if err != nil {
		return fmt.Errorf("bootstrap: create temp key file: %w", err)
	}
	tmpPath := tmpFile.Name()
	cleanup := true
	defer func() {
		_ = tmpFile.Close()
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	if err := tmpFile.Chmod(0o600); err != nil {
		return fmt.Errorf("bootstrap: chmod temp key file: %w", err)
	}
	if _, err := io.WriteString(tmpFile, identity+"\n"); err != nil {
		return fmt.Errorf("bootstrap: write temp key file: %w", err)
	}
	if err := tmpFile.Sync(); err != nil {
		return fmt.Errorf("bootstrap: fsync temp key file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("bootstrap: close temp key file: %w", err)
	}

	keyPath := filepath.Join(dataDir, crypto.KeyFileName)
	if err := os.Rename(tmpPath, keyPath); err != nil {
		return fmt.Errorf("bootstrap: replace key file: %w", err)
	}
	cleanup = false

	if err := os.Chmod(keyPath, 0o600); err != nil {
		return fmt.Errorf("bootstrap: chmod key file: %w", err)
	}
	st, err := os.Stat(keyPath)
	if err != nil {
		return fmt.Errorf("bootstrap: stat key file: %w", err)
	}
	if st.Mode().Perm() != 0o600 {
		return fmt.Errorf("bootstrap: key file permissions are %o, expected 600", st.Mode().Perm())
	}
	return nil
}

func (m *Manager) randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := io.ReadFull(m.rand, b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func (m *Manager) pruneExpiredLocked(now time.Time) {
	for id, s := range m.sessions {
		if now.After(s.expiresAt) {
			delete(m.sessions, id)
		}
	}
}

func (m *Manager) pruneExpiredExceptLocked(now time.Time, keepSessionID string) {
	for id, s := range m.sessions {
		if id == keepSessionID {
			continue
		}
		if now.After(s.expiresAt) {
			delete(m.sessions, id)
		}
	}
}

func readLocalKeyPayload(dataDir string) ([]byte, error) {
	if _, err := crypto.EnsureX25519Identity(dataDir); err != nil {
		return nil, fmt.Errorf("bootstrap: ensure local key: %w", err)
	}

	keyPath := filepath.Join(dataDir, crypto.KeyFileName)
	b, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("bootstrap: read local key: %w", err)
	}
	if len(bytes.TrimSpace(b)) == 0 {
		return nil, fmt.Errorf("bootstrap: local key file is empty")
	}
	return b, nil
}

func encryptToRecipient(payload []byte, recipient *age.X25519Recipient) ([]byte, error) {
	var buf bytes.Buffer
	w, err := age.Encrypt(&buf, recipient)
	if err != nil {
		return nil, fmt.Errorf("bootstrap: encrypt payload: %w", err)
	}
	if _, err := w.Write(payload); err != nil {
		_ = w.Close()
		return nil, fmt.Errorf("bootstrap: write payload: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("bootstrap: finalize payload: %w", err)
	}
	return buf.Bytes(), nil
}
