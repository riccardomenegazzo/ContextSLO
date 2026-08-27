package server

import (
	"bytes"
	"encoding/json"
	"github.com/riccardomenegazzo/ContextSLO/internal/domain"
	"github.com/riccardomenegazzo/ContextSLO/internal/engine"
	"github.com/riccardomenegazzo/ContextSLO/internal/store"
	"net/http"
	"net/http/httptest"
	"testing"
)

func testServer(t *testing.T) *Server {
	t.Helper()
	slo := domain.SLO{Name: "orders-context", Service: "orders-api", Cluster: "kind", Target: 99.5, LatencyP95MS: 10000, RequiredContext: domain.DimensionOrder}
	st, err := store.Open("", slo, nil)
	if err != nil {
		t.Fatal(err)
	}
	return New(st, engine.New(), nil)
}
func TestHealth(t *testing.T) {
	s := testServer(t)
	r := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
}
func TestCreateAndFetchRun(t *testing.T) {
	s := testServer(t)
	r := httptest.NewRequest(http.MethodPost, "/api/v1/runs", bytes.NewBufferString(`{"scenario":"mcp-gap"}`))
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var run domain.Run
	if err := json.NewDecoder(w.Body).Decode(&run); err != nil {
		t.Fatal(err)
	}
	get := httptest.NewRequest(http.MethodGet, "/api/v1/runs/"+run.ID, nil)
	got := httptest.NewRecorder()
	s.Handler().ServeHTTP(got, get)
	if got.Code != http.StatusOK {
		t.Fatalf("get status %d", got.Code)
	}
}
func TestRejectsUnknownScenario(t *testing.T) {
	s := testServer(t)
	r := httptest.NewRequest(http.MethodPost, "/api/v1/runs", bytes.NewBufferString(`{"scenario":"attack"}`))
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status %d", w.Code)
	}
}
