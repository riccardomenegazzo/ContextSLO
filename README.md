<div align="center">

# ContextSLO

### Your security agent is healthy. But is your security visibility?

**ContextSLO proves whether cloud security telemetry preserves enough runtime context to explain what actually happened.**

Synthetic monitoring and SLOs—but for security visibility.

[![CI](https://github.com/riccardomenegazzo/ContextSLO/actions/workflows/ci.yml/badge.svg)](https://github.com/riccardomenegazzo/ContextSLO/actions/workflows/ci.yml)
[![Go 1.27](https://img.shields.io/badge/Go-1.27-00ADD8?logo=go)](https://go.dev/)
[![eBPF](https://img.shields.io/badge/eBPF-CO--RE-b7f268)](agent/ebpf/context_truth_full.bpf.c)
[![License: MIT](https://img.shields.io/badge/License-MIT-b7f268.svg)](LICENSE)

</div>

---

Sensor health is an implementation detail. ContextSLO measures the outcome: can a security stack connect a real action to the correct workload, process, identity, API, cloud resource, and business service?

```text
real canary ──► independent truth ──┐
                                   ├──► marker correlator ──► Security Context SLO
security telemetry ─► adapters ────┘
```

ContextSLO includes the complete path: real safe probes, persisted validation sessions, vendor adapters, correlation, scoring, dashboard, CLI, Kubernetes operator, CO-RE eBPF sensor, authentication, metrics, deployment resources, and CI regression gates.

## Start in two minutes

```bash
docker compose up --build
```

Open [http://localhost:8080](http://localhost:8080). The seeded dashboard is immediately usable. Choose **Live telemetry validation** to execute real process, filesystem, DNS, TCP, HTTP, Kubernetes-identity, cloud, and MCP probes.

The cloud and MCP probes are intentionally unsuccessful until their endpoints are configured. That is a real failed ground-truth requirement, not fabricated telemetry.

## Two validation modes

### Live mode

Live mode is the production data path:

1. allocate a cryptographically random `CTX-*` marker;
2. create a durable collection session;
3. execute benign process, file, DNS, network, HTTP, workload-identity, cloud, and MCP activity;
4. accept matching Falco, Tetragon, OTLP, CloudTrail, MCP, eBPF, or generic evidence;
5. deduplicate and correlate observations before the deadline;
6. score observation, attribution, latency, regressions, and error-budget consumption.

```bash
curl -X POST http://localhost:8080/api/v1/runs \
  -H 'Content-Type: application/json' \
  -d '{"mode":"live","cluster":"kind","service":"orders-api","waitSeconds":10}'
```

### Demo mode

Demo mode remains deterministic for interviews, screenshots, and UI development. It never claims to be external telemetry.

```bash
contextslo validate --scenario baseline
contextslo validate --scenario identity-gap
```

## What is measured

| Dimension | Safe ground truth | Attribution expected from telemetry |
|---|---|---|
| Process | child `execve` | PID, process, parent or workload |
| Filesystem | temporary marker file | process or workload |
| DNS | configured hostname lookup | process, workload, or service |
| Network | loopback TCP exchange | source workload or process |
| Kubernetes | downward-API identity | pod, namespace, ServiceAccount |
| API | marked HTTP transaction | route and originating workload |
| Cloud identity | configured identity endpoint | principal and originating workload |
| MCP | JSON-RPC `tools/call` | tool and calling process/identity |

Each required check receives 45 points for observation and 55 points for correct attribution. Missing ground truth fails the requirement instead of silently shrinking the denominator.

## Telemetry adapters

Vendor payloads are normalized at dedicated endpoints:

```text
POST /api/v1/ingest/falco
POST /api/v1/ingest/tetragon
POST /api/v1/ingest/otlp
POST /api/v1/ingest/cloudtrail
POST /api/v1/ingest/mcp
POST /api/v1/ingest/generic
```

Adapters recursively locate the signed marker, preserve useful attributes, infer the context dimension, evaluate attribution, assign an idempotent event ID, and attach evidence to an active session. See the [adapter contract](docs/adapter-contract.md).

## Kubernetes operator

The `SecurityContextSLO` CRD is reconciled into short-lived canary Jobs:

```yaml
apiVersion: contextslo.io/v1alpha1
kind: SecurityContextSLO
metadata:
  name: orders-api-context
  annotations:
    contextslo.io/interval: 5m
spec:
  service: orders-api
  tier: Tier 1
  target: 99.5
  window: 30d
  latency: {p95: 10s}
  requiredContext: [process, filesystem, network, dns, kubernetes, cloudIdentity, api, mcp]
```

```bash
docker build -t contextslo:local .
kind load docker-image contextslo:local
kubectl apply -k deploy/kubernetes
kubectl -n contextslo-system port-forward service/contextslo 8080:8080
```

The controller uses the in-cluster API directly, creates Jobs with restricted security contexts, writes CRD status, and requires only discovery, Job, pod, and status permissions.

## Independent eBPF truth sensor

The Linux sensor observes process execution, file opens, connects, and UDP sends with CO-RE tracepoints. Its userspace loader uses `cilium/ebpf`, attaches all programs, consumes the ring buffer, associates kernel PIDs with active canary sessions, and submits kernel-verified truth.

Build it on Linux:

```bash
make generate-ebpf
make build-sensor
```

Or use the reproducible container build:

```bash
docker build -f Dockerfile.sensor -t contextslo-sensor:local .
kubectl apply -k deploy/overlays/ebpf
```

The DaemonSet requests `BPF`, `PERFMON`, `SYS_RESOURCE`, and `DAC_READ_SEARCH`; the unprivileged server and operator do not receive those capabilities.

## Configuration

| Environment variable | Purpose | Default |
|---|---|---|
| `CONTEXTSLO_API_TOKEN` | Optional bearer token for every write endpoint | disabled |
| `CONTEXTSLO_CLUSTER` | Logical cluster identity | `production-eu` |
| `CONTEXTSLO_COLLECTION_WAIT` | Live adapter collection window | `2s` |
| `CONTEXTSLO_MAX_COLLECTION_WAIT` | Maximum caller-selected window | `30s` |
| `CONTEXTSLO_DNS_NAME` | Safe DNS target | `localhost` |
| `CONTEXTSLO_CLOUD_PROBE_URL` | Customer-owned identity/canary endpoint | unset |
| `CONTEXTSLO_MCP_PROBE_URL` | MCP JSON-RPC server | unset |
| `CONTEXTSLO_INTEGRATION_TOKEN` | Bearer token sent only to probe endpoints | unset |
| `CONTEXTSLO_SEED_DEMO` | Seed dashboard history for a new store | `true` |

When API authentication is enabled, the dashboard asks for the token on the first protected action and keeps it only in browser local storage.

## API and operations

ContextSLO exposes health (`/healthz`), readiness (`/readyz`), Prometheus metrics (`/metrics`), cluster inventory, sessions, truth ingestion, adapter ingestion, correlation, run history, SLO management, and the embedded dashboard. The complete endpoint and data-flow reference is in [architecture.md](docs/architecture.md).

State is written atomically with file and directory sync, schema-versioned, bounded, and backward-compatible with preview state. For high-availability installations, mount a durable ReadWriteOnce volume and run one writer; horizontally distributed storage is outside the semantics of the included file backend.

## Security and scope

- Canaries perform no exploit, persistence, privilege escalation, or attack technique.
- The application image is static, non-root, capability-free, and read-only.
- Payloads and responses are size-limited; adapter evidence is truncated.
- Write authentication uses constant-time token comparison.
- Raw cloud credentials and complete request bodies are never persisted by the adapter contract.
- eBPF is isolated in an optional Linux DaemonSet with explicit capabilities.

External integrations still require customer-owned endpoints, export rules, and credentials. The repository provides the implementation and configuration boundary; it cannot provision accounts or manufacture real third-party telemetry.

## Development

```bash
make fmt
make vet
make test-race
make build
kubectl kustomize deploy/kubernetes >/dev/null
```

CI additionally builds the application container, compiles the CO-RE object and sensor loader on Linux, and executes a ContextSLO regression gate.

## License

[MIT](LICENSE) © 2026 Riccardo Menegazzo.
