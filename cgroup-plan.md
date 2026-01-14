# Cgroup Auto-Discovery Implementation Plan

## Goal

Eliminate the need for users to manually specify cgroup paths or use initContainers in Kubernetes deployments. Snoop will automatically discover its own cgroup path at startup.

## Current State

- Users must provide `-cgroup=/sys/fs/cgroup/path/to/cgroup` flag
- Kubernetes deployments require an initContainer to discover the cgroup path and write it to a shared volume
- The snoop sidecar then reads this file via shell substitution: `-cgroup=/sys/fs/cgroup$(cat /data/cgroup-path)`

## Proposed State

- The `-cgroup` flag becomes optional
- When omitted, snoop automatically discovers its own cgroup path from `/proc/self/cgroup`
- No initContainer needed in Kubernetes deployments
- Simpler user experience: just add the sidecar container

## Implementation Tasks

### Milestone 1: Core Auto-Discovery

- [ ] Add `GetSelfCgroupPath()` function to `pkg/cgroup/discovery.go`
  - Read `/proc/self/cgroup` to find cgroup v2 path
  - Parse the `0::/path` format
  - Return the full `/sys/fs/cgroup/path` to use for ID lookup
  
- [ ] Make `-cgroup` flag optional in `cmd/snoop/main.go`
  - Change flag default from `""` to empty
  - Remove validation error for empty cgroup path
  
- [ ] Update `config.Config` validation in `pkg/config/config.go`
  - Remove requirement that `CgroupPath` be non-empty
  - Add logic to auto-discover if empty
  
- [ ] Wire auto-discovery into main startup flow
  - If `cfg.CgroupPath == ""`, call `cgroup.GetSelfCgroupPath()`
  - Use discovered path for `GetCgroupIDByPath()` call
  - Log the discovered cgroup path

### Milestone 2: Testing

- [ ] Add unit tests for `GetSelfCgroupPath()`
  - Test parsing valid cgroup v2 format
  - Test error handling for missing/malformed files
  - Test that discovered path works with `GetCgroupIDByPath()`
  
- [ ] Update integration tests
  - Test that snoop works without `-cgroup` flag
  - Verify auto-discovery logs appear

### Milestone 3: Documentation Updates

- [ ] Update `deploy/kubernetes/README.md`
  - Remove initContainer from examples
  - Simplify deployment instructions
  - Remove shell substitution from args
  - Show before/after comparison
  
- [ ] Update `deploy/kubernetes/deployment.yaml`
  - Remove `cgroup-finder` initContainer
  - Remove `cgroup-path` file handling
  - Simplify snoop container args to not reference `-cgroup` at all
  
- [ ] Update `deploy/kubernetes/example-app.yaml`
  - Same changes as deployment.yaml
  
- [ ] Update main `README.md`
  - Note that `-cgroup` flag is optional
  - Mention auto-discovery feature
  
- [ ] Update `CLAUDE.md` architecture notes
  - Document auto-discovery behavior

## Technical Notes

### Cgroup v2 Format

The `/proc/self/cgroup` file for cgroup v2 has this format:
```
0::/path/to/cgroup
```

For example, in a Kubernetes pod:
```
0::/kubepods/burstable/pod<uid>/<container-id>
```

The path after `::` is relative to `/sys/fs/cgroup/`.

### Backward Compatibility

The `-cgroup` flag will remain available for users who want to:
- Trace a different cgroup than their own
- Override auto-discovery for testing
- Use explicit paths in non-standard environments

## Success Criteria

- [ ] Snoop starts successfully without `-cgroup` flag
- [ ] Auto-discovered cgroup path matches what initContainer previously found
- [ ] Kubernetes deployment works without initContainer
- [ ] All tests pass
- [ ] Documentation updated and clear

## Risks and Mitigations

**Risk**: Users on cgroup v1 systems won't have `0::` format  
**Mitigation**: Auto-discovery will fail with clear error; users can still use `-cgroup` flag manually

**Risk**: Some container runtimes might have non-standard cgroup layouts  
**Mitigation**: Keep `-cgroup` flag as fallback; log discovered path clearly for debugging

**Risk**: Breaking change for existing users who parse our CLI  
**Mitigation**: This is additive only - existing usage with `-cgroup` flag continues to work
