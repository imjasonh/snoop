//go:build linux

package ebpf

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/imjasonh/snoop/pkg/ebpf/bpf"
)

// Event represents a file access event from the eBPF program
type Event struct {
	CgroupID  uint64
	PID       uint32
	SyscallNr uint32
	Path      string
}

// Probe manages the eBPF program lifecycle
type Probe struct {
	objs   *bpf.SnoopObjects
	links  []link.Link
	reader *ringbuf.Reader
}

// NewProbe creates and loads the eBPF program
func NewProbe() (*Probe, error) {
	// Load the eBPF program
	objs := &bpf.SnoopObjects{}
	if err := bpf.LoadSnoopObjects(objs, nil); err != nil {
		return nil, fmt.Errorf("loading eBPF objects: %w", err)
	}

	p := &Probe{
		objs: objs,
	}

	// Attach to tracepoints
	if err := p.attachTracepoints(); err != nil {
		p.Close()
		return nil, fmt.Errorf("attaching tracepoints: %w", err)
	}

	// Create ring buffer reader
	rd, err := ringbuf.NewReader(objs.Events)
	if err != nil {
		p.Close()
		return nil, fmt.Errorf("creating ring buffer reader: %w", err)
	}
	p.reader = rd

	return p, nil
}

// attachTracepoints attaches the eBPF programs to syscall tracepoints
func (p *Probe) attachTracepoints() error {
	// Required tracepoints (must exist on all supported kernels)
	// Attach openat tracepoint
	l, err := link.Tracepoint("syscalls", "sys_enter_openat", p.objs.TraceOpenat, nil)
	if err != nil {
		return fmt.Errorf("attaching openat tracepoint: %w", err)
	}
	p.links = append(p.links, l)

	// Attach execve tracepoint
	l, err = link.Tracepoint("syscalls", "sys_enter_execve", p.objs.TraceExecve, nil)
	if err != nil {
		return fmt.Errorf("attaching execve tracepoint: %w", err)
	}
	p.links = append(p.links, l)

	// Attach newfstatat tracepoint (fstatat/stat)
	l, err = link.Tracepoint("syscalls", "sys_enter_newfstatat", p.objs.TraceNewfstatat, nil)
	if err != nil {
		return fmt.Errorf("attaching newfstatat tracepoint: %w", err)
	}
	p.links = append(p.links, l)

	// Attach faccessat tracepoint (access)
	l, err = link.Tracepoint("syscalls", "sys_enter_faccessat", p.objs.TraceFaccessat, nil)
	if err != nil {
		return fmt.Errorf("attaching faccessat tracepoint: %w", err)
	}
	p.links = append(p.links, l)

	// Attach readlinkat tracepoint (readlink)
	l, err = link.Tracepoint("syscalls", "sys_enter_readlinkat", p.objs.TraceReadlinkat, nil)
	if err != nil {
		return fmt.Errorf("attaching readlinkat tracepoint: %w", err)
	}
	p.links = append(p.links, l)

	// Optional tracepoints (may not exist on older kernels)
	// execveat - exec with dirfd
	if l, err = link.Tracepoint("syscalls", "sys_enter_execveat", p.objs.TraceExecveat, nil); err == nil {
		p.links = append(p.links, l)
	}

	// openat2 - kernel 5.6+
	if l, err = link.Tracepoint("syscalls", "sys_enter_openat2", p.objs.TraceOpenat2, nil); err == nil {
		p.links = append(p.links, l)
	}

	// statx - kernel 4.11+
	if l, err = link.Tracepoint("syscalls", "sys_enter_statx", p.objs.TraceStatx, nil); err == nil {
		p.links = append(p.links, l)
	}

	// faccessat2 - kernel 5.8+
	if l, err = link.Tracepoint("syscalls", "sys_enter_faccessat2", p.objs.TraceFaccessat2, nil); err == nil {
		p.links = append(p.links, l)
	}

	return nil
}

// AddTracedCgroup adds a cgroup ID to the set of traced cgroups
func (p *Probe) AddTracedCgroup(cgroupID uint64) error {
	var dummy uint8 = 1
	return p.objs.TracedCgroups.Put(&cgroupID, &dummy)
}

// RemoveTracedCgroup removes a cgroup ID from the set of traced cgroups
func (p *Probe) RemoveTracedCgroup(cgroupID uint64) error {
	return p.objs.TracedCgroups.Delete(&cgroupID)
}

// ReadEvent reads one event from the ring buffer
func (p *Probe) ReadEvent(ctx context.Context) (*Event, error) {
	record, err := p.reader.Read()
	if err != nil {
		if errors.Is(err, ringbuf.ErrClosed) {
			return nil, err
		}
		return nil, fmt.Errorf("reading from ring buffer: %w", err)
	}

	// Parse the event
	if len(record.RawSample) < 16 {
		return nil, fmt.Errorf("invalid event size: %d", len(record.RawSample))
	}

	event := &Event{
		CgroupID:  binary.LittleEndian.Uint64(record.RawSample[0:8]),
		PID:       binary.LittleEndian.Uint32(record.RawSample[8:12]),
		SyscallNr: binary.LittleEndian.Uint32(record.RawSample[12:16]),
	}

	// Extract the null-terminated path string
	pathBytes := record.RawSample[16:]
	for i, b := range pathBytes {
		if b == 0 {
			event.Path = string(pathBytes[:i])
			break
		}
	}
	if event.Path == "" && len(pathBytes) > 0 {
		event.Path = string(pathBytes)
	}

	return event, nil
}

// Close cleans up all resources
func (p *Probe) Close() error {
	var errs []error

	if p.reader != nil {
		if err := p.reader.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	for _, l := range p.links {
		if err := l.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	if p.objs != nil {
		if err := p.objs.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors closing probe: %v", errs)
	}
	return nil
}
