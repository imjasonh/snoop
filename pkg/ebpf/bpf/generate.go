package bpf

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -target amd64,arm64 -type event snoop snoop.c -- -I/usr/include -I/usr/include/bpf
