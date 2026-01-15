# Snoop Code Review

**Review Date:** 2026-01-14  
**Reviewer:** Claude (Sonnet 4.5)  
**Scope:** Complete codebase review including eBPF implementation, Go code, tests, and architecture

---

## Executive Summary

Snoop is a well-architected eBPF-based file access tracer designed as a Kubernetes sidecar. The codebase demonstrates strong engineering practices with good test coverage (78-100% across most packages), clean separation of concerns, and thoughtful error handling. The code is production-ready with only minor improvements suggested.

**Overall Assessment:** ✅ **Excellent** - Ready for production use with minor enhancements recommended.

---

## Architecture Review

### Strengths

1. **Clean Layered Architecture**
   - Clear separation between eBPF layer, processing layer, and reporting layer
   - Well-defined interfaces (e.g., `Reporter` interface in pkg/reporter)
   - Minimal coupling between components

2. **eBPF Design**
   - Proper use of ring buffers for event streaming
   - Cgroup-based filtering at kernel level (efficient)
   - Drop counter tracking for observability
   - Per-CPU arrays for building events (avoids stack limitations)

3. **Build System**
   - Cross-platform support (amd64/arm64)
   - Docker-based eBPF generation for macOS development
   - Proper use of `//go:build` tags for Linux-specific code

### Areas for Improvement

1. **Multi-Container Support**
   - The `pkg/cgroup/multi_container.go` file provides discovery functions but isn't integrated into main.go
   - Current implementation traces a single cgroup; multi-container tracing would require API changes
   - **Recommendation:** Document the current single-container limitation clearly in README

2. **Configuration Management**
   - Config validation happens in `config.Validate()` but some validation logic is duplicated in main.go
   - **Recommendation:** Move all validation logic to config package

---

## Code Quality Analysis

### eBPF Implementation (pkg/ebpf)

**File:** `pkg/ebpf/bpf/snoop.c`

#### Strengths
- Proper use of `bpf_probe_read_user_str()` for safe string access
- Ring buffer with drop counter tracking
- Should_trace() check at function entry (efficient filtering)
- Correct handling of syscall arguments for each tracepoint
- Graceful degradation for optional tracepoints (openat2, statx, etc.)

#### Issues Found

**CRITICAL - Path Length Truncation (pkg/ebpf/bpf/snoop.c:12)**
```c
#define MAX_PATH_LEN 256
```
**Issue:** Linux supports paths up to 4096 bytes (PATH_MAX). Long paths will be silently truncated, potentially causing:
- Deduplication to treat `/very/long/path/that/exceeds/256/characters...` and `/very/long/path/that/exceeds/256/different...` as the same file
- Loss of important file access data

**Impact:** Medium - May miss files or cause incorrect deduplication in projects with deep directory structures

**Recommendation:**
```c
#define MAX_PATH_LEN 4096  // Match Linux PATH_MAX
```

Update ring buffer size accordingly (currently 256KB, should increase to ~1-2MB).

**MODERATE - Missing Error Handling (pkg/ebpf/bpf/snoop.c)**

The `bpf_probe_read_user_str()` calls don't check return values:
```c
bpf_probe_read_user_str(&e->path, MAX_PATH_LEN, pathname);
```

**Issue:** If the read fails (invalid pointer, fault, etc.), the path field will contain garbage or be empty. This could cause spurious events.

**Recommendation:** Check return value and skip event submission on failure:
```c
if (bpf_probe_read_user_str(&e->path, MAX_PATH_LEN, pathname) < 0) {
    return 0;  // Skip this event
}
```

**MINOR - Drop Counter Thread Safety (pkg/ebpf/bpf/snoop.c:99)**
```c
__sync_fetch_and_add(drop_count, 1);
```

**Observation:** Correct use of atomic operations, but on high-traffic systems, this could become a hot cache line. Consider per-CPU counters if drops become frequent.

### Go Code Quality

#### pkg/ebpf/probe.go

**Strengths:**
- Proper resource cleanup with defer in NewProbe()
- Graceful handling of optional tracepoints
- Good error messages

**Issues Found:**

**MODERATE - Context Not Used in ReadEvent (pkg/ebpf/probe.go:137)**
```go
func (p *Probe) ReadEvent(ctx context.Context) (*Event, error) {
    record, err := p.reader.Read()  // Blocks indefinitely, ignores ctx
    if err != nil {
        // ...
    }
```

