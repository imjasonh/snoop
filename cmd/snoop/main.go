//go:build linux

package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/imjasonh/snoop/pkg/cgroup"
	"github.com/imjasonh/snoop/pkg/ebpf"
	"github.com/imjasonh/snoop/pkg/metrics"
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
		metricsAddr    string
		logLevel       string
	)

	flag.StringVar(&cgroupPath, "cgroup", "", "Cgroup path to trace (e.g., /system.slice/docker-abc123.scope)")
	flag.StringVar(&reportPath, "report", "/data/snoop-report.json", "Path to write the JSON report")
	flag.DurationVar(&reportInterval, "interval", 30*time.Second, "Interval between report writes")
	flag.StringVar(&excludePaths, "exclude", "/proc/,/sys/,/dev/", "Comma-separated path prefixes to exclude")
	flag.StringVar(&imageRef, "image", "", "Image reference for report metadata")
	flag.StringVar(&containerID, "container-id", "", "Container ID for report metadata")
	flag.StringVar(&metricsAddr, "metrics-addr", ":9090", "Address for Prometheus metrics endpoint (empty to disable)")
	flag.StringVar(&logLevel, "log-level", "info", "Log level (debug, info, warn, error)")
	flag.Parse()

	// Initialize logging context
	ctx := clog.WithLogger(context.Background(), clog.New(clog.ParseLevel(logLevel)))

	if err := run(ctx, cgroupPath, reportPath, reportInterval, excludePaths, imageRef, containerID, metricsAddr); err != nil {
		clog.FromContext(ctx).Fatalf("Fatal error: %v", err)
	}
}

func run(ctx context.Context, cgroupPath, reportPath string, reportInterval time.Duration, excludePaths, imageRef, containerID, metricsAddr string) error {
	log := clog.FromContext(ctx)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Handle signals for graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Info("Received shutdown signal")
		cancel()
	}()

	// Initialize metrics
	m := metrics.New()

	// Start metrics server if address is provided
	if metricsAddr != "" {
		mux := http.NewServeMux()
		mux.Handle("/metrics", m.Handler())
		server := &http.Server{
			Addr:    metricsAddr,
			Handler: mux,
		}
		go func() {
			log.Infof("Starting metrics server on %s", metricsAddr)
			if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Errorf("Metrics server error: %v", err)
			}
		}()
		defer func() {
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer shutdownCancel()
			server.Shutdown(shutdownCtx)
		}()
	}

	// Create and load the eBPF probe
	log.Info("Loading eBPF program")
	probe, err := ebpf.NewProbe(ctx)
	if err != nil {
		return fmt.Errorf("creating probe: %w", err)
	}
	defer probe.Close()

	log.Info("eBPF program loaded successfully")

	// Add cgroup to trace
	if cgroupPath == "" {
		log.Error("No cgroup specified. Use -cgroup flag to specify a cgroup to trace.")
		log.Error("Example: -cgroup /system.slice/docker-abc123.scope")
		return fmt.Errorf("no cgroup specified")
	}

	cgroupID, err := cgroup.GetCgroupIDByPath(cgroupPath)
	if err != nil {
		return fmt.Errorf("getting cgroup ID: %w", err)
	}
	log.Infof("Tracing cgroup: %s (ID: %d)", cgroupPath, cgroupID)
	if err := probe.AddTracedCgroup(cgroupID); err != nil {
		return fmt.Errorf("adding traced cgroup: %w", err)
	}

	// Parse exclusions
	var exclusions []string
	if excludePaths != "" {
		exclusions = strings.Split(excludePaths, ",")
	}

	// Create processor and reporter
	proc := processor.NewProcessor(ctx, exclusions)
	rep := reporter.NewFileReporter(ctx, reportPath)

	startedAt := time.Now()
	log.Infof("Writing reports to: %s (interval: %s)", reportPath, reportInterval)

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
			log.Errorf("Error writing report: %v", err)
			m.ReportWriteErrors.Inc()
		} else {
			log.Infof("Report written: %d unique files, %d events processed",
				stats.UniqueFiles, stats.EventsProcessed)
			m.ReportWrites.Inc()
		}
		// Update gauge for unique files count
		m.UniqueFiles.Set(float64(stats.UniqueFiles))
	}

	// Read and process events
	log.Info("Waiting for events (press Ctrl+C to exit)")
	for {
		select {
		case <-ctx.Done():
			// Graceful shutdown: write final report
			log.Info("Writing final report")
			writeReport()
			return nil

		case <-reportTicker.C:
			writeReport()

		default:
			event, err := probe.ReadEvent(ctx)
			if err != nil {
				if ctx.Err() != nil {
					// Context cancelled, write final report
					log.Info("Writing final report")
					writeReport()
					return nil
				}
				log.Errorf("Error reading event: %v", err)
				continue
			}

			// Convert ebpf.Event to processor.Event
			procEvent := &processor.Event{
				CgroupID:  event.CgroupID,
				PID:       event.PID,
				SyscallNr: event.SyscallNr,
				Path:      event.Path,
			}

			// Update received counter
			m.EventsReceived.Inc()

			path, result := proc.Process(procEvent)
			switch result {
			case processor.ResultNew:
				m.EventsProcessed.Inc()
				log.Debugf("New file: %s", path)
			case processor.ResultDuplicate:
				m.EventsDuplicate.Inc()
			case processor.ResultExcluded:
				m.EventsExcluded.Inc()
			}
		}
	}
}
