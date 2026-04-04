package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/dHarshMakwana/lfess/internal/crypto"
	"github.com/dHarshMakwana/lfess/internal/engine"
	"github.com/dHarshMakwana/lfess/internal/model"
)

type noteWriteRequest struct {
	Content string `json:"content"`
}

type connectRequest struct {
	PeerBaseURL string `json:"peer_base_url"`
}

type identityResponse struct {
	Identity string `json:"identity"`
}

func (s *Service) handleNotes(w http.ResponseWriter, r *http.Request) {
	setCORSHeaders(w, http.MethodGet, http.MethodPost)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.handleListNotes(w)
	case http.MethodPost:
		s.handleCreateNote(w, r)
	default:
		methodNotAllowed(w, http.MethodGet, http.MethodPost)
	}
}

func (s *Service) handleIdentity(w http.ResponseWriter, r *http.Request) {
	setCORSHeaders(w, http.MethodGet)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}

	identity, err := crypto.IdentityString(filepath.Dir(s.log.Path()))
	if err != nil {
		http.Error(w, fmt.Sprintf("read identity: %v", err), http.StatusInternalServerError)
		return
	}

	if err := writeJSON(w, http.StatusOK, identityResponse{Identity: identity}); err != nil {
		http.Error(w, fmt.Sprintf("encode response: %v", err), http.StatusInternalServerError)
	}
}

func (s *Service) handleConnect(w http.ResponseWriter, r *http.Request) {
	setCORSHeaders(w, http.MethodPost)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}

	defer r.Body.Close()

	var req connectRequest
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(&req); err != nil {
		http.Error(w, "invalid request payload", http.StatusBadRequest)
		return
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		http.Error(w, "invalid request payload", http.StatusBadRequest)
		return
	}

	peerIdentity, err := fetchPeerIdentity(req.PeerBaseURL)
	if err != nil {
		http.Error(w, fmt.Sprintf("fetch peer identity: %v", err), http.StatusBadRequest)
		return
	}

	if err := crypto.ImportPeerIdentity(filepath.Dir(s.log.Path()), peerIdentity); err != nil {
		http.Error(w, fmt.Sprintf("import peer identity: %v", err), http.StatusInternalServerError)
		return
	}

	if err := writeJSON(w, http.StatusOK, map[string]any{"connected": true, "peer_base_url": strings.TrimSpace(req.PeerBaseURL)}); err != nil {
		http.Error(w, fmt.Sprintf("encode response: %v", err), http.StatusInternalServerError)
	}
}

func (s *Service) handleNoteByID(w http.ResponseWriter, r *http.Request) {
	setCORSHeaders(w, http.MethodGet, http.MethodPut, http.MethodDelete)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	noteID, ok := noteIDFromPath(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.handleGetNote(w, r, noteID)
	case http.MethodPut:
		s.handleUpdateNote(w, r, noteID)
	case http.MethodDelete:
		s.handleDeleteNote(w, r, noteID)
	default:
		methodNotAllowed(w, http.MethodGet, http.MethodPut, http.MethodDelete)
	}
}

func (s *Service) handleListNotes(w http.ResponseWriter) {
	notes, err := s.currentNotes()
	if err != nil {
		http.Error(w, fmt.Sprintf("read notes: %v", err), http.StatusInternalServerError)
		return
	}

	if err := writeJSON(w, http.StatusOK, notes); err != nil {
		http.Error(w, fmt.Sprintf("encode response: %v", err), http.StatusInternalServerError)
	}
}

func (s *Service) handleGetNote(w http.ResponseWriter, r *http.Request, noteID string) {
	note, ok, err := s.currentNote(noteID)
	if err != nil {
		http.Error(w, fmt.Sprintf("read note: %v", err), http.StatusInternalServerError)
		return
	}
	if !ok {
		http.NotFound(w, r)
		return
	}

	if err := writeJSON(w, http.StatusOK, note); err != nil {
		http.Error(w, fmt.Sprintf("encode response: %v", err), http.StatusInternalServerError)
	}
}

func (s *Service) handleCreateNote(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	var req noteWriteRequest
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(&req); err != nil {
		http.Error(w, "invalid request payload", http.StatusBadRequest)
		return
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		http.Error(w, "invalid request payload", http.StatusBadRequest)
		return
	}

	noteID, err := newNoteID()
	if err != nil {
		http.Error(w, fmt.Sprintf("create note id: %v", err), http.StatusInternalServerError)
		return
	}

	op, err := model.NewAddOperation(s.deviceID, noteID, req.Content, time.Now().UTC())
	if err != nil {
		http.Error(w, fmt.Sprintf("create note: %v", err), http.StatusInternalServerError)
		return
	}
	if err := s.log.Append(op); err != nil {
		http.Error(w, fmt.Sprintf("append note: %v", err), http.StatusInternalServerError)
		return
	}

	created, ok, err := s.currentNote(noteID)
	if err != nil {
		http.Error(w, fmt.Sprintf("read note: %v", err), http.StatusInternalServerError)
		return
	}
	if !ok {
		created = model.Item{ID: noteID, Content: req.Content}
	}

	if err := writeJSON(w, http.StatusCreated, map[string]model.Item{"item": created}); err != nil {
		http.Error(w, fmt.Sprintf("encode response: %v", err), http.StatusInternalServerError)
	}
}

