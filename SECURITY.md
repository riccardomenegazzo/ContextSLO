# Security policy

ContextSLO generates benign synthetic activity and is not an attack simulator. Run privileged truth-sensor components only in isolated test environments until the eBPF backend is declared stable.

Please report vulnerabilities privately through GitHub Security Advisories for this repository. Do not include cloud credentials, customer telemetry, access tokens, or production evidence in public issues.

The dashboard process runs as a non-root user, has a read-only root filesystem in the supplied manifests, drops Linux capabilities, and stores only generated demo evidence by default.
