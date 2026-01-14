.PHONY: help generate build test docker-build clean

help: ## Show this help message
	@echo 'Usage: make [target]'
	@echo ''
	@echo 'Available targets:'
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-20s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

vmlinux: ## Generate vmlinux.h from current kernel (Linux only)
	@if [ ! -f /sys/kernel/btf/vmlinux ]; then \
		echo "Error: /sys/kernel/btf/vmlinux not found. Kernel BTF support required."; \
		exit 1; \
	fi
	bpftool btf dump file /sys/kernel/btf/vmlinux format c > pkg/ebpf/bpf/vmlinux.h
	@echo "Generated pkg/ebpf/bpf/vmlinux.h"

generate: ## Generate eBPF code (requires clang, llvm, vmlinux.h)
	go generate ./pkg/ebpf/bpf

generate-in-docker: ## Generate eBPF code in Docker (works on macOS)
	@echo "Building multi-platform Docker image to generate eBPF code..."
	docker build --platform=linux/amd64 --target builder -t snoop-builder:amd64 -f Dockerfile .
	docker build --platform=linux/arm64 --target builder -t snoop-builder:arm64 -f Dockerfile .
	@echo "Extracting generated files from amd64 build..."
	docker create --name snoop-builder-amd64 snoop-builder:amd64
	docker cp snoop-builder-amd64:/workspace/pkg/ebpf/bpf/snoop_x86_bpfel.go pkg/ebpf/bpf/
	docker cp snoop-builder-amd64:/workspace/pkg/ebpf/bpf/snoop_x86_bpfel.o pkg/ebpf/bpf/
	docker rm snoop-builder-amd64
	@echo "Extracting generated files from arm64 build..."
	docker create --name snoop-builder-arm64 snoop-builder:arm64
	docker cp snoop-builder-arm64:/workspace/pkg/ebpf/bpf/snoop_arm64_bpfel.go pkg/ebpf/bpf/
	docker cp snoop-builder-arm64:/workspace/pkg/ebpf/bpf/snoop_arm64_bpfel.o pkg/ebpf/bpf/
	docker rm snoop-builder-arm64
	@echo "Cleaning up temporary images..."
	docker rmi snoop-builder:amd64 snoop-builder:arm64
	@echo "Generated files extracted to pkg/ebpf/bpf/"
	@echo "Verifying files..."
	@ls -lh pkg/ebpf/bpf/snoop_*.go pkg/ebpf/bpf/snoop_*.o

build: generate ## Build the snoop binary
	go build -o snoop ./cmd/snoop

test: ## Run tests
	go test ./...

docker-build: ## Build Docker image
	docker build -t snoop:latest .

docker-compose-up: ## Start test environment
	cd deploy && docker compose up -d

docker-compose-down: ## Stop test environment
	cd deploy && docker compose down

clean: ## Clean build artifacts
	rm -f snoop
	rm -f pkg/ebpf/bpf/snoop_*.go
	rm -f pkg/ebpf/bpf/snoop_*.o

.DEFAULT_GOAL := help