func (s *Service) handleUpdateNote(w http.ResponseWriter, r *http.Request, noteID string) {
	defer r.Body.Close()

	existing, ok, err := s.currentNote(noteID)
	if err != nil {
		http.Error(w, fmt.Sprintf("read note: %v", err), http.StatusInternalServerError)
		return
	}
	if !ok || existing.Deleted {
		http.NotFound(w, r)
		return
	}

	var req noteWriteRequest
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(&req); err != nil {
		http.Error(w, "invalid request payload", http.StatusBadRequest)
		return
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		http.Error(w, "invalid request payload", http.StatusBadRequest)
		return
	}

	op, err := model.NewUpdateOperation(s.deviceID, noteID, req.Content, time.Now().UTC())
	if err != nil {
		http.Error(w, fmt.Sprintf("update note: %v", err), http.StatusInternalServerError)
		return
	}
	if err := s.log.Append(op); err != nil {
		http.Error(w, fmt.Sprintf("append note: %v", err), http.StatusInternalServerError)
		return
	}

	updated, ok, err := s.currentNote(noteID)
	if err != nil {
		http.Error(w, fmt.Sprintf("read note: %v", err), http.StatusInternalServerError)
		return
	}
	if !ok {
		updated = model.Item{ID: noteID, Content: req.Content}
	}

	if err := writeJSON(w, http.StatusOK, map[string]model.Item{"item": updated}); err != nil {
		http.Error(w, fmt.Sprintf("encode response: %v", err), http.StatusInternalServerError)
	}
}

func (s *Service) handleDeleteNote(w http.ResponseWriter, r *http.Request, noteID string) {
	existing, ok, err := s.currentNote(noteID)
	if err != nil {
		http.Error(w, fmt.Sprintf("read note: %v", err), http.StatusInternalServerError)
		return
	}
	if !ok || existing.Deleted {
		http.NotFound(w, r)
		return
	}

	op, err := model.NewDeleteOperation(s.deviceID, noteID, time.Now().UTC())
	if err != nil {
		http.Error(w, fmt.Sprintf("delete note: %v", err), http.StatusInternalServerError)
		return
	}
	if err := s.log.Append(op); err != nil {
		http.Error(w, fmt.Sprintf("append note: %v", err), http.StatusInternalServerError)
		return
	}

	deleted, ok, err := s.currentNote(noteID)
	if err != nil {
		http.Error(w, fmt.Sprintf("read note: %v", err), http.StatusInternalServerError)
		return
	}
	if !ok {
		deleted = model.Item{ID: noteID, Deleted: true}
	}
	deleted.Deleted = true

	if err := writeJSON(w, http.StatusOK, map[string]model.Item{"item": deleted}); err != nil {
		http.Error(w, fmt.Sprintf("encode response: %v", err), http.StatusInternalServerError)
	}
}

func (s *Service) currentNotes() ([]model.Item, error) {
	ops, err := s.log.ReadAll()
	if err != nil {
		return nil, err
	}
	state := engine.Replay(ops)

	ids := make([]string, 0, len(state))
	for id := range state {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	notes := make([]model.Item, 0, len(ids))
	for _, id := range ids {
		notes = append(notes, state[id])
	}
	return notes, nil
}

func (s *Service) currentNote(noteID string) (model.Item, bool, error) {
	notes, err := s.currentNotes()
	if err != nil {
		return model.Item{}, false, err
	}
	for _, note := range notes {
		if note.ID == noteID {
			return note, true, nil
		}
	}
	return model.Item{}, false, nil
}

func noteIDFromPath(path string) (string, bool) {
	noteID := strings.TrimPrefix(path, "/notes/")
	if noteID == "" || strings.Contains(noteID, "/") {
		return "", false
	}
	return strings.TrimSpace(noteID), true
}

func fetchPeerIdentity(peerBaseURL string) (string, error) {
	peerBaseURL = strings.TrimSpace(peerBaseURL)
	if peerBaseURL == "" {
		return "", fmt.Errorf("peer base url is required")
	}

	parsed, err := url.Parse(peerBaseURL)
	if err != nil {
		return "", fmt.Errorf("parse peer base url: %w", err)
	}
	parsed.Path = path.Join(parsed.Path, "identity")
	if !strings.HasPrefix(parsed.Path, "/") {
		parsed.Path = "/" + parsed.Path
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(parsed.String())
	if err != nil {
		return "", fmt.Errorf("get identity: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("peer identity status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var out identityResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("decode identity response: %w", err)
	}
	out.Identity = strings.TrimSpace(out.Identity)
	if out.Identity == "" {
		return "", fmt.Errorf("peer identity is empty")
	}
	return out.Identity, nil
}

func newNoteID() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf[:]), nil
}