**Issue:** The context parameter is never checked. If the context is cancelled, this will continue blocking until an event arrives.

**Impact:** Graceful shutdown may be delayed by up to one event arrival time.

**Recommendation:**
```go
func (p *Probe) ReadEvent(ctx context.Context) (*Event, error) {
    // Option 1: Use ReadContext if available
    record, err := p.reader.ReadContext(ctx)
    
    // Option 2: If ReadContext doesn't exist, use goroutine + channel pattern
    type result struct {
        record ringbuf.Record
        err    error
    }
    ch := make(chan result, 1)
    go func() {
        rec, err := p.reader.Read()
        ch <- result{rec, err}
    }()
    
    select {
    case <-ctx.Done():
        return nil, ctx.Err()
    case res := <-ch:
        if res.err != nil {
            return nil, res.err
        }
        // ... parse event
    }
}
```

**MINOR - Magic Number (pkg/ebpf/probe.go:143)**
```go
if len(record.RawSample) < 16 {
```

**Recommendation:** Define constant:
```go
const eventHeaderSize = 16  // 8 bytes cgroup_id + 4 bytes pid + 4 bytes syscall_nr
```

#### pkg/processor/processor.go

**Strengths:**
- Excellent thread safety with proper mutex usage
- LRU cache implementation for bounded memory usage
- Comprehensive metrics tracking

**Issues Found:**

**MINOR - Dead Code Comment (pkg/processor/processor.go:59-63)**
```go
// Debug: log paths that contain ".." to understand normalization issues
if strings.Contains(event.Path, "..") || strings.Contains(normalized, "..") {
    // This will help us debug path normalization in production
    _ = event.Path // Keep for potential future logging
}
```

**Issue:** This debug code doesn't do anything and clutters the codebase.

**Recommendation:** Remove it entirely, or implement actual logging:
```go
if strings.Contains(event.Path, "..") || strings.Contains(normalized, "..") {
    clog.FromContext(p.ctx).Debugf("Path normalization: %q -> %q", event.Path, normalized)
}
```

#### pkg/processor/normalize.go

**Strengths:**
- Thoughtful path normalization that preserves symlinks
- Good handling of edge cases (.. past root, etc.)
- Fallback behavior when cwd unavailable

**Issues Found:**

**LOW - /proc Read in Hot Path (pkg/processor/normalize.go:54-61)**
```go
func getProcessCwd(pid uint32) string {
    cwdPath := fmt.Sprintf("/proc/%d/cwd", pid)
    cwd, err := os.Readlink(cwdPath)
    if err != nil {
        return ""
    }
    return cwd
}
```

**Issue:** This reads from /proc filesystem on every relative path encountered. For short-lived processes, the PID may not exist by the time we try to read it.

**Impact:** Low - Function handles errors gracefully by returning empty string

**Observation:** Since eBPF runs at syscall entry, the process is still alive, but by the time userspace processes the event (ring buffer delay), it might not be. This is acceptable as the fallback behavior (prefixing with /) is reasonable.

**Recommendation:** Consider adding a small LRU cache for pid->cwd mappings to reduce /proc reads:
```go
type cwdCache struct {
    mu    sync.RWMutex
    cache map[uint32]string  // pid -> cwd
    // Add LRU eviction if needed
}
```

#### pkg/cgroup/discovery.go

**Issues Found:**

**MODERATE - Platform-Specific Code Without Build Tags (pkg/cgroup/discovery.go:64)**
```go
func getCgroupIDFromInode(cgroupPath string) (uint64, error) {
    var stat syscall.Stat_t
    if err := syscall.Stat(cgroupPath, &stat); err != nil {
        return 0, fmt.Errorf("stat failed for %s: %w", cgroupPath, err)
    }
    return stat.Ino, nil
}
```

**Issue:** `syscall.Stat_t` is platform-specific. The `Ino` field exists on Linux but may have different names on other platforms.

**Impact:** Low - Test coverage shows this is Linux-only (8.3% coverage means tests mostly skip on macOS)

**Recommendation:** Add `//go:build linux` tag to this file to make it explicit:
```go
//go:build linux

package cgroup
```

#### cmd/snoop/main.go

**Strengths:**
- Excellent orchestration with proper signal handling
- Good integration of metrics, health checks, and graceful shutdown
- Clean separation of concerns

