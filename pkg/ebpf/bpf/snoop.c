//go:build ignore

#include <vmlinux.h>
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>
#include <bpf/bpf_core_read.h>

#define MAX_PATH_LEN 256

// Event structure sent to userspace
struct event {
    u64 cgroup_id;
    u32 pid;
    u32 syscall_nr;
    char path[MAX_PATH_LEN];
};

// Ring buffer for sending events to userspace
struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 256 * 1024);  // 256KB buffer
} events SEC(".maps");

// Per-CPU array for building event data
struct {
    __uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
    __uint(max_entries, 1);
    __type(key, u32);
    __type(value, struct event);
} heap SEC(".maps");

// Hash set of cgroup IDs to trace (populated from userspace)
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 64);
    __type(key, u64);      // cgroup ID
    __type(value, u8);     // dummy value (presence = traced)
} traced_cgroups SEC(".maps");

// Counter for tracking dropped events due to ring buffer overflow
struct {
    __uint(type, BPF_MAP_TYPE_ARRAY);
    __uint(max_entries, 1);
    __type(key, u32);
    __type(value, u64);
} dropped_events SEC(".maps");

// Helper to check if current task's cgroup should be traced
static __always_inline bool should_trace() {
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    u64 cgroup_id = BPF_CORE_READ(task, cgroups, dfl_cgrp, kn, id);
    
    // If no cgroups are configured, don't trace anything
    u8 *val = bpf_map_lookup_elem(&traced_cgroups, &cgroup_id);
    return val != NULL;
}

// Helper to submit event to ring buffer and track drops
static __always_inline void submit_event(struct event *e) {
    int ret = bpf_ringbuf_output(&events, e, sizeof(*e), 0);
    if (ret != 0) {
        // Ring buffer is full, increment drop counter
        u32 key = 0;
        u64 *drop_count = bpf_map_lookup_elem(&dropped_events, &key);
        if (drop_count) {
            __sync_fetch_and_add(drop_count, 1);
        }
    }
}

// Tracepoint for openat syscall
SEC("tracepoint/syscalls/sys_enter_openat")
int trace_openat(struct trace_event_raw_sys_enter *ctx) {
    if (!should_trace()) {
        return 0;
    }
    
    u32 zero = 0;
    struct event *e = bpf_map_lookup_elem(&heap, &zero);
    if (!e) {
        return 0;
    }
    
    // Get cgroup ID
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    e->cgroup_id = BPF_CORE_READ(task, cgroups, dfl_cgrp, kn, id);
    
    // Get PID
    e->pid = bpf_get_current_pid_tgid() >> 32;
    
    // Syscall number
    e->syscall_nr = ctx->id;
    
    // Read pathname argument (second argument for openat)
    const char *pathname = (const char *)ctx->args[1];
    bpf_probe_read_user_str(&e->path, MAX_PATH_LEN, pathname);
    
    // Submit event to ring buffer
    submit_event(e);
    
    return 0;
}

// Tracepoint for execve syscall
// execve(const char *pathname, char *const argv[], char *const envp[])
SEC("tracepoint/syscalls/sys_enter_execve")
int trace_execve(struct trace_event_raw_sys_enter *ctx) {
    if (!should_trace()) {
        return 0;
    }
    
    u32 zero = 0;
    struct event *e = bpf_map_lookup_elem(&heap, &zero);
    if (!e) {
        return 0;
    }
    
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    e->cgroup_id = BPF_CORE_READ(task, cgroups, dfl_cgrp, kn, id);
    e->pid = bpf_get_current_pid_tgid() >> 32;
    e->syscall_nr = ctx->id;
    
    const char *pathname = (const char *)ctx->args[0];
    bpf_probe_read_user_str(&e->path, MAX_PATH_LEN, pathname);
    
    submit_event(e);
    
    return 0;
}

// Tracepoint for execveat syscall
// execveat(int dirfd, const char *pathname, char *const argv[], char *const envp[], int flags)
SEC("tracepoint/syscalls/sys_enter_execveat")
int trace_execveat(struct trace_event_raw_sys_enter *ctx) {
    if (!should_trace()) {
        return 0;
    }
    
    u32 zero = 0;
    struct event *e = bpf_map_lookup_elem(&heap, &zero);
    if (!e) {
        return 0;
    }
    
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    e->cgroup_id = BPF_CORE_READ(task, cgroups, dfl_cgrp, kn, id);
    e->pid = bpf_get_current_pid_tgid() >> 32;
    e->syscall_nr = ctx->id;
    
    const char *pathname = (const char *)ctx->args[1];
    bpf_probe_read_user_str(&e->path, MAX_PATH_LEN, pathname);
    
    submit_event(e);
    
    return 0;
}

