package engine

import (
	"fmt"
	"strings"
	"time"

	"github.com/riccardomenegazzo/ContextSLO/internal/domain"
)

func GenerateMarker() string { return marker() }

// Correlate evaluates independent truth against normalized telemetry.
func (e *Engine) Correlate(slo domain.SLO, session domain.ValidationSession, baseline float64) domain.Run {
	checks := make([]domain.Check, 0, len(slo.RequiredContext))
	breaks := make([]domain.ContextBreak, 0)
	for _, dimension := range slo.RequiredContext {
		truth := firstTruth(session.Truth, dimension)
		observation := bestObservation(session.Observations, dimension)
		check := domain.Check{ID: string(dimension), Dimension: dimension, Label: dimensionLabel(dimension), Description: "Required context attribution", GroundTruth: truth != nil && truth.Success}
		if truth != nil {
			check.Description = truth.Kind
			check.Evidence = truth.Evidence
			check.Source = "truth/" + truth.Kind
		}
		if observation != nil {
			check.Observed = true
			check.Attributed = observation.Attributed
			check.Source = observation.Source
			check.Evidence = observation.Evidence
			if truth != nil {
				check.LatencyMS = int(observation.ReceivedAt.Sub(truth.OccurredAt).Milliseconds())
				if check.LatencyMS < 0 {
					check.LatencyMS = 0
				}
			}
		}
		checks = append(checks, check)
		if !check.GroundTruth || !check.Observed || !check.Attributed {
			reason := "The canary could not establish ground truth for this dimension."
			remediation := "Verify the probe configuration and required runtime permissions."
			if check.GroundTruth && !check.Observed {
				reason = "No matching telemetry arrived before the correlation deadline."
				remediation = "Verify the adapter, marker propagation, and telemetry export window."
			}
			if check.Observed && !check.Attributed {
				reason = "Telemetry was observed but did not retain the originating workload or identity."
				remediation = "Enable context enrichment and propagate the ContextSLO marker across this boundary."
			}
			breaks = append(breaks, domain.ContextBreak{From: previousHop(dimension), To: dimensionLabel(dimension), Dimension: dimensionLabel(dimension), Explanation: reason, Remediation: remediation})
		}
	}
	dimensions, score, latency := scoreChecks(checks)
	status := "passing"
	if score < slo.Target || latency > slo.LatencyP95MS {
		status = "failing"
	}
	budget := errorBudget(slo.Target, score)
	regression := 0.0
	if baseline > 0 {
		regression = round(score - baseline)
	}
	nodes, edges := liveGraph(checks)
	summary := "All required context was preserved end to end."
	if len(breaks) > 0 {
		summary = fmt.Sprintf("%d context break%s detected from independent ground truth.", len(breaks), map[bool]string{true: "", false: "s"}[len(breaks) == 1])
	}
	completed := e.now().UTC()
	if !session.CompletedAt.IsZero() {
		completed = session.CompletedAt
	}
	return domain.Run{ID: "run-" + strings.ToLower(session.Marker[4:]), Marker: session.Marker, Scenario: session.Mode, Status: status, StartedAt: session.CreatedAt, CompletedAt: completed, Score: score, Target: slo.Target, LatencyP95MS: latency, Checks: checks, Dimensions: dimensions, Nodes: nodes, Edges: edges, Breaks: breaks, ErrorBudget: budget, Regression: regression, BaselineScore: baseline, Summary: summary}
}

func firstTruth(events []domain.TruthEvent, dimension domain.Dimension) *domain.TruthEvent {
	for i := range events {
		if events[i].Dimension == dimension {
			return &events[i]
		}
	}
	return nil
}
func bestObservation(events []domain.Observation, dimension domain.Dimension) *domain.Observation {
	var best *domain.Observation
	for i := range events {
		if events[i].Dimension != dimension {
			continue
		}
		if best == nil || (!best.Attributed && events[i].Attributed) {
			best = &events[i]
		}
	}
	return best
}
func dimensionLabel(d domain.Dimension) string {
	labels := map[domain.Dimension]string{domain.Process: "Process", domain.Filesystem: "Filesystem", domain.Network: "Network", domain.DNS: "DNS", domain.Kubernetes: "Kubernetes", domain.CloudIAM: "Cloud identity", domain.API: "API", domain.MCP: "MCP"}
	return labels[d]
}
func previousHop(d domain.Dimension) string {
	hops := map[domain.Dimension]string{domain.Process: "canary", domain.Filesystem: "process", domain.Network: "workload", domain.DNS: "workload", domain.Kubernetes: "process", domain.CloudIAM: "workload", domain.API: "network", domain.MCP: "agent"}
	return hops[d]
}
func liveGraph(checks []domain.Check) ([]domain.Node, []domain.Edge) {
	nodes := []domain.Node{{ID: "canary", Label: "Synthetic canary", Kind: "principal", Subtitle: "signed marker"}, {ID: "workload", Label: "Workload", Kind: "workload", Subtitle: "Kubernetes context"}, {ID: "process", Label: "Process", Kind: "process", Subtitle: "kernel lineage"}, {ID: "api", Label: "API", Kind: "resource", Subtitle: "L7 endpoint"}, {ID: "identity", Label: "Cloud identity", Kind: "identity", Subtitle: "runtime principal"}, {ID: "mcp", Label: "MCP tool", Kind: "agent", Subtitle: "agent action"}}
	preserved := map[domain.Dimension]bool{}
	for _, c := range checks {
		preserved[c.Dimension] = c.GroundTruth && c.Observed && c.Attributed
	}
	edges := []domain.Edge{{From: "canary", To: "workload", Label: "workload", Preserved: preserved[domain.Kubernetes]}, {From: "workload", To: "process", Label: "lineage", Preserved: preserved[domain.Process]}, {From: "process", To: "api", Label: "request", Preserved: preserved[domain.API] && preserved[domain.Network]}, {From: "api", To: "identity", Label: "assume role", Preserved: preserved[domain.CloudIAM]}, {From: "identity", To: "mcp", Label: "tool call", Preserved: preserved[domain.MCP]}}
	return nodes, edges
}

var _ = time.Second
