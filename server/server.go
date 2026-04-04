package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/dHarshMakwana/lfess/internal/store"
)

const (
	DefaultHost            = "127.0.0.1"
	DefaultPort            = 7777
	defaultReadTimeout     = 10 * time.Second
	defaultWriteTimeout    = 10 * time.Second
	defaultShutdownTimeout = 5 * time.Second
)

type Config struct {
	DataDir         string
	Host            string
	Port            int
	ShutdownTimeout time.Duration
}

type Service struct {
	log             *store.OpsLog
	deviceID        string
	server          *http.Server
	shutdownTimeout time.Duration
}

func New(cfg Config) (*Service, error) {
	if strings.TrimSpace(cfg.DataDir) == "" {
		return nil, errors.New("data dir is required")
	}

	host := strings.TrimSpace(cfg.Host)
	if host == "" {
		host = DefaultHost
	}

	switch {
	case cfg.Port == 0:
		cfg.Port = DefaultPort
	case cfg.Port < 0 || cfg.Port > 65535:
		return nil, fmt.Errorf("invalid port: %d", cfg.Port)
	}

	shutdownTimeout := cfg.ShutdownTimeout
	if shutdownTimeout <= 0 {
		shutdownTimeout = defaultShutdownTimeout
	}

	log, err := store.NewOpsLog(cfg.DataDir)
	if err != nil {
		return nil, err
	}
	deviceID, err := store.EnsureDeviceID(cfg.DataDir)
	if err != nil {
		return nil, err
	}

	svc := &Service{
		log:             log,
		deviceID:        deviceID,
		shutdownTimeout: shutdownTimeout,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", svc.handleHealth)
	mux.HandleFunc("/identity", svc.handleIdentity)
	mux.HandleFunc("/connect", svc.handleConnect)
	mux.HandleFunc("/ops", svc.handleOps)
	mux.HandleFunc("/notes", svc.handleNotes)
	mux.HandleFunc("/notes/", svc.handleNoteByID)
	mux.HandleFunc("/demo", svc.handleDemo)

	svc.server = &http.Server{
		Addr:         fmt.Sprintf("%s:%d", host, cfg.Port),
		Handler:      mux,
		ReadTimeout:  defaultReadTimeout,
		WriteTimeout: defaultWriteTimeout,
	}

	return svc, nil
}

func Run(ctx context.Context, cfg Config) error {
	svc, err := New(cfg)
	if err != nil {
		return err
	}
	return svc.Run(ctx)
}

func (s *Service) Run(ctx context.Context) error {
	return s.run(ctx, nil)
}

func (s *Service) run(ctx context.Context, signalCh <-chan os.Signal) error {
	errCh := make(chan error, 1)
	go func() {
		errCh <- s.server.ListenAndServe()
	}()

	stopSignals := func() {}
	if signalCh == nil {
		c := make(chan os.Signal, 1)
		signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
		signalCh = c
		stopSignals = func() {
			signal.Stop(c)
		}
	}
	defer stopSignals()

	select {
	case err := <-errCh:
		return normalizeServeError(err)
	case <-ctx.Done():
	case <-signalCh:
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), s.shutdownTimeout)
	defer cancel()
	if err := s.server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown server: %w", err)
	}

	return normalizeServeError(<-errCh)
}

func (s *Service) Handler() http.Handler {
	return s.server.Handler
}

func (s *Service) HTTPServer() *http.Server {
	return s.server
}

func normalizeServeError(err error) error {
	if err == nil || errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (s *Service) handleHealth(w http.ResponseWriter, r *http.Request) {
	setCORSHeaders(w, http.MethodGet)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, "ok")
}

func (s *Service) handleOps(w http.ResponseWriter, r *http.Request) {
	setCORSHeaders(w, http.MethodGet, http.MethodPost)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.handleGetOps(w)
	case http.MethodPost:
		s.handlePostOps(w, r)
	default:
		methodNotAllowed(w, http.MethodGet, http.MethodPost)
	}
}

func (s *Service) handleGetOps(w http.ResponseWriter) {
	lines, err := s.log.ReadEncryptedLines()
	if err != nil {
		http.Error(w, fmt.Sprintf("read ops log: %v", err), http.StatusInternalServerError)
		return
	}

	if err := writeJSON(w, http.StatusOK, lines); err != nil {
		http.Error(w, fmt.Sprintf("encode response: %v", err), http.StatusInternalServerError)
	}
}

func (s *Service) handlePostOps(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	var lines []string
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(&lines); err != nil {
		http.Error(w, "invalid request payload", http.StatusBadRequest)
		return
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		http.Error(w, "invalid request payload", http.StatusBadRequest)
		return
	}

	imported, err := s.log.ImportEncryptedLines(lines)
	if err != nil {
		if errors.Is(err, store.ErrInvalidImportPayload) || errors.Is(err, store.ErrOperationIDCollision) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		http.Error(w, fmt.Sprintf("import ops: %v", err), http.StatusInternalServerError)
		return
	}

	if err := writeJSON(w, http.StatusOK, map[string]int{"imported": imported}); err != nil {
		http.Error(w, fmt.Sprintf("encode response: %v", err), http.StatusInternalServerError)
	}
}

func methodNotAllowed(w http.ResponseWriter, allowedMethods ...string) {
	w.Header().Set("Allow", strings.Join(allowedMethods, ", "))
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

func writeJSON(w http.ResponseWriter, status int, v any) error {
	payload, err := json.Marshal(v)
	if err != nil {
		return err
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, err = w.Write(append(payload, '\n'))
	return err
}

func setCORSHeaders(w http.ResponseWriter, allowedMethods ...string) {
	methods := append([]string{}, allowedMethods...)
	if !containsMethod(methods, http.MethodOptions) {
		methods = append(methods, http.MethodOptions)
	}
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", strings.Join(methods, ", "))
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
}

func containsMethod(methods []string, method string) bool {
	for _, candidate := range methods {
		if candidate == method {
			return true
		}
	}
	return false
}
