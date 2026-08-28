# Adapter contract

Adapters accept original JSON payloads from Falco, Tetragon, OTLP HTTP exporters, CloudTrail, and MCP audit streams. The normalizer recursively extracts the first `CTX-*` marker and produces this stable envelope:

```json
{
  "id": "cloudtrail:4b4721f0",
  "marker": "CTX-82FA10",
  "cluster": "production-eu",
  "source": "cloudtrail",
  "dimension": "cloudIdentity",
  "occurredAt": "2026-08-27T18:31:22Z",
  "receivedAt": "2026-08-27T18:31:24Z",
  "workload": "orders-api",
  "namespace": "prod",
  "identity": "arn:aws:sts::123456789012:assumed-role/orders",
  "attributed": true,
  "evidence": "cloudtrail · GetCallerIdentity"
}
```

The normalized endpoint `POST /api/v1/ingest` accepts this envelope directly. Vendor endpoints accept a single object, an array, or common record containers such as `Records`, `events`, `resourceSpans`, and `resourceLogs`.

Attribution is dimension-specific: cloud evidence needs both a principal and originating workload/service context; Kubernetes needs pod and namespace; process needs PID or executable; network, DNS, and API need a workload/process/service; MCP needs a caller identity, process, or workload.

Payloads without a marker are rejected rather than stored as uncorrelated data. Event IDs are deterministic when the upstream system does not provide one.
