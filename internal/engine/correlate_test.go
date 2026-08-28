package engine

import (
	"github.com/riccardomenegazzo/ContextSLO/internal/domain"
	"testing"
	"time"
)

func TestCorrelateUsesIndependentEvidence(t *testing.T) {
	now := time.Now()
	truth := make([]domain.TruthEvent, 0, len(domain.DimensionOrder))
	obs := make([]domain.Observation, 0, len(domain.DimensionOrder))
	for _, d := range domain.DimensionOrder {
		truth = append(truth, domain.TruthEvent{Marker: "CTX-123456", Dimension: d, OccurredAt: now, Success: true, Kind: string(d), Evidence: "truth"})
		obs = append(obs, domain.Observation{Marker: "CTX-123456", Dimension: d, ReceivedAt: now.Add(time.Second), Attributed: true, Source: "test", Evidence: "observed"})
	}
	session := domain.ValidationSession{Marker: "CTX-123456", Mode: "live", CreatedAt: now, Truth: truth, Observations: obs}
	run := New().Correlate(testSLO(), session, 100)
	if run.Status != "passing" || run.Score != 100 {
		t.Fatalf("unexpected run: %#v", run)
	}
}
func TestCorrelateDetectsMissingAttribution(t *testing.T) {
	now := time.Now()
	session := domain.ValidationSession{Marker: "CTX-123456", Mode: "live", CreatedAt: now, Truth: []domain.TruthEvent{{Dimension: domain.Process, OccurredAt: now, Success: true}}}
	slo := testSLO()
	slo.RequiredContext = []domain.Dimension{domain.Process}
	run := New().Correlate(slo, session, 100)
	if run.Status != "failing" || len(run.Breaks) != 1 {
		t.Fatalf("unexpected run: %#v", run)
	}
}