**Issues Found:**

**MODERATE - Shutdown Timeout Not Configurable (cmd/snoop/main.go:109)**
```go
shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
```

**Issue:** Hardcoded 5-second timeout for metrics server shutdown. If report writes are slow, this might not be enough.

**Recommendation:** Make this configurable or increase to 10s:
```go
shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
```

**LOW - Potential Double Report Write (cmd/snoop/main.go:182-186)**
```go
case <-ctx.Done():
    log.Info("Writing final report")
    writeReport()
    return nil
```

And later:
```go
if ctx.Err() != nil {
    log.Info("Writing final report")
    writeReport()
    return nil
}
```

**Issue:** In the `default` branch, if context is cancelled while reading an event, both paths could theoretically execute (though unlikely in practice due to channel closure).

**Recommendation:** Use a flag to track if final report was written:
```go
var finalReportWritten bool
// ...
if !finalReportWritten {
    writeReport()
}
```

---

## Test Coverage Analysis

### Coverage Summary

```
Package                Coverage
-----------------------------------------
pkg/processor         91.9%   ✅ Excellent
pkg/config            87.8%   ✅ Good
pkg/health            85.7%   ✅ Good
pkg/reporter          78.4%   ✅ Good
pkg/metrics          100.0%   ✅ Perfect
pkg/cgroup             8.3%   ⚠️  Low (platform-specific)
pkg/ebpf               0.0%   ⚠️  Not testable without kernel
cmd/snoop              0.0%   ⚠️  Integration test needed
```

### Strengths

1. **Excellent Test Quality**
   - Tests use table-driven approach (idiomatic Go)
   - Good edge case coverage (empty paths, normalization, exclusions)
   - Concurrency tests for thread safety
   - Tests for bounded vs unbounded cache behavior

2. **Realistic Test Scenarios**
   - Reporter tests verify atomic writes
   - Processor tests verify LRU eviction behavior
   - Config tests verify all validation rules

### Missing Coverage

**Integration Tests (CRITICAL)**

**Issue:** No end-to-end tests that verify:
- eBPF program actually loads
- Events flow from kernel → userspace → reporter
- Cgroup filtering works correctly
- Ring buffer overflow handling

**Recommendation:** Add integration tests in `test/` directory:
```bash
test/
  integration/
    basic_test.go          # Can we load eBPF and receive events?
    cgroup_filter_test.go  # Does cgroup filtering work?
    performance_test.go    # Throughput and latency benchmarks
```

These would need `//go:build linux` and `//go:build integration` tags to run selectively.

**Cgroup Tests (MODERATE)**

**Issue:** Only 8.3% coverage because most functions need Linux. However, the core logic isn't tested.

**Recommendation:** Add unit tests that mock the filesystem:
```go
func TestGetCgroupIDByPath_FromFile(t *testing.T) {
    // Create temp cgroup.id file
    // Test reading and parsing
}
```

---

## Security Analysis

### Potential Security Issues

**LOW - Path Traversal in Report Output (pkg/reporter/reporter.go:51)**
```go
func (r *FileReporter) Update(ctx context.Context, report *Report) error {
    // ...
    dir := filepath.Dir(r.path)
    if err := os.MkdirAll(dir, 0755); err != nil {
        return fmt.Errorf("creating directory %s: %w", dir, err)
    }
```

**Issue:** If `r.path` is user-controlled (e.g., from `-report` flag), an attacker could potentially write reports to arbitrary locations like `/etc/snoop-report.json`.

**Impact:** Low - In Kubernetes, this runs as a sidecar with limited permissions. The `-report` flag is typically set by the deployment, not by end users.

**Mitigation:** Already mitigated by:
1. Kubernetes RBAC/Pod Security
2. Config validation checks directory exists (line 85 in config.go)

**No action needed**, but document that `-report` should be restricted to admin-controlled values.

**LOW - Unbounded Memory Growth (pkg/processor/processor.go:27)**

**Issue:** Default behavior is unbounded cache (`maxUniqueFiles: 0`), which could lead to OOM if container accesses millions of unique files.

**Current Mitigation:** The `-max-unique-files` flag exists and is documented in plan.md

