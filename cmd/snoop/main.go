//go:build linux

package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/chainguard-dev/clog"
	"github.com/chainguard-dev/clog/slag"
	"github.com/imjasonh/snoop/pkg/apk"
	"github.com/imjasonh/snoop/pkg/cgroup"
	"github.com/imjasonh/snoop/pkg/config"
	"github.com/imjasonh/snoop/pkg/ebpf"
	"github.com/imjasonh/snoop/pkg/health"
	"github.com/imjasonh/snoop/pkg/metrics"
	"github.com/imjasonh/snoop/pkg/processor"
	"github.com/imjasonh/snoop/pkg/reporter"
)

func main() {
	var (
		reportPath     string
		reportInterval time.Duration
		excludePaths   string
		imageRef       string
		imageDigest    string
		containerID    string
		podName        string
		namespace      string
		labels         string
		metricsAddr    string
		logLevel       slag.Level
		maxUniqueFiles int
	)

	flag.StringVar(&reportPath, "report", "/data/snoop-report.json", "Path to write the JSON report")
	flag.DurationVar(&reportInterval, "interval", 30*time.Second, "Interval between report writes")
	flag.StringVar(&excludePaths, "exclude", "/proc/,/sys/,/dev/", "Comma-separated path prefixes to exclude")
	flag.StringVar(&imageRef, "image", "", "Image reference for report metadata")
	flag.StringVar(&imageDigest, "image-digest", "", "Image digest for report metadata")
	flag.StringVar(&containerID, "container-id", "", "Container ID for report metadata")
	flag.StringVar(&podName, "pod-name", "", "Pod name for report metadata")
	flag.StringVar(&namespace, "namespace", "", "Namespace for report metadata")
	flag.StringVar(&labels, "labels", "", "Comma-separated key=value labels for report metadata")
	flag.StringVar(&metricsAddr, "metrics-addr", ":9090", "Address for Prometheus metrics endpoint (empty to disable)")
	flag.Var(&logLevel, "log-level", "Log level (debug, info, warn, error)")
	flag.IntVar(&maxUniqueFiles, "max-unique-files", config.DefaultMaxUniqueFiles, fmt.Sprintf("Maximum unique files to track per container (0 = unbounded, default = %d)", config.DefaultMaxUniqueFiles))
	flag.Parse()

	// Build configuration from flags (also check environment variables)
	if podName == "" {
		podName = os.Getenv("POD_NAME")
	}
	if namespace == "" {
		namespace = os.Getenv("POD_NAMESPACE")
	}

	cfg := &config.Config{
		ReportPath:     reportPath,
		ReportInterval: reportInterval,
		ExcludePaths:   config.ParseExcludePaths(excludePaths),
		ImageRef:       imageRef,
		ImageDigest:    imageDigest,
		ContainerID:    containerID,
		PodName:        podName,
		Namespace:      namespace,
		Labels:         parseLabels(labels),
		MetricsAddr:    metricsAddr,
		LogLevel:       slog.Level(logLevel),
		MaxUniqueFiles: maxUniqueFiles,
	}

	// Initialize logging context
	ctx := clog.WithLogger(context.Background(), clog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.Level(logLevel),
	})))

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		clog.FromContext(ctx).Fatalf("Configuration validation failed: %v", err)
	}

	if err := run(ctx, cfg); err != nil {
		clog.FromContext(ctx).Fatalf("Fatal error: %v", err)
	}
}

