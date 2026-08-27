# Generic adapter contract

Telemetry adapters translate vendor-specific records into a minimal observation envelope. They never decide the score.

```json
{
  "marker": "CTX-82FA10",
  "source": "cloudtrail",
  "dimension": "cloudIdentity",
  "observed": true,
  "attributed": false,
  "occurredAt": "2026-08-27T18:31:22Z"
}
```

Send the envelope to `POST /api/v1/ingest`. A production adapter should preserve the raw evidence pointer, use an idempotency key derived from source and event ID, and never send credentials or full request bodies. Supported dimensions are `process`, `filesystem`, `network`, `dns`, `kubernetes`, `cloudIdentity`, `api`, and `mcp`.
