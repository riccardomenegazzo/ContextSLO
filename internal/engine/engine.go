package engine

import (
	"crypto/rand"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/riccardomenegazzo/ContextSLO/internal/domain"
)

type Engine struct{ now func() time.Time }

func New() *Engine { return &Engine{now: time.Now} }

func (e *Engine) Run(slo domain.SLO, scenario string, baseline float64) domain.Run {
	if scenario == "" {
		scenario = "identity-gap"
	}
	now := e.now().UTC()
	marker := marker()
	checks := checksFor(scenario, marker)
	dimensions, score, latency := scoreChecks(checks)
	nodes, edges := graphFor(scenario)
	breaks := breaksFor(scenario)
	budget := errorBudget(slo.Target, score)
	status := "passing"
	if score < slo.Target || latency > slo.LatencyP95MS {
		status = "failing"
	}
	summary := "All required context was preserved end to end."
	if len(breaks) == 1 {
		summary = "1 context break detected between runtime activity and its security attribution."
	}
	if len(breaks) > 1 {
		summary = fmt.Sprintf("%d context breaks detected across the validation path.", len(breaks))
	}
	regression := 0.0
	if baseline > 0 {
		regression = round(score - baseline)
	}
	return domain.Run{ID: fmt.Sprintf("run-%d-%s", now.Unix(), strings.ToLower(marker[4:])), Marker: marker, Scenario: scenario, Status: status,
		StartedAt: now.Add(-time.Duration(latency+480) * time.Millisecond), CompletedAt: now, Score: score, Target: slo.Target, LatencyP95MS: latency,
		Checks: checks, Dimensions: dimensions, Nodes: nodes, Edges: edges, Breaks: breaks, ErrorBudget: budget, Regression: regression, BaselineScore: baseline, Summary: summary}
}

func checksFor(scenario, marker string) []domain.Check {
	checks := []domain.Check{
		{ID: "exec", Dimension: domain.Process, Label: "Process execution", Description: "execve /bin/echo", GroundTruth: true, Observed: true, Attributed: true, LatencyMS: 780, Source: "truth/process", Evidence: marker + " · pid 1842 → 1851"},
		{ID: "file", Dimension: domain.Filesystem, Label: "File access", Description: "read /tmp/context-canary", GroundTruth: true, Observed: true, Attributed: true, LatencyMS: 920, Source: "truth/filesystem", Evidence: marker + " · orders-api:/tmp/context-canary"},
		{ID: "network", Dimension: domain.Network, Label: "Network flow", Description: "TCP connection to storage peer", GroundTruth: true, Observed: true, Attributed: true, LatencyMS: 1120, Source: "otel/network", Evidence: marker + " · 10.42.2.17:41822 → 10.42.3.8:443"},
		{ID: "dns", Dimension: domain.DNS, Label: "DNS resolution", Description: "Resolve storage.internal", GroundTruth: true, Observed: true, Attributed: true, LatencyMS: 840, Source: "truth/dns", Evidence: marker + " · storage.internal A 10.42.3.8"},
		{ID: "workload", Dimension: domain.Kubernetes, Label: "Workload identity", Description: "Resolve pod and ServiceAccount", GroundTruth: true, Observed: true, Attributed: true, LatencyMS: 1360, Source: "kubernetes/audit", Evidence: marker + " · prod/orders-api · sa/orders-api"},
		{ID: "api", Dimension: domain.API, Label: "API transaction", Description: "POST /v1/orders/:id/validate", GroundTruth: true, Observed: true, Attributed: true, LatencyMS: 1710, Source: "otel/http", Evidence: marker + " · POST /v1/orders/82fa/validate → 200"},
		{ID: "cloud", Dimension: domain.CloudIAM, Label: "Cloud identity", Description: "STS GetCallerIdentity", GroundTruth: true, Observed: true, Attributed: true, LatencyMS: 2420, Source: "cloudtrail", Evidence: marker + " · arn:aws:sts::123456789012:assumed-role/orders"},
		{ID: "mcp", Dimension: domain.MCP, Label: "MCP tool call", Description: "storage.put_object", GroundTruth: true, Observed: true, Attributed: true, LatencyMS: 3180, Source: "mcp/audit", Evidence: marker + " · storage.put_object(bucket=context-demo)"},
	}
	switch scenario {
	case "identity-gap":
		checks[6].Attributed = false
		checks[6].Evidence = marker + " · AWS action observed; originating workload missing"
		checks[7].Observed = false
		checks[7].Attributed = false
		checks[7].Evidence = marker + " · no matching telemetry within window"
	case "mcp-gap":
		checks[7].Attributed = false
		checks[7].Evidence = marker + " · tool call observed; caller identity missing"
	case "latency-regression":
		checks[5].LatencyMS = 11400
		checks[6].LatencyMS = 13800
		checks[7].LatencyMS = 15200
	case "baseline":
	default:
		checks[6].Attributed = false
	}
	return checks
}

