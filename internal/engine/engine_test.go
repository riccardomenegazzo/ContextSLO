package engine

import (
	"github.com/riccardomenegazzo/ContextSLO/internal/domain"
	"testing"
	"time"
)

func testSLO() domain.SLO {
	return domain.SLO{Name: "test", Service: "orders", Target: 99.5, LatencyP95MS: 10000, RequiredContext: domain.DimensionOrder}
}

func TestBaselinePasses(t *testing.T) {
	e := New()
	e.now = func() time.Time { return time.Unix(100, 0) }
	run := e.Run(testSLO(), "baseline", 0)
	if run.Score != 100 || run.Status != "passing" {
		t.Fatalf("expected perfect passing run, got score=%v status=%s", run.Score, run.Status)
	}
	if run.ErrorBudget.Exhausted {
		t.Fatal("baseline should not exhaust the error budget")
	}
}
func TestIdentityGapProducesBreaks(t *testing.T) {
	run := New().Run(testSLO(), "identity-gap", 100)
	if run.Score >= run.Target {
		t.Fatalf("identity gap should fail, got %v", run.Score)
	}
	if len(run.Breaks) != 2 {
		t.Fatalf("expected two context breaks, got %d", len(run.Breaks))
	}
	if !run.ErrorBudget.Exhausted {
		t.Fatal("expected error budget exhaustion")
	}
	if run.Regression >= 0 {
		t.Fatalf("expected negative regression, got %v", run.Regression)
	}
}
func TestPercentile(t *testing.T) {
	if got := percentile([]int{10, 20, 30, 40}, .95); got != 40 {
		t.Fatalf("got %d", got)
	}
	if got := percentile(nil, .95); got != 0 {
		t.Fatalf("empty percentile = %d", got)
	}
}
