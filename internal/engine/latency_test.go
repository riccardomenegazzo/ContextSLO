package engine

import "testing"

func TestLatencyRegressionFailsFreshnessSLO(t *testing.T) {
	run := New().Run(testSLO(), "latency-regression", 100)
	if run.Status != "failing" {
		t.Fatalf("latency regression should fail, got %s", run.Status)
	}
	if run.LatencyP95MS <= testSLO().LatencyP95MS {
		t.Fatalf("expected latency above objective, got %dms", run.LatencyP95MS)
	}
}
