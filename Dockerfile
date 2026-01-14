# Build stage
FROM golang:1.25 AS builder

WORKDIR /workspace

# Install dependencies for eBPF
RUN apt-get update && apt-get install -y \
    clang \
    llvm \
    libbpf-dev \
    curl \
    && rm -rf /var/lib/apt/lists/*

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Generate eBPF code
RUN go generate ./pkg/ebpf/bpf

# Verify generated files exist and check their contents
RUN ls -la pkg/ebpf/bpf/ && \
    echo "=== Checking generated file contents ===" && \
    grep -E "^package|^type Snoop|^func.*Snoop" pkg/ebpf/bpf/snoop_arm64_bpfel.go || true

# Build the application
# Check what architecture we're building for
RUN echo "Building for: GOOS=linux GOARCH=$(go env GOARCH)" && \
    echo "Checking which bpf files will be included:" && \
    ls pkg/ebpf/bpf/*_$(go env GOARCH)_*.go || echo "No arch-specific files found"

# Try compiling just the bpf package first
RUN CGO_ENABLED=0 GOOS=linux go build -v ./pkg/ebpf/bpf || echo "BPF package failed to build"

# Build without -a flag to avoid rebuilding stdlib
RUN CGO_ENABLED=0 GOOS=linux go build -v -o snoop ./cmd/snoop

# Runtime stage
FROM debian:bookworm-slim

# Install runtime dependencies (including bash for wrapper scripts)
RUN apt-get update && apt-get install -y \
    ca-certificates \
    bash \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /workspace/snoop /usr/local/bin/snoop

# No ENTRYPOINT - let Kubernetes control the command
# When run directly: docker run image /usr/local/bin/snoop [args]
