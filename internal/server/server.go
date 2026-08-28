package server

import (
	"crypto/subtle"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/riccardomenegazzo/ContextSLO/internal/adapters"
	"github.com/riccardomenegazzo/ContextSLO/internal/domain"
	"github.com/riccardomenegazzo/ContextSLO/internal/engine"
	"github.com/riccardomenegazzo/ContextSLO/internal/probe"
	"github.com/riccardomenegazzo/ContextSLO/internal/store"
)

//go:embed web/*
var assets embed.FS

type Config struct {
	APIToken, CloudProbeURL, MCPProbeURL, IntegrationToken, DNSName string
	CollectionWait                                                  time.Duration
	MaxCollectionWait                                               time.Duration
}
type Server struct {
	store    *store.Store
	engine   *engine.Engine
	runner   *probe.Runner
	logger   *slog.Logger
	config   Config
	handler  http.Handler
	requests atomic.Uint64
	ingested atomic.Uint64
	failed   atomic.Uint64
}

func New(st *store.Store, eng *engine.Engine, logger *slog.Logger) *Server {
	return NewWithConfig(st, eng, probe.New(), logger, Config{})
}
func NewWithConfig(st *store.Store, eng *engine.Engine, runner *probe.Runner, logger *slog.Logger, config Config) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	if runner == nil {
		runner = probe.New()
	}
	if config.CollectionWait <= 0 {
		config.CollectionWait = 2 * time.Second
	}
	if config.MaxCollectionWait <= 0 {
		config.MaxCollectionWait = 30 * time.Second
	}
	s := &Server{store: st, engine: eng, runner: runner, logger: logger, config: config}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /readyz", s.health)
	mux.HandleFunc("GET /metrics", s.metrics)
	mux.HandleFunc("GET /api/v1/meta", s.meta)
	mux.HandleFunc("GET /api/v1/overview", s.overview)
	mux.HandleFunc("GET /api/v1/runs", s.runs)
	mux.HandleFunc("POST /api/v1/runs", s.createRun)
	mux.HandleFunc("GET /api/v1/runs/{id}", s.run)
	mux.HandleFunc("GET /api/v1/slo", s.getSLO)
	mux.HandleFunc("PUT /api/v1/slo", s.updateSLO)
	mux.HandleFunc("GET /api/v1/clusters", s.clusters)
	mux.HandleFunc("GET /api/v1/sessions", s.sessions)
	mux.HandleFunc("POST /api/v1/sessions", s.createSession)
	mux.HandleFunc("GET /api/v1/sessions/{marker}", s.session)
	mux.HandleFunc("POST /api/v1/sessions/{marker}/correlate", s.correlateSession)
	mux.HandleFunc("POST /api/v1/truth", s.ingestTruth)
	mux.HandleFunc("POST /api/v1/ingest", s.ingestNormalized)
	mux.HandleFunc("POST /api/v1/ingest/{adapter}", s.ingestAdapter)
	static, _ := fs.Sub(assets, "web")
	mux.Handle("/", http.FileServer(http.FS(static)))
	s.handler = s.middleware(mux)
	return s
}
func (s *Server) Handler() http.Handler { return s.handler }

