# Architecture

ContextSLO separates facts from claims. A truth provider records that a safe event occurred; telemetry adapters report what a security or observability platform retained; the correlator compares both views; and the SLO engine turns that comparison into an actionable reliability signal.

```text
Synthetic journey ──► Ground-truth provider ──┐
                                              ├──► Correlator ──► SLI/SLO engine ──► API/UI/CI
Security telemetry ──► Vendor-neutral adapters┘
```

## Current community preview

The runnable preview ships with a deterministic demo truth provider and four scenarios. This makes the full score, API, persistence, dashboard, error-budget, and regression workflow reproducible on any laptop without privileged access.

The repository also contains the CO-RE eBPF tracepoint program that anchors the Linux truth-sensor backend. Its userspace loader, Kubernetes operator reconciliation, and production telemetry adapters are intentionally tracked as the next implementation phase; the preview does not claim that demo observations came from a production CNAPP.

## Scoring contract

Every expected event is scored using two independent questions:

- **Observed (45 points):** did matching telemetry arrive inside the measurement window?
- **Attributed (55 points):** did that telemetry retain the correct originating context?

The run score is the mean of required checks. Context dimensions retain their own score and latency distribution. An SLO breach consumes `100 - score` from the configured context error budget.

## API surface

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/healthz` | Liveness/readiness |
| `GET` | `/api/v1/overview` | Dashboard aggregate |
| `GET` | `/api/v1/runs` | Validation history |
| `POST` | `/api/v1/runs` | Execute a validation scenario |
| `GET` | `/api/v1/runs/{id}` | Retrieve evidence for one run |
| `GET/PUT` | `/api/v1/slo` | Read or update the active SLO |
| `POST` | `/api/v1/ingest` | Accept a generic adapter observation |

Adapter events require a marker, source, dimension, observation state, attribution state, and occurrence time. The endpoint is deliberately vendor-neutral.
