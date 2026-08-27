package domain

import "time"

type Dimension string

const (
	Process    Dimension = "process"
	Filesystem Dimension = "filesystem"
	Network    Dimension = "network"
	DNS        Dimension = "dns"
	Kubernetes Dimension = "kubernetes"
	CloudIAM   Dimension = "cloudIdentity"
	API        Dimension = "api"
	MCP        Dimension = "mcp"
)

var DimensionOrder = []Dimension{Process, Filesystem, Network, DNS, Kubernetes, CloudIAM, API, MCP}

type SLO struct {
	Name            string      `json:"name"`
	Service         string      `json:"service"`
	Cluster         string      `json:"cluster"`
	Target          float64     `json:"target"`
	LatencyP95MS    int         `json:"latencyP95Ms"`
	RequiredContext []Dimension `json:"requiredContext"`
	Window          string      `json:"window"`
	Tier            string      `json:"tier"`
}

type Check struct {
	ID          string    `json:"id"`
	Dimension   Dimension `json:"dimension"`
	Label       string    `json:"label"`
	Description string    `json:"description"`
	GroundTruth bool      `json:"groundTruth"`
	Observed    bool      `json:"observed"`
	Attributed  bool      `json:"attributed"`
	LatencyMS   int       `json:"latencyMs"`
	Source      string    `json:"source"`
	Evidence    string    `json:"evidence"`
}

type Node struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Kind     string `json:"kind"`
	Subtitle string `json:"subtitle"`
}
type Edge struct {
	From      string `json:"from"`
	To        string `json:"to"`
	Label     string `json:"label"`
	Preserved bool   `json:"preserved"`
}
type ContextBreak struct {
	From        string `json:"from"`
	To          string `json:"to"`
	Dimension   string `json:"dimension"`
	Explanation string `json:"explanation"`
	Remediation string `json:"remediation"`
}
type DimensionScore struct {
	Dimension Dimension `json:"dimension"`
	Score     float64   `json:"score"`
	Observed  int       `json:"observed"`
	Expected  int       `json:"expected"`
	LatencyMS int       `json:"latencyMs"`
}
type ErrorBudget struct {
	Allowed       float64 `json:"allowed"`
	Consumed      float64 `json:"consumed"`
	Remaining     float64 `json:"remaining"`
	ConsumedRatio float64 `json:"consumedRatio"`
	Exhausted     bool    `json:"exhausted"`
}

type Run struct {
	ID            string           `json:"id"`
	Marker        string           `json:"marker"`
	Scenario      string           `json:"scenario"`
	Status        string           `json:"status"`
	StartedAt     time.Time        `json:"startedAt"`
	CompletedAt   time.Time        `json:"completedAt"`
	Score         float64          `json:"score"`
	Target        float64          `json:"target"`
	LatencyP95MS  int              `json:"latencyP95Ms"`
	Checks        []Check          `json:"checks"`
	Dimensions    []DimensionScore `json:"dimensions"`
	Nodes         []Node           `json:"nodes"`
	Edges         []Edge           `json:"edges"`
	Breaks        []ContextBreak   `json:"breaks"`
	ErrorBudget   ErrorBudget      `json:"errorBudget"`
	Regression    float64          `json:"regression"`
	BaselineScore float64          `json:"baselineScore"`
	Summary       string           `json:"summary"`
}

type Overview struct {
	SLO             SLO       `json:"slo"`
	Latest          Run       `json:"latest"`
	History         []Run     `json:"history"`
	TotalRuns       int       `json:"totalRuns"`
	PassingRuns     int       `json:"passingRuns"`
	ContextCoverage float64   `json:"contextCoverage"`
	LastValidatedAt time.Time `json:"lastValidatedAt"`
}