func (s *Server) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		s.requests.Add(1)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		if isWrite(r.Method) && strings.HasPrefix(r.URL.Path, "/api/") && s.config.APIToken != "" && !s.authorized(r) {
			s.failed.Add(1)
			w.Header().Set("WWW-Authenticate", `Bearer realm="ContextSLO"`)
			writeError(w, http.StatusUnauthorized, "valid bearer token required")
			return
		}
		next.ServeHTTP(w, r)
		s.logger.Debug("request", "method", r.Method, "path", r.URL.Path, "duration", time.Since(started))
	})
}
func (s *Server) authorized(r *http.Request) bool {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if token == "" {
		token = r.Header.Get("X-ContextSLO-Token")
	}
	return len(token) == len(s.config.APIToken) && subtle.ConstantTimeCompare([]byte(token), []byte(s.config.APIToken)) == 1
}
func isWrite(method string) bool {
	return method == http.MethodPost || method == http.MethodPut || method == http.MethodPatch || method == http.MethodDelete
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "service": "contextslo", "time": time.Now().UTC()})
}
func (s *Server) metrics(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	fmt.Fprintf(w, "# HELP contextslo_http_requests_total HTTP requests received.\n# TYPE contextslo_http_requests_total counter\ncontextslo_http_requests_total %d\n# HELP contextslo_observations_ingested_total Normalized observations accepted.\n# TYPE contextslo_observations_ingested_total counter\ncontextslo_observations_ingested_total %d\n# HELP contextslo_request_failures_total Rejected or failed requests.\n# TYPE contextslo_request_failures_total counter\ncontextslo_request_failures_total %d\n", s.requests.Load(), s.ingested.Load(), s.failed.Load())
}
func (s *Server) meta(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"authRequired": s.config.APIToken != "", "modes": []string{"demo", "live"}, "adapters": []string{"generic", "falco", "tetragon", "otlp", "cloudtrail", "mcp"}})
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
func (s *Server) clusters(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"clusters": s.store.Clusters()})
}
func (s *Server) sessions(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"sessions": s.store.Sessions()})
}
func (s *Server) session(w http.ResponseWriter, r *http.Request) {
	session, ok := s.store.Session(strings.ToUpper(r.PathValue("marker")))
	if !ok {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	writeJSON(w, http.StatusOK, session)
}

type runInput struct {
	Mode        string      `json:"mode"`
	Scenario    string      `json:"scenario"`
	Cluster     string      `json:"cluster"`
	Service     string      `json:"service"`
	SLO         *domain.SLO `json:"slo,omitempty"`
	WaitSeconds int         `json:"waitSeconds"`
}

func (s *Server) createRun(w http.ResponseWriter, r *http.Request) {
	var input runInput
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if input.Mode == "" {
		input.Mode = "demo"
	}
	if input.Mode == "demo" {
		allowed := map[string]bool{"": true, "baseline": true, "identity-gap": true, "mcp-gap": true, "latency-regression": true}
		if !allowed[input.Scenario] {
			writeError(w, http.StatusBadRequest, "unknown scenario")
			return
		}
		run := s.engine.Run(s.store.SLO(), input.Scenario, s.store.Baseline())
		if err := s.store.Add(run); err != nil {
			s.fail(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, run)
		return
	}
	if input.Mode != "live" {
		writeError(w, http.StatusBadRequest, "mode must be demo or live")
		return
	}
	session, err := s.newSession(input)
	if err != nil {
		s.fail(w, err)
		return
	}
	events, err := s.runner.Run(r.Context(), probe.Request{Marker: session.Marker, Cluster: session.Cluster, Service: session.Service, DNSName: s.config.DNSName, CloudURL: s.config.CloudProbeURL, MCPURL: s.config.MCPProbeURL, Token: s.config.IntegrationToken})
	if err != nil {
		s.fail(w, err)
		return
	}
	for _, event := range events {
		if err = s.store.AddTruth(event); err != nil {
			s.fail(w, err)
			return
		}
	}
	wait := s.config.CollectionWait
	if input.WaitSeconds > 0 {
		wait = time.Duration(input.WaitSeconds) * time.Second
	}
	if wait > s.config.MaxCollectionWait {
		wait = s.config.MaxCollectionWait
	}
	timer := time.NewTimer(wait)
	select {
	case <-r.Context().Done():
		timer.Stop()
		writeError(w, 499, "request cancelled")
		return
	case <-timer.C:
	}
	run, err := s.correlate(session.Marker)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, run)
}

func (s *Server) newSession(input runInput) (domain.ValidationSession, error) {
	slo := s.store.SLO()
	if input.SLO != nil {
		slo = *input.SLO
		if slo.Target <= 0 || slo.Target > 100 || slo.LatencyP95MS <= 0 || !validDimensions(slo.RequiredContext) {
			return domain.ValidationSession{}, fmt.Errorf("session SLO requires target, latencyP95Ms, and requiredContext")
		}
	}
	cluster := input.Cluster
	if cluster == "" {
		cluster = slo.Cluster
	}
	service := input.Service
	if service == "" {
		service = slo.Service
	}
	slo.Cluster = cluster
	slo.Service = service
	sloSnapshot := slo
	marker := engine.GenerateMarker()
	session := domain.ValidationSession{ID: "session-" + strings.ToLower(marker[4:]), Marker: marker, Mode: "live", Cluster: cluster, Service: service, SLO: &sloSnapshot, Status: "collecting", CreatedAt: time.Now().UTC(), Deadline: time.Now().UTC().Add(s.config.MaxCollectionWait)}
	return session, s.store.CreateSession(session)
}
func (s *Server) createSession(w http.ResponseWriter, r *http.Request) {
	var input runInput
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	input.Mode = "live"
	session, err := s.newSession(input)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, session)
}
func (s *Server) correlateSession(w http.ResponseWriter, r *http.Request) {
	run, err := s.correlate(strings.ToUpper(r.PathValue("marker")))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, run)
}
func (s *Server) correlate(marker string) (domain.Run, error) {
	session, ok := s.store.Session(marker)
	if !ok {
		return domain.Run{}, fmt.Errorf("session %s not found", marker)
	}
	session.CompletedAt = time.Now().UTC()
	slo := s.store.SLO()
	if session.SLO != nil {
		slo = *session.SLO
	}
	run := s.engine.Correlate(slo, session, s.store.Baseline())
	if err := s.store.CompleteSession(marker, run); err != nil {
		return domain.Run{}, err
	}
	return run, nil
}

