package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/riccardomenegazzo/ContextSLO/internal/domain"
)

func TestSessionSnapshotsCustomSLO(t *testing.T) {
	s := testServer(t)
	body := bytes.NewBufferString(`{"cluster":"edge","service":"checkout","slo":{"name":"checkout-context","service":"checkout","cluster":"edge","target":98.7,"latencyP95Ms":3000,"requiredContext":["network"],"window":"7d","tier":"Tier 2"}}`)
	response := httptest.NewRecorder()
	s.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/sessions", body))
	if response.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", response.Code, response.Body.String())
	}
	var session domain.ValidationSession
	if err := json.NewDecoder(response.Body).Decode(&session); err != nil {
		t.Fatal(err)
	}
	if session.SLO == nil || session.SLO.Target != 98.7 || len(session.SLO.RequiredContext) != 1 || session.SLO.RequiredContext[0] != domain.Network {
		t.Fatalf("snapshot = %#v", session.SLO)
	}
}
