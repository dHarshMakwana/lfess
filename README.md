# LFESS

LFESS is a local-first encrypted sync system written in Go.

## Requirements

- Go 1.24.6 or newer

## How to run

This repository is a Go module with a `main.go` entrypoint. If you try to run `lfess` before building or installing it, your shell will return `command not found`.

### Option 1: run directly from the repo

From the repository root:

```bash
go run . --help
```

Examples:

```bash
go run . add "hii"
go run . list
go run . sync http://127.0.0.1:7777
```

### Option 2: build a local binary

```bash
go build -o lfess .
./lfess --help
./lfess add "hii"
```

### Option 3: install into your Go bin directory

If you want to run `lfess` from anywhere, install it and make sure your Go bin directory is on `PATH`.

```bash
go install .
```

If `lfess` is still not found after installation, add your Go bin directory to `PATH`.

```bash
export PATH="$PATH:$(go env GOPATH)/bin"
```

## Common commands

```bash
lfess add <content>
lfess update <id> <content>
lfess delete <id>
lfess list
lfess pair start
lfess pair join <peer-addr> --code <pair-code>
lfess sync <peer-addr>
lfess serve
lfess discover
```

## Data directory

LFESS stores encrypted data in `~/.lfess` by default.

You can override the storage location with `--data-dir`:

```bash
lfess --data-dir /tmp/lfess add "hello"
```

## Useful notes

- `ops.log` contains encrypted operations.
- `key.age` contains the local age identity used to encrypt and decrypt data.
- `pair start` and `pair join` are for one-time authenticated bootstrap on the same network.
- `sync` pulls encrypted operations from a peer over HTTP.

## Development

Run the test suite with:

```bash
go test ./...
```

Run the race detector with:

```bash
go test -race ./...
```