func (s *Server) ingestTruth(w http.ResponseWriter, r *http.Request) {
	var envelope struct {
		Events []domain.TruthEvent `json:"events"`
	}
	if err := decodeJSON(w, r, &envelope); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(envelope.Events) == 0 {
		writeError(w, http.StatusUnprocessableEntity, "events are required")
		return
	}
	for i := range envelope.Events {
		event := &envelope.Events[i]
		event.Marker = strings.ToUpper(event.Marker)
		if event.ID == "" {
			event.ID = event.Marker + "-" + string(event.Dimension) + "-" + strconv.Itoa(i)
		}
		if event.OccurredAt.IsZero() {
			event.OccurredAt = time.Now().UTC()
		}
		if err := s.store.AddTruth(*event); err != nil {
			s.fail(w, err)
			return
		}
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"accepted": len(envelope.Events)})
}
func (s *Server) ingestNormalized(w http.ResponseWriter, r *http.Request) {
	var event domain.Observation
	if err := decodeJSON(w, r, &event); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if event.Marker == "" || event.Source == "" || event.Dimension == "" {
		writeError(w, http.StatusUnprocessableEntity, "marker, source, and dimension are required")
		return
	}
	event.Marker = strings.ToUpper(event.Marker)
	if event.ID == "" {
		event.ID = event.Source + ":" + event.Marker + ":" + string(event.Dimension)
	}
	if event.ReceivedAt.IsZero() {
		event.ReceivedAt = time.Now().UTC()
	}
	if event.OccurredAt.IsZero() {
		event.OccurredAt = event.ReceivedAt
	}
	if err := s.store.AddObservation(event); err != nil {
		s.fail(w, err)
		return
	}
	s.ingested.Add(1)
	writeJSON(w, http.StatusAccepted, map[string]any{"accepted": 1, "marker": event.Marker, "receivedAt": event.ReceivedAt})
}
func (s *Server) ingestAdapter(w http.ResponseWriter, r *http.Request) {
	adapter := strings.ToLower(r.PathValue("adapter"))
	allowed := map[string]bool{"falco": true, "tetragon": true, "otlp": true, "cloudtrail": true, "mcp": true, "generic": true}
	if !allowed[adapter] {
		writeError(w, http.StatusNotFound, "unknown adapter")
		return
	}
	events, err := adapters.Decode(adapter, r.Body, time.Now().UTC())
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	for _, event := range events {
		if err = s.store.AddObservation(event); err != nil {
			s.fail(w, err)
			return
		}
	}
	s.ingested.Add(uint64(len(events)))
	writeJSON(w, http.StatusAccepted, map[string]any{"accepted": len(events), "markers": markers(events)})
}
func markers(events []domain.Observation) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, event := range events {
		if !seen[event.Marker] {
			seen[event.Marker] = true
			out = append(out, event.Marker)
		}
	}
	return out
}

func validDimensions(dimensions []domain.Dimension) bool {
	if len(dimensions) == 0 {
		return false
	}
	known := map[domain.Dimension]bool{}
	for _, dimension := range domain.DimensionOrder {
		known[dimension] = true
	}
	seen := map[domain.Dimension]bool{}
	for _, dimension := range dimensions {
		if !known[dimension] || seen[dimension] {
			return false
		}
		seen[dimension] = true
	}
	return true
}

func (s *Server) updateSLO(w http.ResponseWriter, r *http.Request) {
	var slo domain.SLO
	if err := decodeJSON(w, r, &slo); err != nil {
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
	if !validDimensions(slo.RequiredContext) {
		writeError(w, http.StatusUnprocessableEntity, "requiredContext contains an unknown or duplicate dimension")
		return
	}
	if err := s.store.UpdateSLO(slo); err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, slo)
}
func (s *Server) fail(w http.ResponseWriter, err error) {
	s.failed.Add(1)
	s.logger.Error("request failed", "error", err)
	writeError(w, http.StatusInternalServerError, "operation failed")
}
func decodeJSON(w http.ResponseWriter, r *http.Request, destination any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 4<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
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