func parseLabels(s string) map[string]string {
	if s == "" {
		return nil
	}
	result := make(map[string]string)
	for _, pair := range strings.Split(s, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) == 2 {
			result[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}
	return result
}

func run(ctx context.Context, cfg *config.Config) error {
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

	// Initialize metrics and health checker
	m := metrics.New()
	healthChecker := health.New()

	// Start metrics and health server if address is provided
	if cfg.MetricsAddr != "" {
		mux := http.NewServeMux()
		mux.Handle("/metrics", m.Handler())
		mux.Handle("/healthz", healthChecker.Handler())
		server := &http.Server{
			Addr:    cfg.MetricsAddr,
			Handler: mux,
		}
		go func() {
			log.Infof("Starting metrics and health server on %s", cfg.MetricsAddr)
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
	healthChecker.SetEBPFLoaded()

	// Auto-discover all containers in the pod
	log.Info("Discovering containers in pod")
	discoveredContainers, err := cgroup.DiscoverAllExceptSelf()
	if err != nil {
		return fmt.Errorf("discovering containers: %w", err)
	}

	if len(discoveredContainers) == 0 {
		return fmt.Errorf("no containers discovered (pod has only snoop?)")
	}

	log.Infof("Discovered %d containers to trace", len(discoveredContainers))
	for cgroupID, info := range discoveredContainers {
		log.Infof("  - %s (cgroup_id=%d, path=%s)", info.Name, cgroupID, info.CgroupPath)
		if err := probe.AddTracedCgroup(cgroupID); err != nil {
			return fmt.Errorf("adding cgroup %s: %w", info.Name, err)
		}
	}

	// Initialize APK mappers for containers with APK databases
	apkMappers := make(map[uint64]*apk.Mapper)
	for cgroupID, info := range discoveredContainers {
		if info.HasAPK {
			log.Infof("Loading APK database for container %s from %s", info.Name, info.APKDBPath)
			db, err := apk.ParseDatabase(info.APKDBPath)
			if err != nil {
				log.Warnf("Failed to parse APK database for %s: %v", info.Name, err)
				continue
			}
			apkMappers[cgroupID] = apk.NewMapper(db)
			log.Infof("Loaded APK database for %s: %d packages, %d files",
				info.Name, len(db.Packages), len(db.FileToPackage))
		}
	}

	// Convert cgroup.ContainerInfo to processor.ContainerInfo to avoid import cycle
	processorContainers := make(map[uint64]*processor.ContainerInfo)
	for cgroupID, info := range discoveredContainers {
		processorContainers[cgroupID] = &processor.ContainerInfo{
			CgroupID:   info.CgroupID,
			CgroupPath: info.CgroupPath,
			Name:       info.Name,
		}
	}

	// Create processor and reporter
	proc := processor.NewProcessor(ctx, processorContainers, cfg.ExcludePaths, cfg.MaxUniqueFiles)
	rep := reporter.NewFileReporter(ctx, cfg.ReportPath)

	startedAt := time.Now()
	log.Infof("Writing reports to: %s (interval: %s)", cfg.ReportPath, cfg.ReportInterval)

	// Track last seen drops and evictions count for computing deltas
	var lastDrops uint64
	var lastEvicted uint64
	var finalReportWritten bool

	// Start periodic report writer
	reportTicker := time.NewTicker(cfg.ReportInterval)
	defer reportTicker.Stop()

	writeReport := func() {
		containerStats := proc.Stats()
		aggregateStats := proc.Aggregate()
		drops, err := probe.Drops()
		if err != nil {
			log.Warnf("Failed to read drops counter: %v", err)
			drops = 0
		}

		// Update the drops counter metric with the delta
		if drops > lastDrops {
			delta := drops - lastDrops
			m.EventsDropped.Add(float64(delta))
			if delta > 0 {
				log.Warnf("Ring buffer overflow: %d events dropped since last report", delta)
			}
			lastDrops = drops
		}

		// Update the evictions counter metric with the delta
		if aggregateStats.EventsEvicted > lastEvicted {
			delta := aggregateStats.EventsEvicted - lastEvicted
			m.EventsEvicted.Add(float64(delta))
			if delta > 0 {
				log.Warnf("Deduplication cache eviction: %d file paths evicted since last report", delta)
			}
			lastEvicted = aggregateStats.EventsEvicted
		}

		// Build per-container reports
		filesPerContainer := proc.Files()
		containers := make([]reporter.ContainerReport, 0, len(containerStats))
		for cgroupID, stats := range containerStats {
			cr := reporter.ContainerReport{
				Name:        stats.Name,
				CgroupID:    cgroupID,
				CgroupPath:  stats.CgroupPath,
				Files:       filesPerContainer[cgroupID],
				TotalEvents: stats.EventsReceived,
				UniqueFiles: stats.UniqueFiles,
			}

			// Add APK package stats if available
			if mapper, ok := apkMappers[cgroupID]; ok {
				apkStats := mapper.Stats()
				cr.APKPackages = make([]reporter.APKPackageReport, len(apkStats))
				for i, ps := range apkStats {
					cr.APKPackages[i] = reporter.APKPackageReport{
						Name:          ps.Name,
						Version:       ps.Version,
						TotalFiles:    ps.TotalFiles,
						AccessedFiles: ps.AccessedFiles,
						AccessCount:   ps.AccessCount,
					}
				}
			}

			containers = append(containers, cr)
		}

		report := &reporter.Report{
			PodName:       cfg.PodName,
			Namespace:     cfg.Namespace,
			StartedAt:     startedAt,
			Containers:    containers,
			TotalEvents:   aggregateStats.EventsReceived,
			DroppedEvents: drops,
		}
		if err := rep.Update(ctx, report); err != nil {
			log.Errorf("Error writing report: %v", err)
			m.ReportWriteErrors.Inc()
		} else {
			log.Infof("Report written: %d containers, %d unique files, %d events processed, %d dropped, %d evicted",
				len(containers), aggregateStats.UniqueFiles, aggregateStats.EventsProcessed, drops, aggregateStats.EventsEvicted)
			m.ReportWrites.Inc()
			healthChecker.RecordReportWritten()
		}
		// Update gauge for unique files count
		m.UniqueFiles.Set(float64(aggregateStats.UniqueFiles))
	}

	// Read and process events
	log.Info("Waiting for events (press Ctrl+C to exit)")
	for {
		select {
		case <-ctx.Done():
			// Graceful shutdown: write final report
			if !finalReportWritten {
				log.Info("Writing final report")
				writeReport()
				finalReportWritten = true
			}
			return nil

		case <-reportTicker.C:
			writeReport()

		default:
			event, err := probe.ReadEvent(ctx)
			if err != nil {
				if ctx.Err() != nil {
					// Context cancelled, write final report
					if !finalReportWritten {
						log.Info("Writing final report")
						writeReport()
						finalReportWritten = true
					}
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
			healthChecker.RecordEventReceived()

			cgroupID, path, result := proc.Process(procEvent)
			switch result {
			case processor.ResultNew:
				m.EventsProcessed.Inc()
				log.Debugf("New file: %s (container cgroup_id=%d)", path, cgroupID)
				// Record APK access if mapper exists
				if mapper, ok := apkMappers[cgroupID]; ok {
					mapper.RecordAccess(path)
				}
			case processor.ResultDuplicate:
				m.EventsDuplicate.Inc()
			case processor.ResultExcluded:
				m.EventsExcluded.Inc()
			case processor.ResultUnknownContainer:
				// Already logged by processor
			}
		}
	}
}
