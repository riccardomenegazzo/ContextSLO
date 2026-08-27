// SPDX-License-Identifier: MIT
// Optional Linux ground-truth backend. The default demo does not require eBPF.
#include "vmlinux.h"
#include <bpf/bpf_core_read.h>
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>

char LICENSE[] SEC("license") = "Dual MIT/GPL";

struct truth_event {
    __u64 timestamp_ns;
    __u32 pid;
    __u32 parent_pid;
    char command[16];
};

struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 1 << 20);
} truth_events SEC(".maps");

SEC("tp/sched/sched_process_exec")
int record_process_exec(struct trace_event_raw_sched_process_exec *ctx)
{
    struct truth_event *event = bpf_ringbuf_reserve(&truth_events, sizeof(*event), 0);
    if (!event)
        return 0;
    __u64 id = bpf_get_current_pid_tgid();
    event->timestamp_ns = bpf_ktime_get_ns();
    event->pid = id >> 32;
    event->parent_pid = BPF_CORE_READ((struct task_struct *)bpf_get_current_task(), real_parent, tgid);
    bpf_get_current_comm(event->command, sizeof(event->command));
    bpf_ringbuf_submit(event, 0);
    return 0;
}
