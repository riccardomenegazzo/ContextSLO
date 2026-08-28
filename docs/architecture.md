# Architecture and API

## Runtime sequence

```text
Operator / API
      │ create session + CTX marker
      ▼
Canary runner ───────► process / file / DNS / TCP / HTTP / cloud / MCP
      │                                      │
      │ truth events                         │ external telemetry
      ▼                                      ▼
Durable session ◄──── eBPF sensor      Adapter normalization
      │                                      │
      └──────────── marker + dimension ──────┘
                             │
                             ▼
                    Correlation deadline
                             │
                             ▼
                  SLI / SLO / error budget
```

Truth and observation are distinct types. A failed canary action is a failed truth requirement. A successful action without telemetry is an observation failure. Telemetry without workload, process, or identity causality is an attribution failure.

## API

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/healthz`, `/readyz` | Health and readiness |
| `GET` | `/metrics` | Prometheus text metrics |
| `GET` | `/api/v1/meta` | Auth, mode, and adapter discovery |
| `GET` | `/api/v1/overview` | Dashboard aggregate |
| `GET/PUT` | `/api/v1/slo` | Active Security Context SLO |
| `GET` | `/api/v1/clusters` | Multi-cluster evidence inventory |
| `GET/POST` | `/api/v1/sessions` | List or allocate live sessions |
| `GET` | `/api/v1/sessions/{marker}` | Session truth and observations |
| `POST` | `/api/v1/sessions/{marker}/correlate` | Close and score a session |
| `POST` | `/api/v1/truth` | Canary/eBPF truth envelope |
| `POST` | `/api/v1/ingest` | Normalized observation |
| `POST` | `/api/v1/ingest/{adapter}` | Vendor payload ingestion |
| `GET/POST` | `/api/v1/runs` | History or synchronous validation |
| `GET` | `/api/v1/runs/{id}` | Evidence and score for one run |

Every mutating endpoint requires `Authorization: Bearer <token>` when `CONTEXTSLO_API_TOKEN` is configured.

## Persistence

The versioned state contains the SLO, bounded run history, active and completed sessions, deduplicated observations, and per-cluster counters. Writes use a same-directory temporary file, `fsync`, atomic rename, and directory `fsync`.

## Deployment boundaries

The server, operator, canary, and sensor are separate runtime roles even though the first three share one static binary. Only the sensor needs kernel capabilities. Telemetry adapters terminate at the server HTTP boundary and never run privileged code.