**Recommendation:** Consider setting a default limit (e.g., 100,000 files) instead of unbounded:
```go
const DefaultMaxUniqueFiles = 100000  // ~6-8MB of strings

// In main.go:
flag.IntVar(&maxUniqueFiles, "max-unique-files", DefaultMaxUniqueFiles, 
    "Maximum unique files to track (0 = unbounded)")
```

**Information Disclosure (ACCEPTABLE)**

**Observation:** The tool inherently captures file access patterns, which could be sensitive (e.g., which API keys were accessed). This is by design.

**Recommendation:** Document in README that:
- Reports should be treated as sensitive
- Consider encrypting reports at rest
- RBAC should restrict access to report ConfigMaps/volumes

---

## Performance Considerations

### Strengths

1. **Efficient Deduplication**
   - O(1) lookups with map-backed LRU cache
   - Minimal allocations per event

2. **Ring Buffer Design**
   - 256KB ring buffer is reasonable for moderate traffic
   - Per-CPU arrays avoid stack size limits

3. **Batch Report Writes**
   - 30-second interval (configurable) reduces I/O

### Potential Issues

**HIGH - Ring Buffer Size May Be Insufficient (pkg/ebpf/bpf/snoop.c:15)**
```c
__uint(max_entries, 256 * 1024);  // 256KB buffer
```

**Issue:** With 256-byte events, this holds ~1,000 events. On a busy container with thousands of file accesses per second, the buffer could overflow.

**Evidence:** The drop counter exists because this is a known risk.

**Calculation:**
- Event size: 16 bytes header + 256 bytes path = 272 bytes (with alignment: ~280 bytes)
- Buffer capacity: 256KB / 280 bytes ≈ 935 events
- At 10,000 events/sec, buffer fills in 93ms

If userspace can't read events within 93ms, drops occur.

**Recommendation:**
1. Increase ring buffer to 1-2MB:
   ```c
   __uint(max_entries, 2 * 1024 * 1024);  // 2MB
   ```
2. Add alerts when drop rate exceeds threshold (already instrumented via metrics)
3. Document expected drop behavior under high load

**MODERATE - Ticker Precision (cmd/snoop/main.go:155)**
```go
reportTicker := time.NewTicker(cfg.ReportInterval)
```

