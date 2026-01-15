package processor

import (
	"context"
	"sort"
	"sync"

	"github.com/chainguard-dev/clog"
)

// ContainerInfo holds information about a discovered container.
// This mirrors cgroup.ContainerInfo to avoid circular dependencies.
type ContainerInfo struct {
	CgroupID   uint64
	CgroupPath string
	Name       string
}

// Event represents a file access event from the eBPF program.
// This mirrors the ebpf.Event type to avoid circular dependencies.
type Event struct {
	CgroupID  uint64
	PID       uint32
	SyscallNr uint32
	Path      string
}

// containerState holds per-container tracking state.
type containerState struct {
	info   *ContainerInfo
	seen   *lruCache
	seenMu sync.RWMutex

	// Per-container metrics
	eventsReceived  uint64
	eventsProcessed uint64
	eventsExcluded  uint64
	eventsDuplicate uint64
	mu              sync.Mutex
}

// Processor handles event processing including path normalization,
// exclusion filtering, and per-container deduplication.
type Processor struct {
	ctx          context.Context
	containers   map[uint64]*containerState
	containersMu sync.RWMutex
	excluded     []string

	// Global metrics for unknown containers
	unknownEvents uint64
	mu            sync.Mutex
}

// NewProcessor creates a new event processor for multiple containers.
// containers maps cgroup IDs to container information.
// If excludePrefixes is nil, DefaultExclusions() will be used.
// maxUniqueFilesPerContainer limits each container's deduplication cache size (0 = unbounded).
func NewProcessor(ctx context.Context, containers map[uint64]*ContainerInfo, excludePrefixes []string, maxUniqueFilesPerContainer int) *Processor {
	log := clog.FromContext(ctx)
	if excludePrefixes == nil {
		excludePrefixes = DefaultExclusions()
	}
	log.Infof("Initialized processor for %d containers with %d exclusion prefixes", len(containers), len(excludePrefixes))
	for _, prefix := range excludePrefixes {
		log.Debugf("Excluding paths with prefix: %s", prefix)
	}
	if maxUniqueFilesPerContainer > 0 {
		log.Infof("Per-container deduplication cache limited to %d unique files", maxUniqueFilesPerContainer)
	} else {
		log.Info("Per-container deduplication cache is unbounded")
	}

	// Initialize per-container state
	containerStates := make(map[uint64]*containerState)
	for cgroupID, info := range containers {
		containerStates[cgroupID] = &containerState{
			info: info,
			seen: newLRUCache(maxUniqueFilesPerContainer),
		}
	}

	return &Processor{
		ctx:        ctx,
		containers: containerStates,
		excluded:   excludePrefixes,
	}
}

// ProcessResult indicates what happened when processing an event.
type ProcessResult int

const (
	// ResultNew indicates a new unique file was recorded.
	ResultNew ProcessResult = iota
	// ResultDuplicate indicates the file was already seen.
	ResultDuplicate
	// ResultExcluded indicates the file was filtered by exclusion rules.
	ResultExcluded
	// ResultEmpty indicates the path was empty after normalization.
	ResultEmpty
	// ResultUnknownContainer indicates the event came from an unknown container.
	ResultUnknownContainer
)

