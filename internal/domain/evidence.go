package domain

import "time"

// TruthEvent is an independently observed fact produced by a canary or kernel sensor.
type TruthEvent struct {
	ID             string            `json:"id"`
	Marker         string            `json:"marker"`
	Cluster        string            `json:"cluster"`
	Dimension      Dimension         `json:"dimension"`
	Kind           string            `json:"kind"`
	OccurredAt     time.Time         `json:"occurredAt"`
	PID            int               `json:"pid,omitempty"`
	ParentPID      int               `json:"parentPid,omitempty"`
	Workload       string            `json:"workload,omitempty"`
	Namespace      string            `json:"namespace,omitempty"`
	ServiceAccount string            `json:"serviceAccount,omitempty"`
	Identity       string            `json:"identity,omitempty"`
	Peer           string            `json:"peer,omitempty"`
	Endpoint       string            `json:"endpoint,omitempty"`
	Evidence       string            `json:"evidence"`
	Success        bool              `json:"success"`
	Error          string            `json:"error,omitempty"`
	Attributes     map[string]string `json:"attributes,omitempty"`
}

// Observation is the normalized boundary shared by every telemetry adapter.
type Observation struct {
	ID             string            `json:"id"`
	Marker         string            `json:"marker"`
	Cluster        string            `json:"cluster"`
	Source         string            `json:"source"`
	Dimension      Dimension         `json:"dimension"`
	OccurredAt     time.Time         `json:"occurredAt"`
	ReceivedAt     time.Time         `json:"receivedAt"`
	Workload       string            `json:"workload,omitempty"`
	Namespace      string            `json:"namespace,omitempty"`
	ServiceAccount string            `json:"serviceAccount,omitempty"`
	Identity       string            `json:"identity,omitempty"`
	Process        string            `json:"process,omitempty"`
	PID            int               `json:"pid,omitempty"`
	Peer           string            `json:"peer,omitempty"`
	Endpoint       string            `json:"endpoint,omitempty"`
	Attributed     bool              `json:"attributed"`
	Evidence       string            `json:"evidence"`
	Attributes     map[string]string `json:"attributes,omitempty"`
}

type ValidationSession struct {
	ID           string        `json:"id"`
	Marker       string        `json:"marker"`
	Mode         string        `json:"mode"`
	Scenario     string        `json:"scenario,omitempty"`
	Cluster      string        `json:"cluster"`
	Service      string        `json:"service"`
	SLO          *SLO          `json:"slo,omitempty"`
	Status       string        `json:"status"`
	CreatedAt    time.Time     `json:"createdAt"`
	Deadline     time.Time     `json:"deadline"`
	CompletedAt  time.Time     `json:"completedAt,omitempty"`
	Truth        []TruthEvent  `json:"truth"`
	Observations []Observation `json:"observations"`
	RunID        string        `json:"runId,omitempty"`
}

type Cluster struct {
	Name          string    `json:"name"`
	LastSeenAt    time.Time `json:"lastSeenAt"`
	TruthEvents   int       `json:"truthEvents"`
	Observations  int       `json:"observations"`
	ActiveSession int       `json:"activeSessions"`
}
