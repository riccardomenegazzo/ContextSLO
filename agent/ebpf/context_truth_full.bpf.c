// SPDX-License-Identifier: MIT
#include "vmlinux.h"
#include <bpf/bpf_core_read.h>
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>

char LICENSE[] SEC("license") = "Dual MIT/GPL";
enum event_type { EVENT_EXEC = 1, EVENT_FILE = 2, EVENT_NETWORK = 3, EVENT_DNS = 4 };
struct truth_event { __u64 timestamp_ns; __u32 pid; __u32 parent_pid; __u32 type; char command[16]; char detail[128]; };
struct { __uint(type, BPF_MAP_TYPE_RINGBUF); __uint(max_entries, 1 << 24); } truth_events SEC(".maps");

static __always_inline struct truth_event *reserve_event(__u32 type) {
    struct truth_event *event = bpf_ringbuf_reserve(&truth_events, sizeof(*event), 0);
    if (!event) return 0;
    __u64 id = bpf_get_current_pid_tgid();
    event->timestamp_ns = bpf_ktime_get_ns(); event->pid = id >> 32; event->type = type;
    event->parent_pid = BPF_CORE_READ((struct task_struct *)bpf_get_current_task(), real_parent, tgid);
    bpf_get_current_comm(event->command, sizeof(event->command));
    return event;
}

SEC("tp/sched/sched_process_exec")
int record_process_exec(struct trace_event_raw_sched_process_exec *ctx) {
    struct truth_event *event = reserve_event(EVENT_EXEC); if (!event) return 0;
    bpf_probe_read_kernel_str(event->detail, sizeof(event->detail), (void *)ctx + ctx->__data_loc_filename);
    bpf_ringbuf_submit(event, 0); return 0;
}

SEC("tp/syscalls/sys_enter_openat")
int record_openat(struct trace_event_raw_sys_enter *ctx) {
    struct truth_event *event = reserve_event(EVENT_FILE); if (!event) return 0;
    bpf_probe_read_user_str(event->detail, sizeof(event->detail), (const void *)ctx->args[1]);
    bpf_ringbuf_submit(event, 0); return 0;
}

SEC("tp/syscalls/sys_enter_connect")
int record_connect(struct trace_event_raw_sys_enter *ctx) {
    struct truth_event *event = reserve_event(EVENT_NETWORK); if (!event) return 0;
    __builtin_memcpy(event->detail, "connect", 8); bpf_ringbuf_submit(event, 0); return 0;
}

SEC("tp/syscalls/sys_enter_sendto")
int record_sendto(struct trace_event_raw_sys_enter *ctx) {
    struct truth_event *event = reserve_event(EVENT_DNS); if (!event) return 0;
    __builtin_memcpy(event->detail, "udp-send", 9); bpf_ringbuf_submit(event, 0); return 0;
}