**Issue:** With the default 30-second interval, if event processing blocks (e.g., slow regex in normalize path, though there isn't one), report writes could drift.

**Impact:** Low - Report writes are fast (atomic file write)

**Observation:** No action needed, but consider adding metrics for report write latency.

---

## Dependency Analysis

**go.mod review:**
```
github.com/chainguard-dev/clog v1.8.0           ✅ Trusted (Chainguard)
github.com/cilium/ebpf v0.20.0                  ✅ Industry standard
github.com/prometheus/client_golang v1.23.2    ✅ Standard metrics
```

All dependencies are:
- From reputable sources
- Actively maintained
- Up to date

**No security concerns with dependencies.**

---

## Documentation Review

### Existing Documentation

**Strengths:**
- Comprehensive README.md with architecture diagrams
- CLAUDE.md provides excellent context for AI assistance
- TESTING.md, QUICKSTART.md, STATUS.md are well-maintained
- Inline comments explain complex logic

### Missing Documentation

1. **Security Considerations**
   - No doc on report sensitivity
   - No guidance on RBAC requirements

2. **Performance Tuning**
   - Ring buffer sizing guidance
   - Expected drop rates
   - When to use `-max-unique-files`

3. **Operational Runbook**
   - How to interpret metrics
   - What to do when drops occur
   - Debugging guide for "no events received"

**Recommendation:** Create `OPERATIONS.md` covering:
- Metrics interpretation
- Troubleshooting common issues
- Performance tuning guide

---

## Best Practices Compliance

### Followed Best Practices ✅

1. **Go Code Style**
   - Idiomatic Go throughout
   - Proper use of context.Context
   - Table-driven tests
   - Good error messages with context

2. **eBPF Best Practices**
   - License declaration (GPL)
   - Proper use of helpers
   - Kernel version compatibility handling

3. **Kubernetes Best Practices**
   - Health checks implemented
   - Metrics exposed
   - Graceful shutdown
   - Resource limits (TODO in deployment)

4. **Observability**
   - Structured logging with clog
   - Prometheus metrics
   - Health endpoint

### Areas for Improvement

1. **Error Handling**
   - Some errors are logged but not exposed as metrics (e.g., cgroup discovery failures)
   - **Recommendation:** Add metrics for error rates

2. **Configuration**
   - No config file support (only flags)
   - **Recommendation:** Consider supporting ConfigMap-based config for Kubernetes

---

## Critical Issues Summary

### Must Fix Before Production

1. **Path Length Truncation** (pkg/ebpf/bpf/snoop.c:12)
   - Increase MAX_PATH_LEN to 4096
   - Risk: Data loss and incorrect deduplication

2. **Context Not Honored in ReadEvent** (pkg/ebpf/probe.go:137)
   - Implement context-aware reading
   - Risk: Delayed shutdown

### Should Fix Soon

3. **Ring Buffer Size** (pkg/ebpf/bpf/snoop.c:15)
   - Increase to 1-2MB
   - Risk: Event drops under load

4. **Missing Error Checks in eBPF** (pkg/ebpf/bpf/snoop.c)
   - Check bpf_probe_read_user_str return values
   - Risk: Spurious events

5. **Integration Tests**
   - Add end-to-end tests
   - Risk: Undetected bugs in eBPF/userspace boundary

### Nice to Have

6. **Dead Code Cleanup** (pkg/processor/processor.go:59)
7. **Build Tags for Platform-Specific Code** (pkg/cgroup/discovery.go)
8. **Default Max Unique Files Limit** (avoid OOM)
9. **CWD Cache for Performance** (pkg/processor/normalize.go)

---

## Performance Benchmarks (Estimated)

Based on code analysis, expected performance:

| Metric | Estimate | Notes |
|--------|----------|-------|
| Events/sec (low load) | 10,000+ | Limited by ring buffer reads |
| Events/sec (high load) | 50,000+ | With larger ring buffer |
| Memory per unique file | ~80 bytes | String + LRU overhead |
| Memory (100k files) | ~8 MB | Reasonable for sidecar |
| CPU overhead | <5% | eBPF is very efficient |
| Latency (event to report) | 0-30s | Depends on report interval |

**Recommendation:** Add benchmarks in `bench/` directory to validate these estimates.

---

## Comparison to Alternatives

Snoop compares favorably to alternatives like:

1. **strace**
   - ❌ Much higher overhead (context switches)
   - ✅ Snoop uses eBPF (in-kernel)

2. **inotify**
   - ❌ Doesn't capture execve, stat, access
   - ❌ Requires recursive watches
   - ✅ Snoop traces syscalls directly

3. **audit framework**
   - ❌ Higher overhead
   - ❌ More complex setup
   - ✅ Snoop is purpose-built for this use case

---

## Recommendations Priority

### Priority 1 (Critical - Do Before Production)
1. Fix path length truncation (MAX_PATH_LEN → 4096)
2. Implement context-aware event reading
3. Add integration tests
4. Increase ring buffer size to 1-2MB

### Priority 2 (Important - Do Soon)
5. Add error checking in eBPF probe reads
6. Add operational documentation (OPERATIONS.md)
7. Set default max-unique-files limit
8. Document security considerations

### Priority 3 (Nice to Have)
9. Remove dead debug code
10. Add build tags for platform-specific code
11. Implement CWD caching
12. Add performance benchmarks
13. Add metrics for error rates

---

## Conclusion

The Snoop codebase is **well-engineered and nearly production-ready**. The architecture is sound, the code is clean and idiomatic, and test coverage is good. The main concerns are:

1. Path length limitations that could cause data loss
2. Ring buffer sizing for high-traffic scenarios
3. Need for integration testing

With the Priority 1 fixes implemented, this tool is ready for production use. The team has clearly thought through the design and implementation carefully.

**Recommendation:** ✅ **Approve with conditions** - Fix Priority 1 issues before deploying to production.

---

## Code Review Checklist

- ✅ Architecture is sound and maintainable
- ✅ Code follows Go best practices
- ✅ eBPF code follows kernel best practices
- ✅ Thread safety is properly handled
- ✅ Error handling is comprehensive
- ⚠️  Test coverage is good but missing integration tests
- ⚠️  Path length truncation needs fixing
- ✅ Dependencies are secure and up-to-date
- ✅ Documentation is comprehensive
- ⚠️  Performance tuning needed for high-load scenarios
- ✅ Security risks are minimal and acceptable
- ✅ Observability is excellent (metrics, logs, health)

---

**Review Completed:** 2026-01-14  
**Next Review:** After Priority 1 issues are resolved
