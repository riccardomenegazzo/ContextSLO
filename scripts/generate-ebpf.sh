#!/bin/sh
set -eu
output="internal/truthsensor/context_truth_bpfel.o"
temporary="$(mktemp -d)"
trap 'rm -rf "$temporary"' EXIT
bpftool btf dump file /sys/kernel/btf/vmlinux format c > "$temporary/vmlinux.h"
arch="$(uname -m)"
case "$arch" in
  x86_64) target_arch=x86 ;;
  aarch64|arm64) target_arch=arm64 ;;
  *) echo "unsupported architecture: $arch" >&2; exit 1 ;;
esac
clang -g -O2 -target bpf -D__TARGET_ARCH_${target_arch} -I"$temporary" -c agent/ebpf/context_truth_full.bpf.c -o "$output"
llvm-strip -g "$output"
echo "generated $output"
