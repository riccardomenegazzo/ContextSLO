# Five-minute demo

1. Start ContextSLO with `docker compose up --build`.
2. Open `http://localhost:8080` and point out that the sensor being connected is not the outcome—the 80.6% context integrity score is.
3. Follow the dashed graph edge from `order-agent` to `orders-role`. The cloud action exists, but workload causality was lost.
4. Run **Healthy baseline**. All eight dimensions become fully attributed and the graph heals.
5. Run **MCP attribution gap**. The tool call remains visible but loses its calling process, demonstrating why event counts alone are insufficient.
6. Open **Security SLO**, change the target, and export the audit report.

The CLI expresses the same gate for CI:

```bash
contextslo validate --scenario baseline
contextslo validate --scenario identity-gap # returns non-zero
```

The failing exit status is the primitive a pull-request check can use to block a security-observability regression.
