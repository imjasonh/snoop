package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/imjasonh/snoop/pkg/cgroup"
	"github.com/imjasonh/snoop/pkg/ebpf"
	"github.com/imjasonh/snoop/pkg/processor"
	"github.com/imjasonh/snoop/pkg/reporter"
)

func main() {
	var (
		cgroupPath     string
		reportPath     string
		reportInterval time.Duration
		excludePaths   string
		imageRef       string
		containerID    string
	)

	flag.StringVar(&cgroupPath, "cgroup", "", "Cgroup path to trace (e.g., /system.slice/docker-abc123.scope)")
	flag.StringVar(&reportPath, "report", "/data/snoop-report.json", "Path to write the JSON report")
	flag.DurationVar(&reportInterval, "interval", 30*time.Second, "Interval between report writes")
	flag.StringVar(&excludePaths, "exclude", "/proc/,/sys/,/dev/", "Comma-separated path prefixes to exclude")
	flag.StringVar(&imageRef, "image", "", "Image reference for report metadata")
	flag.StringVar(&containerID, "container-id", "", "Container ID for report metadata")
	flag.Parse()

	if err := run(cgroupPath, reportPath, reportInterval, excludePaths, imageRef, containerID); err != nil {
		log.Fatalf("Error: %v", err)
	}
}

func run(cgroupPath, reportPath string, reportInterval time.Duration, excludePaths, imageRef, containerID string) error {
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
	if cgroupPath == "" {
		log.Println("No cgroup specified. Use -cgroup flag to specify a cgroup to trace.")
		log.Println("Example: -cgroup /system.slice/docker-abc123.scope")
		return fmt.Errorf("no cgroup specified")
	}

	cgroupID, err := cgroup.GetCgroupIDByPath(cgroupPath)
	if err != nil {
		return fmt.Errorf("getting cgroup ID: %w", err)
	}
	log.Printf("Tracing cgroup: %s (ID: %d)", cgroupPath, cgroupID)
	if err := probe.AddTracedCgroup(cgroupID); err != nil {
		return fmt.Errorf("adding traced cgroup: %w", err)
	}

	// Parse exclusions
	var exclusions []string
	if excludePaths != "" {
		exclusions = strings.Split(excludePaths, ",")
	}

	// Create processor and reporter
	proc := processor.NewProcessor(exclusions)
	rep := reporter.NewFileReporter(reportPath)

	startedAt := time.Now()
	log.Printf("Writing reports to: %s (interval: %s)", reportPath, reportInterval)

	// Start periodic report writer
	reportTicker := time.NewTicker(reportInterval)
	defer reportTicker.Stop()

	writeReport := func() {
		stats := proc.Stats()
		report := &reporter.Report{
			ContainerID:   containerID,
			ImageRef:      imageRef,
			StartedAt:     startedAt,
			Files:         proc.Files(),
			TotalEvents:   stats.EventsReceived,
			DroppedEvents: 0, // TODO: track ring buffer drops
		}
		if err := rep.Update(ctx, report); err != nil {
			log.Printf("Error writing report: %v", err)
		} else {
			log.Printf("Report written: %d unique files, %d events processed",
				stats.UniqueFiles, stats.EventsProcessed)
		}
	}

	// Read and process events
	log.Println("Waiting for events (press Ctrl+C to exit)...")
	for {
		select {
		case <-ctx.Done():
			// Graceful shutdown: write final report
			log.Println("Writing final report...")
			writeReport()
			return nil

		case <-reportTicker.C:
			writeReport()

		default:
			event, err := probe.ReadEvent(ctx)
			if err != nil {
				if ctx.Err() != nil {
					// Context cancelled, write final report
					log.Println("Writing final report...")
					writeReport()
					return nil
				}
				log.Printf("Error reading event: %v", err)
				continue
			}

			// Convert ebpf.Event to processor.Event
			procEvent := &processor.Event{
				CgroupID:  event.CgroupID,
				PID:       event.PID,
				SyscallNr: event.SyscallNr,
				Path:      event.Path,
			}

			path, result := proc.Process(procEvent)
			if result == processor.ResultNew {
				log.Printf("[NEW] %s", path)
			}
		}
	}
}
