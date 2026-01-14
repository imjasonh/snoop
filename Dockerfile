# Build stage
FROM golang:1.21 as builder

WORKDIR /workspace

# Install dependencies for eBPF
RUN apt-get update && apt-get install -y \
    clang \
    llvm \
    libbpf-dev \
    linux-headers-generic \
    && rm -rf /var/lib/apt/lists/*

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Generate eBPF code
RUN go generate ./pkg/ebpf/bpf

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o snoop ./cmd/snoop

# Runtime stage
FROM debian:bookworm-slim

# Install runtime dependencies
RUN apt-get update && apt-get install -y \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /workspace/snoop /usr/local/bin/snoop

ENTRYPOINT ["/usr/local/bin/snoop"]
