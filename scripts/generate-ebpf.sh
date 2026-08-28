#!/bin/sh
set -eu
output="internal/truthsensor/context_truth_bpfel.o"
temporary="$(mktemp -d)"
trap 'rm -rf "$temporary"' EXIT
if command -v bpftool >/dev/null 2>&1 && bpftool version >/dev/null 2>&1; then
  bpftool btf dump file /sys/kernel/btf/vmlinux format c > "$temporary/vmlinux.h"
else
  cp agent/ebpf/vmlinux_min.h "$temporary/vmlinux.h"
fi
arch="$(uname -m)"
case "$arch" in
  x86_64) target_arch=x86 ;;
  aarch64|arm64) target_arch=arm64 ;;
  *) echo "unsupported architecture: $arch" >&2; exit 1 ;;
esac
clang -g -O2 -target bpf -D__TARGET_ARCH_${target_arch} -I"$temporary" -c agent/ebpf/context_truth_full.bpf.c -o "$output"
llvm-strip -g "$output"
echo "generated $output"
