package server

import (
	_ "embed"
	"net/http"
)

//go:embed demo.html
var demoPageHTML []byte

func (s *Service) handleDemo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(demoPageHTML)
}
