package server

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/riccardomenegazzo/ContextSLO/internal/domain"
	"github.com/riccardomenegazzo/ContextSLO/internal/engine"
	"github.com/riccardomenegazzo/ContextSLO/internal/store"
)

//go:embed web/*
var assets embed.FS

type Server struct {
	store   *store.Store
	engine  *engine.Engine
	logger  *slog.Logger
	handler http.Handler
}

func New(st *store.Store, eng *engine.Engine, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	s := &Server{store: st, engine: eng, logger: logger}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /api/v1/overview", s.overview)
	mux.HandleFunc("GET /api/v1/runs", s.runs)
	mux.HandleFunc("POST /api/v1/runs", s.createRun)
	mux.HandleFunc("GET /api/v1/runs/{id}", s.run)
	mux.HandleFunc("GET /api/v1/slo", s.getSLO)
	mux.HandleFunc("PUT /api/v1/slo", s.updateSLO)
	mux.HandleFunc("POST /api/v1/ingest", s.ingest)
	static, _ := fs.Sub(assets, "web")
	mux.Handle("/", http.FileServer(http.FS(static)))
	s.handler = s.middleware(mux)
	return s
}
func (s *Server) Handler() http.Handler { return s.handler }

func (s *Server) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
		s.logger.Debug("request", "method", r.Method, "path", r.URL.Path, "duration", time.Since(started))
	})
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "service": "contextslo", "time": time.Now().UTC()})
}
func (s *Server) overview(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.store.Overview())
}
func (s *Server) runs(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"runs": s.store.Runs()})
}
func (s *Server) run(w http.ResponseWriter, r *http.Request) {
	run, ok := s.store.Get(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "run not found")
		return
	}
	writeJSON(w, http.StatusOK, run)
}
func (s *Server) getSLO(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.store.SLO())
}

func (s *Server) createRun(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Scenario string `json:"scenario"`
	}
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	allowed := map[string]bool{"": true, "baseline": true, "identity-gap": true, "mcp-gap": true, "latency-regression": true}
	if !allowed[input.Scenario] {
		writeError(w, http.StatusBadRequest, "unknown scenario")
		return
	}
	run := s.engine.Run(s.store.SLO(), input.Scenario, s.store.Baseline())
	if err := s.store.Add(run); err != nil {
		s.logger.Error("persist run", "error", err)
		writeError(w, http.StatusInternalServerError, "could not persist validation run")
		return
	}
	writeJSON(w, http.StatusCreated, run)
}

func (s *Server) updateSLO(w http.ResponseWriter, r *http.Request) {
	var slo domain.SLO
	if err := decodeJSON(r, &slo); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if slo.Name == "" || slo.Service == "" || slo.Target <= 0 || slo.Target > 100 || slo.LatencyP95MS <= 0 {
		writeError(w, http.StatusUnprocessableEntity, "name, service, target (0-100), and latencyP95Ms are required")
		return
	}
	if len(slo.RequiredContext) == 0 {
		slo.RequiredContext = domain.DimensionOrder
	}
	if err := s.store.UpdateSLO(slo); err != nil {
		writeError(w, http.StatusInternalServerError, "could not persist SLO")
		return
	}
	writeJSON(w, http.StatusOK, slo)
}

func (s *Server) ingest(w http.ResponseWriter, r *http.Request) {
	var event struct {
		Marker     string           `json:"marker"`
		Source     string           `json:"source"`
		Dimension  domain.Dimension `json:"dimension"`
		Observed   bool             `json:"observed"`
		Attributed bool             `json:"attributed"`
		OccurredAt time.Time        `json:"occurredAt"`
	}
	if err := decodeJSON(r, &event); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if event.Marker == "" || event.Source == "" || event.Dimension == "" {
		writeError(w, http.StatusUnprocessableEntity, "marker, source, and dimension are required")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"accepted": true, "marker": event.Marker, "receivedAt": time.Now().UTC()})
}

func decodeJSON(r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(nil, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	return nil
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
