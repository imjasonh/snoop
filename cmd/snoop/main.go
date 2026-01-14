package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/imjasonh/snoop/pkg/cgroup"
	"github.com/imjasonh/snoop/pkg/ebpf"
)

func main() {
	var cgroupPath string
	flag.StringVar(&cgroupPath, "cgroup", "", "Cgroup path to trace (e.g., /system.slice/docker-abc123.scope)")
	flag.Parse()

	if err := run(cgroupPath); err != nil {
		log.Fatalf("Error: %v", err)
	}
}

func run(cgroupPath string) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle signals for graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Println("Received signal, shutting down...")
		cancel()
	}()

	// Create and load the eBPF probe
	log.Println("Loading eBPF program...")
	probe, err := ebpf.NewProbe()
	if err != nil {
		return fmt.Errorf("creating probe: %w", err)
	}
	defer probe.Close()

	log.Println("eBPF program loaded successfully")

	// Add cgroup to trace
	if cgroupPath != "" {
		cgroupID, err := cgroup.GetCgroupIDByPath(cgroupPath)
		if err != nil {
			return fmt.Errorf("getting cgroup ID: %w", err)
		}
		log.Printf("Tracing cgroup: %s (ID: %d)", cgroupPath, cgroupID)
		if err := probe.AddTracedCgroup(cgroupID); err != nil {
			return fmt.Errorf("adding traced cgroup: %w", err)
		}
	} else {
		log.Println("No cgroup specified. Use -cgroup flag to specify a cgroup to trace.")
		log.Println("Example: -cgroup /system.slice/docker-abc123.scope")
		return fmt.Errorf("no cgroup specified")
	}

	// Read and print events
	log.Println("Waiting for events (press Ctrl+C to exit)...")
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		event, err := probe.ReadEvent(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			log.Printf("Error reading event: %v", err)
			continue
		}

		fmt.Printf("[PID %d] [Cgroup %d] [Syscall %d] %s\n",
			event.PID, event.CgroupID, event.SyscallNr, event.Path)
	}
}
