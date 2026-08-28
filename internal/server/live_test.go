package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/riccardomenegazzo/ContextSLO/internal/domain"
	"github.com/riccardomenegazzo/ContextSLO/internal/engine"
	"github.com/riccardomenegazzo/ContextSLO/internal/probe"
	"github.com/riccardomenegazzo/ContextSLO/internal/store"
)

func TestLiveSessionCorrelation(t *testing.T) {
	s := testServer(t)
	created := httptest.NewRecorder()
	s.Handler().ServeHTTP(created, httptest.NewRequest(http.MethodPost, "/api/v1/sessions", bytes.NewBufferString(`{"cluster":"test","service":"orders"}`)))
	if created.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", created.Code, created.Body.String())
	}
	var session domain.ValidationSession
	_ = json.NewDecoder(created.Body).Decode(&session)
	now := time.Now()
	truth := make([]domain.TruthEvent, 0, len(domain.DimensionOrder))
	for _, dimension := range domain.DimensionOrder {
		truth = append(truth, domain.TruthEvent{ID: session.Marker + string(dimension), Marker: session.Marker, Cluster: "test", Dimension: dimension, OccurredAt: now, Kind: string(dimension), Success: true})
	}
	body, _ := json.Marshal(map[string]any{"events": truth})
	truthResponse := httptest.NewRecorder()
	s.Handler().ServeHTTP(truthResponse, httptest.NewRequest(http.MethodPost, "/api/v1/truth", bytes.NewReader(body)))
	if truthResponse.Code != http.StatusAccepted {
		t.Fatalf("truth: %d %s", truthResponse.Code, truthResponse.Body.String())
	}
	for _, dimension := range domain.DimensionOrder {
		observation := domain.Observation{ID: "obs-" + string(dimension), Marker: session.Marker, Cluster: "test", Source: "test", Dimension: dimension, OccurredAt: now, ReceivedAt: now.Add(time.Second), Attributed: true}
		data, _ := json.Marshal(observation)
		response := httptest.NewRecorder()
		s.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/ingest", bytes.NewReader(data)))
		if response.Code != http.StatusAccepted {
			t.Fatalf("observation: %d %s", response.Code, response.Body.String())
		}
	}
	correlated := httptest.NewRecorder()
	s.Handler().ServeHTTP(correlated, httptest.NewRequest(http.MethodPost, "/api/v1/sessions/"+session.Marker+"/correlate", bytes.NewBufferString(`{}`)))
	if correlated.Code != http.StatusOK {
		t.Fatalf("correlate: %d %s", correlated.Code, correlated.Body.String())
	}
	var run domain.Run
	_ = json.NewDecoder(correlated.Body).Decode(&run)
	if run.Score != 100 || run.Status != "passing" {
		t.Fatalf("unexpected run: %#v", run)
	}
}

func TestWriteAuthentication(t *testing.T) {
	slo := domain.SLO{Name: "test", Service: "orders", Target: 99.5, LatencyP95MS: 10000, RequiredContext: domain.DimensionOrder}
	st, _ := store.Open("", slo, nil)
	s := NewWithConfig(st, engine.New(), probe.New(), nil, Config{APIToken: "secret"})
	denied := httptest.NewRecorder()
	s.Handler().ServeHTTP(denied, httptest.NewRequest(http.MethodPost, "/api/v1/sessions", bytes.NewBufferString(`{}`)))
	if denied.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", denied.Code)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", bytes.NewBufferString(`{}`))
	request.Header.Set("Authorization", "Bearer secret")
	allowed := httptest.NewRecorder()
	s.Handler().ServeHTTP(allowed, request)
	if allowed.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", allowed.Code, allowed.Body.String())
	}
}