// Tracepoint for openat2 syscall (kernel 5.6+)
// openat2(int dirfd, const char *pathname, struct open_how *how, size_t size)
SEC("tracepoint/syscalls/sys_enter_openat2")
int trace_openat2(struct trace_event_raw_sys_enter *ctx) {
    if (!should_trace()) {
        return 0;
    }
    
    u32 zero = 0;
    struct event *e = bpf_map_lookup_elem(&heap, &zero);
    if (!e) {
        return 0;
    }
    
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    e->cgroup_id = BPF_CORE_READ(task, cgroups, dfl_cgrp, kn, id);
    e->pid = bpf_get_current_pid_tgid() >> 32;
    e->syscall_nr = ctx->id;
    
    const char *pathname = (const char *)ctx->args[1];
    bpf_probe_read_user_str(&e->path, MAX_PATH_LEN, pathname);
    
    submit_event(e);
    
    return 0;
}

// Tracepoint for statx syscall (kernel 4.11+)
// statx(int dirfd, const char *pathname, int flags, unsigned int mask, struct statx *statxbuf)
SEC("tracepoint/syscalls/sys_enter_statx")
int trace_statx(struct trace_event_raw_sys_enter *ctx) {
    if (!should_trace()) {
        return 0;
    }
    
    u32 zero = 0;
    struct event *e = bpf_map_lookup_elem(&heap, &zero);
    if (!e) {
        return 0;
    }
    
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    e->cgroup_id = BPF_CORE_READ(task, cgroups, dfl_cgrp, kn, id);
    e->pid = bpf_get_current_pid_tgid() >> 32;
    e->syscall_nr = ctx->id;
    
    const char *pathname = (const char *)ctx->args[1];
    bpf_probe_read_user_str(&e->path, MAX_PATH_LEN, pathname);
    
    submit_event(e);
    
    return 0;
}

// Tracepoint for newfstatat syscall
// newfstatat(int dirfd, const char *pathname, struct stat *statbuf, int flags)
SEC("tracepoint/syscalls/sys_enter_newfstatat")
int trace_newfstatat(struct trace_event_raw_sys_enter *ctx) {
    if (!should_trace()) {
        return 0;
    }
    
    u32 zero = 0;
    struct event *e = bpf_map_lookup_elem(&heap, &zero);
    if (!e) {
        return 0;
    }
    
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    e->cgroup_id = BPF_CORE_READ(task, cgroups, dfl_cgrp, kn, id);
    e->pid = bpf_get_current_pid_tgid() >> 32;
    e->syscall_nr = ctx->id;
    
    const char *pathname = (const char *)ctx->args[1];
    bpf_probe_read_user_str(&e->path, MAX_PATH_LEN, pathname);
    
    submit_event(e);
    
    return 0;
}

// Tracepoint for faccessat syscall
// faccessat(int dirfd, const char *pathname, int mode)
SEC("tracepoint/syscalls/sys_enter_faccessat")
int trace_faccessat(struct trace_event_raw_sys_enter *ctx) {
    if (!should_trace()) {
        return 0;
    }
    
    u32 zero = 0;
    struct event *e = bpf_map_lookup_elem(&heap, &zero);
    if (!e) {
        return 0;
    }
    
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    e->cgroup_id = BPF_CORE_READ(task, cgroups, dfl_cgrp, kn, id);
    e->pid = bpf_get_current_pid_tgid() >> 32;
    e->syscall_nr = ctx->id;
    
    const char *pathname = (const char *)ctx->args[1];
    bpf_probe_read_user_str(&e->path, MAX_PATH_LEN, pathname);
    
    submit_event(e);
    
    return 0;
}

// Tracepoint for faccessat2 syscall (kernel 5.8+)
// faccessat2(int dirfd, const char *pathname, int mode, int flags)
SEC("tracepoint/syscalls/sys_enter_faccessat2")
int trace_faccessat2(struct trace_event_raw_sys_enter *ctx) {
    if (!should_trace()) {
        return 0;
    }
    
    u32 zero = 0;
    struct event *e = bpf_map_lookup_elem(&heap, &zero);
    if (!e) {
        return 0;
    }
    
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    e->cgroup_id = BPF_CORE_READ(task, cgroups, dfl_cgrp, kn, id);
    e->pid = bpf_get_current_pid_tgid() >> 32;
    e->syscall_nr = ctx->id;
    
    const char *pathname = (const char *)ctx->args[1];
    bpf_probe_read_user_str(&e->path, MAX_PATH_LEN, pathname);
    
    submit_event(e);
    
    return 0;
}

// Tracepoint for readlinkat syscall
// readlinkat(int dirfd, const char *pathname, char *buf, size_t bufsiz)
SEC("tracepoint/syscalls/sys_enter_readlinkat")
int trace_readlinkat(struct trace_event_raw_sys_enter *ctx) {
    if (!should_trace()) {
        return 0;
    }
    
    u32 zero = 0;
    struct event *e = bpf_map_lookup_elem(&heap, &zero);
    if (!e) {
        return 0;
    }
    
    struct task_struct *task = (struct task_struct *)bpf_get_current_task();
    e->cgroup_id = BPF_CORE_READ(task, cgroups, dfl_cgrp, kn, id);
    e->pid = bpf_get_current_pid_tgid() >> 32;
    e->syscall_nr = ctx->id;
    
    const char *pathname = (const char *)ctx->args[1];
    bpf_probe_read_user_str(&e->path, MAX_PATH_LEN, pathname);
    
    submit_event(e);
    
    return 0;
}

char __license[] SEC("license") = "GPL";
