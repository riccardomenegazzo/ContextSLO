<div align="center">

# ContextSLO

### Your security agent is healthy. But is your security visibility?

**ContextSLO continuously proves whether cloud security telemetry preserves enough runtime context to explain what actually happened.**

Think synthetic monitoring and SLOs—but for security visibility.

[![CI](https://github.com/riccardomenegazzo/ContextSLO/actions/workflows/ci.yml/badge.svg)](https://github.com/riccardomenegazzo/ContextSLO/actions/workflows/ci.yml)
[![Go 1.27](https://img.shields.io/badge/Go-1.27-00ADD8?logo=go)](https://go.dev/)
[![License: MIT](https://img.shields.io/badge/License-MIT-b7f268.svg)](LICENSE)

</div>

---

Security platforms report that sensors are running. ContextSLO asks the harder question: **can those sensors connect a real action to the correct workload, process, identity, API, cloud resource, and business service?**

It generates a safe, marked journey, establishes ground truth, compares that truth with normalized telemetry, and calculates a measurable Security Context SLO.

```text
Customer → frontend → orders-api → aws-sdk → AI agent → MCP → IAM → S3
     ✓          ✓           ✓           ✓          ✕       ?      ✓

Context integrity: 80.6%       Target: 99.5%       SLO: BREACHED
```

## See it working

The repository is a zero-dependency Go application with an embedded interactive dashboard, REST API, CLI, JSON persistence, Kubernetes resources, and a reproducible demo provider.

```bash
docker compose up --build
```

Open [http://localhost:8080](http://localhost:8080), choose a scenario, and select **Run validation**.

No cloud account, Kubernetes cluster, privileged container, npm install, or external service is required for the preview.

## What it proves

For every generated event, ContextSLO separates three facts:

| Question | Meaning |
|---|---|
| **Ground truth** | Did the synthetic action actually occur? |
| **Observed** | Did matching security telemetry arrive in time? |
| **Attributed** | Did it retain the correct originating context? |

The distinction makes blind spots explicit. Seeing an AWS API call is not the same as knowing which pod, process, ServiceAccount, API request, and MCP tool caused it.

The included scenarios demonstrate:

- a fully preserved healthy baseline;
- loss between a Kubernetes workload and its AWS IAM session;
- an MCP tool call with missing caller attribution;
- a telemetry freshness regression.

## Security Context SLO

```yaml
apiVersion: contextslo.io/v1alpha1
kind: SecurityContextSLO
metadata:
  name: orders-api-context
spec:
  service: orders-api
  tier: Tier 1
  target: 99.5
  window: 30d
  latency:
    p95: 10s
  requiredContext:
    - process
    - filesystem
    - network
    - dns
    - kubernetes
    - cloudIdentity
    - api
    - mcp
```

A target of 99.5% creates a 0.5% context error budget. When an infrastructure or telemetry change drops attribution to 92%, ContextSLO reports the regression and marks the budget exhausted. That turns “visibility looks fine” into an objective release gate.

## Architecture

```text
                         independent facts
Safe synthetic probes ─────────────────────────┐
                                              │
                                              ▼
                                      Ground-truth provider
                                              │
                                              ├──────────┐
                                              │          │ compare
                                              │          ▼
Security / OTel / cloud telemetry ─► Adapters ┴──► Correlator
                                                         │
                                                         ▼
                                               SLI + SLO engine
                                                         │
                                      ┌──────────────────┼────────────┐
                                      ▼                  ▼            ▼
                                  Dashboard           REST API     CI exit code
```

The domain is vendor-neutral. Adapters normalize evidence; they do not own scoring policy. See [architecture](docs/architecture.md) and the [generic adapter contract](docs/adapter-contract.md).

## CLI

Build locally with Go 1.27 or use the container:

```bash
go build -o bin/contextslo ./cmd/contextslo

bin/contextslo serve --listen :8080 --data ./data/state.json
bin/contextslo validate --scenario baseline
bin/contextslo validate --scenario identity-gap --json
```

A passing validation exits with code `0`; an SLO breach exits non-zero, making the command suitable for pull-request checks.

## API

```bash
# Current SLO, scores, graph, and history
curl http://localhost:8080/api/v1/overview

# Execute a safe validation
curl -X POST http://localhost:8080/api/v1/runs \
  -H 'Content-Type: application/json' \
  -d '{"scenario":"mcp-gap"}'

# Submit a normalized adapter observation
curl -X POST http://localhost:8080/api/v1/ingest \
  -H 'Content-Type: application/json' \
  -d '{
    "marker":"CTX-82FA10",
    "source":"cloudtrail",
    "dimension":"cloudIdentity",
    "observed":true,
    "attributed":false,
    "occurredAt":"2026-08-27T18:31:22Z"
  }'
```

The full endpoint table is in [docs/architecture.md](docs/architecture.md#api-surface).

## Kubernetes demo

Build the image, load it into your local cluster if necessary, and apply the included Kustomize bundle:

```bash
docker build -t contextslo:local .
kind load docker-image contextslo:local
kubectl apply -k deploy/kubernetes
kubectl -n contextslo-system port-forward service/contextslo 8080:8080
kubectl get securitycontextslo -A
```

The supplied workload runs as non-root, drops every Linux capability, uses a read-only root filesystem, and requires read-only Kubernetes discovery permissions.

## Ground truth and project scope

The community preview is deliberately honest about what is implemented:

| Component | State |
|---|---|
| SLO/error-budget engine | Functional |
| Dashboard, API, CLI, persistence | Functional |
| Reproducible scenario provider | Functional |
| Generic webhook contract | Functional preview |
| Kubernetes CRD and hardened deployment | Functional manifests |
| CO-RE process-exec eBPF program | Source included |
| Privileged eBPF userspace loader | Next milestone |
| Production CNAPP/cloud adapters | Next milestone |
| Kubernetes reconciliation loop | Next milestone |

The default provider uses deterministic synthetic evidence so the entire product workflow can run safely on macOS, Linux, CI, and Kubernetes without kernel privileges. It does **not** present demo records as evidence captured from a production security platform. The optional eBPF source is the foundation for an independent Linux truth backend.

## ContextSLO is not BAS

Breach-and-attack simulation asks whether a control detected or blocked an attack. ContextSLO generates no attack and evaluates a different property:

> An event happened. Was it seen quickly enough, and did the security stack preserve enough context to explain it end to end?

This is synthetic monitoring for the reliability of security context—not another scanner, detection engine, or AI analyst.

## Development

```bash
make test
make vet
make build
make run
```

The backend uses only the Go standard library. Frontend assets are embedded in the binary, so there is no JavaScript build chain or runtime dependency. Read the [five-minute demo](docs/demo.md), [contribution guide](CONTRIBUTING.md), and [security policy](SECURITY.md).

## License

[MIT](LICENSE) © 2026 Riccardo Menegazzo.