func scoreChecks(checks []domain.Check) ([]domain.DimensionScore, float64, int) {
	type acc struct {
		points             float64
		observed, expected int
		latencies          []int
	}
	byDimension := map[domain.Dimension]*acc{}
	allLatencies := make([]int, 0, len(checks))
	total := 0.0
	for _, check := range checks {
		a := byDimension[check.Dimension]
		if a == nil {
			a = &acc{}
			byDimension[check.Dimension] = a
		}
		points := 0.0
		if check.Observed {
			points += 45
			a.observed++
		}
		if check.Attributed {
			points += 55
		}
		a.points += points
		a.expected++
		a.latencies = append(a.latencies, check.LatencyMS)
		total += points
		if check.Observed {
			allLatencies = append(allLatencies, check.LatencyMS)
		}
	}
	dimensions := make([]domain.DimensionScore, 0, len(byDimension))
	for _, d := range domain.DimensionOrder {
		if a := byDimension[d]; a != nil {
			dimensions = append(dimensions, domain.DimensionScore{Dimension: d, Score: round(a.points / float64(a.expected)), Observed: a.observed, Expected: a.expected, LatencyMS: percentile(a.latencies, .95)})
		}
	}
	return dimensions, round(total / float64(len(checks))), percentile(allLatencies, .95)
}

func errorBudget(target, score float64) domain.ErrorBudget {
	allowed := math.Max(0, 100-target)
	consumed := math.Max(0, 100-score)
	ratio := 0.0
	if allowed > 0 {
		ratio = consumed / allowed * 100
	}
	return domain.ErrorBudget{Allowed: round(allowed), Consumed: round(consumed), Remaining: round(math.Max(0, allowed-consumed)), ConsumedRatio: round(ratio), Exhausted: consumed > allowed}
}

func graphFor(scenario string) ([]domain.Node, []domain.Edge) {
	nodes := []domain.Node{{ID: "user", Label: "Customer", Kind: "principal", Subtitle: "HTTP client"}, {ID: "frontend", Label: "frontend", Kind: "workload", Subtitle: "pod · prod"}, {ID: "orders", Label: "orders-api", Kind: "workload", Subtitle: "pod · sa/orders-api"}, {ID: "process", Label: "aws-sdk", Kind: "process", Subtitle: "pid 1851"}, {ID: "agent", Label: "order-agent", Kind: "agent", Subtitle: "MCP client"}, {ID: "iam", Label: "orders-role", Kind: "identity", Subtitle: "AWS IAM session"}, {ID: "s3", Label: "receipts", Kind: "resource", Subtitle: "S3 bucket"}}
	edges := []domain.Edge{{From: "user", To: "frontend", Label: "POST /checkout", Preserved: true}, {From: "frontend", To: "orders", Label: "HTTP /v1/orders", Preserved: true}, {From: "orders", To: "process", Label: "execve", Preserved: true}, {From: "process", To: "agent", Label: "tool request", Preserved: true}, {From: "agent", To: "iam", Label: "assume role", Preserved: true}, {From: "iam", To: "s3", Label: "PutObject", Preserved: true}}
	if scenario == "identity-gap" {
		edges[4].Preserved = false
		edges[4].Label = "identity lost"
		edges[5].Preserved = false
	}
	if scenario == "mcp-gap" {
		edges[3].Preserved = false
		edges[3].Label = "caller lost"
	}
	return nodes, edges
}

func breaksFor(scenario string) []domain.ContextBreak {
	switch scenario {
	case "identity-gap":
		return []domain.ContextBreak{{From: "order-agent", To: "orders-role", Dimension: "Cloud identity", Explanation: "The AWS API action was observed, but its originating Kubernetes workload was not preserved.", Remediation: "Verify projected ServiceAccount token and cloud audit enrichment configuration."}, {From: "aws-sdk", To: "order-agent", Dimension: "MCP", Explanation: "No MCP audit event matched the synthetic marker inside the telemetry window.", Remediation: "Enable MCP tool-call audit export and propagate the ctx-marker attribute."}}
	case "mcp-gap":
		return []domain.ContextBreak{{From: "aws-sdk", To: "order-agent", Dimension: "MCP", Explanation: "The tool call was visible but could not be attributed to its calling process.", Remediation: "Propagate trace context through the MCP client transport."}}
	case "latency-regression":
		return []domain.ContextBreak{{From: "orders-api", To: "telemetry", Dimension: "Latency", Explanation: "Security telemetry exceeded the configured p95 freshness objective.", Remediation: "Inspect exporter queues and collector backpressure."}}
	default:
		return nil
	}
}

func marker() string {
	b := make([]byte, 3)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("CTX-%06X", time.Now().UnixNano()&0xffffff)
	}
	return fmt.Sprintf("CTX-%02X%02X%02X", b[0], b[1], b[2])
}
func percentile(values []int, p float64) int {
	if len(values) == 0 {
		return 0
	}
	v := append([]int(nil), values...)
	sort.Ints(v)
	i := int(math.Ceil(float64(len(v))*p)) - 1
	if i < 0 {
		i = 0
	}
	return v[i]
}
func round(v float64) float64 { return math.Round(v*10) / 10 }
