package processor

import (
	"context"
	"sync"

	"github.com/chainguard-dev/clog"
)

// Event represents a file access event from the eBPF program.
// This mirrors the ebpf.Event type to avoid circular dependencies.
type Event struct {
	CgroupID  uint64
	PID       uint32
	SyscallNr uint32
	Path      string
}

// Processor handles event processing including path normalization,
// exclusion filtering, and deduplication.
type Processor struct {
	ctx      context.Context
	seen     map[string]struct{}
	seenMu   sync.RWMutex
	excluded []string

	// Metrics
	eventsReceived  uint64
	eventsProcessed uint64
	eventsExcluded  uint64
	eventsDuplicate uint64
	mu              sync.Mutex
}

// NewProcessor creates a new event processor with the given exclusion prefixes.
// If excludePrefixes is nil, DefaultExclusions() will be used.
func NewProcessor(ctx context.Context, excludePrefixes []string) *Processor {
	log := clog.FromContext(ctx)
	if excludePrefixes == nil {
		excludePrefixes = DefaultExclusions()
	}
	log.Infof("Initialized processor with %d exclusion prefixes", len(excludePrefixes))
	for _, prefix := range excludePrefixes {
		log.Debugf("Excluding paths with prefix: %s", prefix)
	}
	return &Processor{
		ctx:      ctx,
		seen:     make(map[string]struct{}),
		excluded: excludePrefixes,
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
)

// Process handles an incoming event, normalizing the path and deduplicating.
// Returns the normalized path and a result indicating what happened.
func (p *Processor) Process(event *Event) (string, ProcessResult) {
	p.mu.Lock()
	p.eventsReceived++
	p.mu.Unlock()

	// Normalize the path
	normalized := NormalizePath(event.Path, event.PID, "")
	if normalized == "" {
		return "", ResultEmpty
	}

	// Check exclusions
	if IsExcluded(normalized, p.excluded) {
		p.mu.Lock()
		p.eventsExcluded++
		p.mu.Unlock()
		return normalized, ResultExcluded
	}

	// Check for duplicates and add if new
	p.seenMu.Lock()
	_, exists := p.seen[normalized]
	if !exists {
		p.seen[normalized] = struct{}{}
	}
	p.seenMu.Unlock()

	if exists {
		p.mu.Lock()
		p.eventsDuplicate++
		p.mu.Unlock()
		return normalized, ResultDuplicate
	}

	p.mu.Lock()
	p.eventsProcessed++
	p.mu.Unlock()
	return normalized, ResultNew
}

// Files returns a snapshot of all unique files seen so far.
// The returned slice is sorted alphabetically.
func (p *Processor) Files() []string {
	p.seenMu.RLock()
	defer p.seenMu.RUnlock()

	files := make([]string, 0, len(p.seen))
	for path := range p.seen {
		files = append(files, path)
	}
	return files
}

// UniqueFileCount returns the number of unique files seen.
func (p *Processor) UniqueFileCount() int {
	p.seenMu.RLock()
	defer p.seenMu.RUnlock()
	return len(p.seen)
}

// Stats returns processing statistics.
type Stats struct {
	EventsReceived  uint64
	EventsProcessed uint64
	EventsExcluded  uint64
	EventsDuplicate uint64
	UniqueFiles     int
}

// Stats returns current processing statistics.
func (p *Processor) Stats() Stats {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.seenMu.RLock()
	uniqueFiles := len(p.seen)
	p.seenMu.RUnlock()

	return Stats{
		EventsReceived:  p.eventsReceived,
		EventsProcessed: p.eventsProcessed,
		EventsExcluded:  p.eventsExcluded,
		EventsDuplicate: p.eventsDuplicate,
		UniqueFiles:     uniqueFiles,
	}
}

// Reset clears all seen files and resets statistics.
// This is primarily useful for testing.
func (p *Processor) Reset() {
	p.seenMu.Lock()
	p.seen = make(map[string]struct{})
	p.seenMu.Unlock()

	p.mu.Lock()
	p.eventsReceived = 0
	p.eventsProcessed = 0
	p.eventsExcluded = 0
	p.eventsDuplicate = 0
	p.mu.Unlock()
}
