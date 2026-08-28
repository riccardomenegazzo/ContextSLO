# Demo and live validation

## Interview demo

1. Run `docker compose up --build` and open `http://localhost:8080`.
2. Explain why an 80.6% context score is more meaningful than a connected sensor count.
3. Follow the dashed edge from the agent to cloud identity.
4. Run **Healthy baseline** to show a complete deterministic control.
5. Run **Live telemetry validation** to execute real local canaries. Cloud, MCP, or adapter gaps appear as honest failures until configured.

## Live adapter exercise

Create a session, note its marker, and send marked telemetry during the collection window:

```bash
curl -sS -X POST localhost:8080/api/v1/sessions \
  -H 'Content-Type: application/json' \
  -d '{"cluster":"kind","service":"orders-api"}'

curl -sS -X POST localhost:8080/api/v1/ingest/falco \
  -H 'Content-Type: application/json' \
  -d '{
    "rule":"Context canary process",
    "output":"CTX-82FA10 execve observed",
    "output_fields":{"proc.name":"contextslo","proc.pid":42,"k8s.pod.name":"orders-api","k8s.ns.name":"prod"}
  }'

curl -sS -X POST localhost:8080/api/v1/sessions/CTX-82FA10/correlate -d '{}'
```

In Kubernetes, the operator automates allocation, Job execution, collection, and correlation from each `SecurityContextSLO` resource.
