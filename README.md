# mymemcache-top

`mymemcache-top` is a terminal user interface inspired by `htop` for monitoring one or more Memcached instances. It samples the server's `stats` output, highlights useful counters, and tracks per-second rates so you can quickly spot cache pressure or throughput changes.

## Features

- Live refresh of key Memcached metrics (hit ratio, evictions, memory usage, connection counts, command rates, bandwidth, and more).
- Per-second rate calculations for command and bandwidth stats.
- Keyboard shortcuts for quick resets and exiting (`q`, `Ctrl+C`, `Esc`, `r`).
- Works out of the box against `127.0.0.1:11211`; configurable host and port via flags or positional arguments.

## Getting Started

### Prerequisites

- Go 1.21 or newer (module is tested with Go 1.24).
- Access to a running Memcached server you want to monitor.

### Build

```bash
go build -o memtop ./cmd/memtop
```

For a portable static binary (helpful on older GNU/Linux distributions), disable cgo when building:

```bash
CGO_ENABLED=0 go build -o memtop ./cmd/memtop
```

### Run

```bash
./memtop [flags] [host [port]]
```

Flags:

- `-host` (`string`): Memcached host (default `127.0.0.1`)
- `-port` (`int`): Memcached port (default `11211`)
- `-interval` (`duration`): Refresh interval (default `2s`)

Examples:

```bash
# Monitor default localhost instance
./memtop

# Override via positional arguments
./memtop cache.internal 12000

# Override via flags and adjust refresh to 1 second
./memtop -host cache.internal -port 12000 -interval=1s
```

### Controls

- `q`, `Q`, `Ctrl+C`, `Esc`: Quit the program.
- `r`: Reset the rate calculations to establish a new baseline.

## Project Layout

- `cmd/memtop/main.go`: Program entry point and TUI implementation.
- `go.mod`, `go.sum`: Module definition and dependencies.

## License

This project is licensed under the MIT License. See [LICENSE](LICENSE) for details.
