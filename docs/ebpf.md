# eBPF truth sensor

The optional sensor is built only on Linux because it embeds a kernel-targeted CO-RE object. `scripts/generate-ebpf.sh` derives `vmlinux.h` from the running kernel BTF, compiles process-exec, openat, connect, and sendto tracepoints, and strips debug-only sections.

The userspace loader removes the memlock limit, loads the collection, attaches all tracepoints, consumes the ring buffer, and closes every link on shutdown. It refreshes active sessions from the ContextSLO API, maps observed PIDs to their signed marker, and submits kernel-verified truth evidence.

```bash
make generate-ebpf
make build-sensor
sudo bin/contextslo-sensor --server http://127.0.0.1:8080 --cluster kind
```

In Kubernetes, build `Dockerfile.sensor`, load the image, and apply `deploy/overlays/ebpf`. Kernel capability availability varies by distribution; clusters without separate `BPF` and `PERFMON` capabilities may require a policy-specific privileged exception for this DaemonSet only.
