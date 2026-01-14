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