// Process handles an incoming event, normalizing the path and deduplicating per container.
// Returns the container ID, normalized path, and a result indicating what happened.
func (p *Processor) Process(event *Event) (uint64, string, ProcessResult) {
	// Find the container state for this cgroup
	p.containersMu.RLock()
	state, exists := p.containers[event.CgroupID]
	p.containersMu.RUnlock()

	if !exists {
		p.mu.Lock()
		p.unknownEvents++
		p.mu.Unlock()
		clog.FromContext(p.ctx).Warnf("Event from unknown container (cgroup_id=%d)", event.CgroupID)
		return event.CgroupID, "", ResultUnknownContainer
	}

	state.mu.Lock()
	state.eventsReceived++
	state.mu.Unlock()

	// Normalize the path
	normalized := NormalizePath(event.Path, event.PID, "")

	if normalized == "" {
		return event.CgroupID, "", ResultEmpty
	}

	// Check exclusions
	if IsExcluded(normalized, p.excluded) {
		state.mu.Lock()
		state.eventsExcluded++
		state.mu.Unlock()
		return event.CgroupID, normalized, ResultExcluded
	}

	// Check for duplicates and add if new (per-container deduplication)
	state.seenMu.Lock()
	exists = state.seen.add(normalized)
	state.seenMu.Unlock()

	if exists {
		state.mu.Lock()
		state.eventsDuplicate++
		state.mu.Unlock()
		return event.CgroupID, normalized, ResultDuplicate
	}

	state.mu.Lock()
	state.eventsProcessed++
	state.mu.Unlock()
	return event.CgroupID, normalized, ResultNew
}

// Files returns a snapshot of all unique files seen so far, per container.
// Returns a map of cgroup_id -> sorted file list.
func (p *Processor) Files() map[uint64][]string {
	p.containersMu.RLock()
	defer p.containersMu.RUnlock()

	result := make(map[uint64][]string)
	for cgroupID, state := range p.containers {
		state.seenMu.RLock()
		files := state.seen.keys()
		sort.Strings(files)
		state.seenMu.RUnlock()
		result[cgroupID] = files
	}

	return result
}

// ContainerStats returns processing statistics for a specific container.
type ContainerStats struct {
	Name            string
	CgroupID        uint64
	CgroupPath      string
	EventsReceived  uint64
	EventsProcessed uint64
	EventsExcluded  uint64
	EventsDuplicate uint64
	EventsEvicted   uint64
	UniqueFiles     int
}

// Stats returns current processing statistics for all containers.
func (p *Processor) Stats() map[uint64]ContainerStats {
	p.containersMu.RLock()
	defer p.containersMu.RUnlock()

	result := make(map[uint64]ContainerStats)
	for cgroupID, state := range p.containers {
		state.mu.Lock()
		received := state.eventsReceived
		processed := state.eventsProcessed
		excluded := state.eventsExcluded
		duplicate := state.eventsDuplicate
		state.mu.Unlock()

		state.seenMu.RLock()
		uniqueFiles := state.seen.len()
		evicted := state.seen.evictions()
		state.seenMu.RUnlock()

		result[cgroupID] = ContainerStats{
			Name:            state.info.Name,
			CgroupID:        cgroupID,
			CgroupPath:      state.info.CgroupPath,
			EventsReceived:  received,
			EventsProcessed: processed,
			EventsExcluded:  excluded,
			EventsDuplicate: duplicate,
			EventsEvicted:   evicted,
			UniqueFiles:     uniqueFiles,
		}
	}

	return result
}

// AggregateStats returns aggregated statistics across all containers.
type AggregateStats struct {
	EventsReceived  uint64
	EventsProcessed uint64
	EventsExcluded  uint64
	EventsDuplicate uint64
	EventsEvicted   uint64
	UniqueFiles     int
	UnknownEvents   uint64
}

// Aggregate returns aggregated statistics across all containers.
func (p *Processor) Aggregate() AggregateStats {
	p.containersMu.RLock()
	defer p.containersMu.RUnlock()

	var stats AggregateStats

	for _, state := range p.containers {
		state.mu.Lock()
		stats.EventsReceived += state.eventsReceived
		stats.EventsProcessed += state.eventsProcessed
		stats.EventsExcluded += state.eventsExcluded
		stats.EventsDuplicate += state.eventsDuplicate
		state.mu.Unlock()

		state.seenMu.RLock()
		stats.UniqueFiles += state.seen.len()
		stats.EventsEvicted += state.seen.evictions()
		state.seenMu.RUnlock()
	}

	p.mu.Lock()
	stats.UnknownEvents = p.unknownEvents
	p.mu.Unlock()

	return stats
}
