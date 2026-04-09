# LFESS — Implementation Plan

> Local-First Encrypted Sync System · Go · MVP

---

## Table of Contents

1. [Overview](#overview)
2. [Package Structure](#package-structure)
3. [Phase 0 — Project Setup](#phase-0--project-setup)
4. [Phase 1 — Data Model & Identity](#phase-1--data-model--identity-internalmodel)
5. [Phase 2 — Append-Only JSON Log](#phase-2--append-only-json-log-internalstore)
6. [Phase 3 — Encryption](#phase-3--encryption-internalcrypto)
7. [Phase 4 — Engine](#phase-4--engine-internalengine)
8. [Phase 5 — HTTP Server](#phase-5--http-server-server)
9. [Phase 6 — Peer-to-Peer LAN Sync](#phase-6--peer-to-peer-lan-sync)
10. [Phase 7 — One-Time Authenticated LAN Bootstrap](#phase-7--one-time-authenticated-lan-bootstrap)
11. [Phase 8 — CLI](#phase-8--cli-cmd)
12. [Phase 9 — Sync Flow End-to-End](#phase-9--sync-flow-end-to-end)
13. [Build Order & Milestones](#build-order--milestones)
14. [Key Libraries](#key-libraries)
15. [Acceptance Criteria](#acceptance-criteria)

---

## Overview

LFESS proves three properties at MVP scale:

- **Local-first**: every device operates fully offline using an append-only encrypted operation log
- **Sync**: two devices exchange operation logs over peer-to-peer HTTP on the same local network (including localhost); one pulls from the other after one-time authenticated bootstrap
- **Merge without data loss**: deduplication by operation ID and a deterministic conflict rule (delete wins) ensure convergence

Single user, multiple devices (simulated via `--data-dir` or run on separate machines on the same network), no central server, no real-time communication required.

---

## Package Structure

```
lfess/
├── main.go
├── cmd/
│   ├── root.go        # cobra setup, global --data-dir flag
│   ├── item.go        # add / update / delete subcommands
│   ├── pair.go        # one-time authenticated bootstrap commands
│   ├── list.go        # view current state
│   └── sync.go        # trigger pull sync from peer
├── internal/
│   ├── model/         # types: Operation, Item, enums
│   ├── store/         # append-only JSON log, safe file writes
│   ├── engine/        # replay, merge, conflict resolution — pure functions
│   ├── bootstrap/     # one-time LAN bootstrap sessions + exchange flow
│   └── crypto/        # age encrypt / decrypt wrappers
└── server/
    └── server.go      # HTTP API: /ops, /health, bootstrap exchange endpoints
```

**Core invariant**: `cmd/` and `server/` are thin shells. All state-manipulation logic lives in `internal/engine`. If you find business logic in a Cobra command or HTTP handler, it belongs in the engine instead.

---

## Phase 0 — Project Setup

Everything in this phase is done once before writing any application code. The goal is a compiling, testable skeleton with all dependencies pinned and the full directory structure in place.

### Prerequisites

Verify the following are installed before starting:

```bash
go version        # must be >= 1.22
git --version
```

### 1. Initialise the module

```bash
mkdir lfess && cd lfess
git init
go mod init github.com/dHarshMakwana/lfess
```

### 2. Create the directory structure

```bash
mkdir -p cmd
mkdir -p internal/model
mkdir -p internal/store
mkdir -p internal/engine
mkdir -p internal/crypto
mkdir -p server
```

### 3. Add all dependencies

```bash
go get github.com/spf13/cobra@latest
go get filippo.io/age@latest
go get github.com/oklog/ulid/v2@latest
go get github.com/stretchr/testify@latest
```

Then tidy to write the lock file:

```bash
go mod tidy
```

Verify `go.sum` was created and `go.mod` lists all four dependencies before continuing.

### 4. Create stub files

Create a minimal compilable file in each package so the module builds from day one. Every stub just declares its package — no logic yet.

```bash
# Entry point
cat > main.go << 'EOF'
package main

func main() {}
EOF

# cmd
cat > cmd/root.go << 'EOF'
package cmd
EOF

# internal packages
cat > internal/model/model.go << 'EOF'
package model
EOF

cat > internal/store/store.go << 'EOF'
package store
EOF

cat > internal/engine/engine.go << 'EOF'
package engine
EOF

cat > internal/crypto/crypto.go << 'EOF'
package crypto
EOF

# server
cat > server/server.go << 'EOF'
package server
EOF
```

### 5. Verify the module compiles

```bash
go build ./...
```

This must exit 0 with no output before you write a single line of application code.

### 6. Set up testing

Create a smoke-test file to confirm the test runner works:

```bash
cat > internal/engine/engine_test.go << 'EOF'
package engine_test

import "testing"

func TestPlaceholder(t *testing.T) {}
EOF

go test ./...
```

Expected output: `ok github.com/yourname/lfess/internal/engine` (and `[no test files]` for other packages — that is fine).

### 7. Initialise `.gitignore`

```bash
cat > .gitignore << 'EOF'
# Binaries
lfess
*.exe

# Test data directories
/tmp/

# Editor
.idea/
.vscode/
*.swp
EOF
```

### 8. First commit

```bash
git add .
git commit -m "chore: project scaffold"
```

### Done when

- `go build ./...` exits 0
- `go test ./...` exits 0
- `go mod tidy` produces no changes after the initial run
- All directories from the package structure exist
- First commit is on `main`

---

## Phase 1 — Data Model & Identity (`internal/model`)

This is the foundation. Define all types here so every other package imports from one place.

### `Operation` — the atomic unit

Every change to state is an operation. Nothing else is ever written to disk.

| Field | Type | Notes |
|---|---|---|
| `OperationID` | `string` | `deviceID + ":" + ulid.Make()` |
| `ItemID` | `string` | Stable ID for the item being changed |
| `DeviceID` | `string` | Originating device |
| `Type` | `enum` | `ADD \| UPDATE \| DELETE` |
| `Payload` | `map[string]any` | `content` string for ADD/UPDATE; `deleted: true` for DELETE |
| `Timestamp` | `time.Time` | Used for display ordering only — never for conflict resolution |

### `Item` — the derived read model

Rebuilt by replaying the operation log. Never stored directly.

| Field | Type |
|---|---|
| `ID` | `string` |
| `Content` | `string` |
| `Deleted` | `bool` |

### Device identity

On first run, generate a short UUID and persist to `~/.lfess/device.id` (or `--data-dir/device.id`). All operation IDs are prefixed with this device ID to guarantee global uniqueness without coordination.

### Operation ID format

Use `deviceID + ":" + ulid.Make()`. ULIDs are time-sortable and globally unique — two devices will never produce the same ID, making deduplication trivially correct.

### Conflict rule — delete wins (with ADD guard)

When replaying, a `DELETE` wins over any concurrent `UPDATE` for the same item — but only if an `ADD` also exists for that item. A `DELETE` with no preceding `ADD` is silently ignored. This prevents a stale or out-of-order `DELETE` from suppressing a valid future `ADD`.

Valid sequence: `ADD → DELETE` → item is deleted.
Invalid sequence: `DELETE` only, or `DELETE → ADD` → `DELETE` is ignored; item exists.

Implement as a two-pass replay:

1. Collect all ops per `ItemID`
2. If no `ADD` exists for that item → skip all ops for it (including any `DELETE`)
3. If an `ADD` exists and any op is a `DELETE` → item is deleted, regardless of `UPDATE` ops
4. Otherwise apply `ADD` then all `UPDATE`s

This makes merge deterministic: given the same set of operations, every device reaches the same state.

---

## Phase 2 — Append-Only JSON Log (`internal/store`)

### File layout

```
~/.lfess/           (or --data-dir)
├── device.id       # persisted device UUID
└── ops.log         # newline-delimited: one base64-encoded age ciphertext per line
```

### Append — pure O_APPEND + fsync

Open with `O_APPEND | O_CREATE | O_WRONLY`. Write one encrypted, base64-encoded JSON line per operation, followed by a newline. Call `f.Sync()` (fsync) after every write to flush to disk before returning. Never seek, never rewrite, no temp files.

```go
f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
// write line + "\n"
f.Sync()
f.Close()
```

> **Why not temp file + rename?** `os.Rename` replaces the entire file, which destroys the append-only guarantee and risks losing concurrent writes. `O_APPEND` + `fsync` is the correct primitive for an append-only log: each write is atomic at the OS level for lines under 4 KB (well within our per-op size), and `fsync` ensures durability before we return success to the caller.

### Read

Read all lines, base64-decode, age-decrypt, unmarshal JSON. Return `[]Operation`. Deduplication by operation ID happens only in `engine.Merge` — the store returns the raw log as-is.

---

## Phase 3 — Encryption (`internal/crypto`)

### Library

`filippo.io/age` — the standard Go age implementation.

### Key management

On first run, generate an age X25519 keypair and write to `~/.lfess/key.age` (chmod 600). For multi-device sync, new devices obtain the existing sync key through one-time authenticated LAN bootstrap (Phase 7). Manual key copy remains a recovery fallback.

### Per-operation encryption

Each operation's JSON bytes are individually wrapped in an `age.Encrypt` stream, then base64-encoded before being written as a log line. On read: base64-decode → `age.Decrypt` → unmarshal.

This keeps the log format simple (each line is a self-contained encrypted blob) and makes partial reads safe — a corrupted line can be skipped without losing the rest.

### Decryption failure behaviour

If any line fails to decrypt (corrupted bytes, wrong key, truncated write), the store **skips that line and logs a warning to stderr** — it does not return an error or crash. This preserves the rest of the log.

```go
op, err := crypto.Decrypt(line)
if err != nil {
    log.Printf("warn: skipping unreadable op at line %d: %v", lineNum, err)
    continue
}
```

The warning must include the line number so it is diagnosable. Callers of the store's read function receive only the successfully decrypted ops.

```go
func Encrypt(plaintext []byte) ([]byte, error)
func Decrypt(ciphertext []byte) ([]byte, error)
```

---

## Phase 4 — Engine (`internal/engine`)

Pure functions only. No I/O, no disk access. The store and HTTP layer call into the engine — never the reverse.

### API

```go
// Merge combines two op slices, deduplicates by OperationID, and returns
// the result in append order (local first, then remote ops not already seen).
// This is the only place deduplication occurs.
func Merge(local, remote []Operation) []Operation

// Replay builds current item state from an already-merged op slice.
// The caller is responsible for passing a deduplicated slice (i.e. the output
// of Merge). Replay does NOT call Merge internally.
func Replay(ops []Operation) map[string]Item
```

**Separation of concerns**: `Merge` owns deduplication. `Replay` owns state derivation. Keeping them separate means each can be tested and reasoned about independently, and callers decide when to merge vs when to just replay a known-clean slice.

### Merge algorithm

```
1. Build a set of seen OperationIDs from local
2. Append remote ops not in that set to local
3. Return the combined slice (local order preserved; new remote ops appended)
```

> Timestamp is **not** used for merge correctness. It is only used inside `Replay` for display ordering of `UPDATE` ops within a single item. Merge correctness depends entirely on operation IDs and the delete-wins rule.

### Replay algorithm

```
Input: []Operation (already deduplicated — output of Merge or a clean local read)

1. Group ops by ItemID
2. For each ItemID:
   a. If no ADD exists → skip (ignore orphan UPDATEs and DELETEs)
   b. If ADD exists and any DELETE exists → Item{Deleted: true}
   c. Otherwise → apply ADD, then UPDATEs sorted by Timestamp for content ordering
3. Return map[string]Item
```

Single-pass variant (preferred for MVP): iterate ops once, maintaining a `map[string]itemState` where each entry tracks `{hasADD, hasDELETE, latestContent}`. One final pass over the map builds the output.

### Testing

Write table-driven tests before writing the store. Cover at minimum:

- Empty op log → empty state
- Single ADD → one item
- ADD then UPDATE → updated content
- ADD then DELETE → deleted item
- DELETE with no ADD → item does not appear in state (orphan delete ignored)
- DELETE then ADD (out of order) → item exists (delete ignored without prior ADD)
- ADD on A + ADD on B, merge → both items present (Scenario 1)
- ADD on A + DELETE on A + UPDATE on B, merge → deleted wins (Scenario 2)
- Duplicate op IDs in Merge input → deduplicated, state unchanged
- All ops already present on both sides → Merge returns same slice, Replay unchanged
- 100 ops across two devices, merged → state matches independent replay of union

---

## Phase 5 — HTTP Server (`server/`)

Minimal, standard library only. No framework.

### Endpoints

| Method | Path | Behaviour |
|---|---|---|
| `GET` | `/ops` | Returns the full encrypted op log as a JSON array of base64 strings |
| `POST` | `/ops` | Accepts ops array, merges locally |
| `GET` | `/health` | Returns `200 OK` |

### Configuration

Default host is `127.0.0.1` and default port is `7777`, overridable via `--host`/`--port` flags or `LFESS_HOST`/`LFESS_PORT` env vars. No central server is required.

### Server setup

```go
srv := &http.Server{
    Addr:         fmt.Sprintf("%s:%d", host, port),
    ReadTimeout:  10 * time.Second,
    WriteTimeout: 10 * time.Second,
}
```

Run in a goroutine. Catch `SIGINT` / `SIGTERM` for graceful shutdown via `srv.Shutdown(ctx)`.

### `/ops` response contract

`GET /ops` returns the **full operation log in append order** (the order lines were written to `ops.log`), with no filtering, sorting, or deduplication applied server-side. Each element is the raw base64-encoded age ciphertext exactly as stored on disk.

```json
["base64encodedciphertext1", "base64encodedciphertext2", "..."]
```

The receiving device is responsible for all decryption, deduplication (`Merge`), and state derivation (`Replay`). The server is a dumb log reader — it applies no business logic to the response.

Both devices must share the same age key for the receiver to decrypt the payload. Phase 7 defines the authenticated bootstrap flow that provisions this key to new devices.

---

## Phase 6 — Peer-to-Peer LAN Sync

This phase enables direct multi-device sync over the same local network while preserving the no-central-server architecture.

### Network model

- Pure peer-to-peer communication between devices over HTTP
- Manual peer addressing (`http://<peer-ip>:<port>`)
- No relay, coordinator, or central synchronization node

### Host binding for LAN peers

- Add host configurability with loopback as the safe default
    - `--host` flag, default `127.0.0.1`
    - `LFESS_HOST` env override
- Use `--host 0.0.0.0` when accepting sync requests from other machines on the same network
- Keep existing port controls (`--port`, `LFESS_PORT`)

### API behavior in P2P mode

- `GET /ops` remains full encrypted log export in append order
- `POST /ops` imports peer operations and triggers local merge through existing store + engine flow
- No server-side conflict logic outside `engine`

### Security assumptions (MVP)

- Single-user trusted network scope
- Sync transport (`/ops`) has no per-request auth in MVP
- Device onboarding authentication is handled by one-time bootstrap in Phase 7
- Operation confidentiality remains protected by age-encrypted log entries

---

## Phase 7 — One-Time Authenticated LAN Bootstrap

This phase removes manual `key.age` copying from the normal onboarding path by introducing a single-use, authenticated bootstrap flow over LAN.

### Design goals

- Pair a new device in one explicit user action
- Keep bootstrap sessions short-lived and single-use
- Avoid persistent bootstrap secrets on disk
- Keep regular sync (`GET /ops` / `POST /ops`) unchanged

### Bootstrap flow

1. **Trusted device starts pairing session**
    - Command: `lfess pair start --ttl 2m`
    - Generates:
      - random `session_id`
      - one-time pairing code (shared out-of-band)
      - in-memory expiry and single-use state

2. **New device requests bootstrap**
    - Command: `lfess pair join http://<peer-ip>:<port> --code <pair-code>`
    - Generates an ephemeral age keypair for response encryption
    - Calls bootstrap exchange endpoint with `session_id`, pairing code proof, and ephemeral recipient

3. **Trusted device authenticates and returns encrypted key material**
    - Validates session exists, is unexpired, and unused
    - Validates pairing code proof
    - Encrypts local `key.age` payload to requester ephemeral recipient
    - Marks session used and returns encrypted bootstrap payload

4. **New device installs key and finalizes**
    - Decrypts bootstrap payload using ephemeral private key
    - Writes `key.age` with `0600`
    - Future `lfess sync` calls work without manual key copying

### Failure and safety requirements

- Invalid, expired, or replayed sessions are rejected
- Pairing code attempts are rate-limited
- Bootstrap session state is memory-only and auto-expires
- Key file writes are atomic and permissions-checked

---

## Phase 8 — CLI (`cmd/`)

Use [Cobra](https://github.com/spf13/cobra) for subcommand routing. Global `--data-dir` flag (default `~/.lfess`) lets you run two instances on the same machine by pointing them at different directories, while `--host`/`--port` support serving peers across the same local network.

### Subcommands

| Command | Behaviour |
|---|---|
| `lfess add <content>` | Creates an ADD operation, encrypts, appends to log |
| `lfess update <id> <content>` | Creates an UPDATE operation for the given item ID |
| `lfess delete <id>` | Creates a DELETE operation for the given item ID |
| `lfess list` | Replays log, prints non-deleted items with their IDs |
| `lfess pair start [--ttl D]` | Starts a one-time authenticated bootstrap session and prints pairing details |
| `lfess pair join <peer-addr> --code <code>` | Joins bootstrap session, imports `key.age`, and marks session consumed |
| `lfess sync <peer-addr>` | Fetches `/ops` from peer, merges, writes missing ops to local log |
| `lfess serve [--host H] [--port N]` | Starts the HTTP server |

### Example session — two devices on the same network

```bash
# Terminal 1 — Device A
lfess --data-dir /tmp/a serve --host 0.0.0.0 --port 7777 &
lfess --data-dir /tmp/a pair start --ttl 2m
# prints one-time pairing code
lfess --data-dir /tmp/a add "buy oat milk"

# Terminal 2 — Device B (same LAN)
lfess --data-dir /tmp/b pair join http://192.168.1.10:7777 --code <pair-code>
lfess --data-dir /tmp/b add "finish the RFC"
lfess --data-dir /tmp/b sync http://192.168.1.10:7777

# Both devices now have both items
lfess --data-dir /tmp/a list
lfess --data-dir /tmp/b list
```

---

## Phase 9 — Sync Flow End-to-End

```
Device A                              Device B
─────────────────────────────────────────────────────
Operate offline                       Operate offline
Append ops to local log               Append ops to local log

lfess pair start --ttl 2m             lfess pair join http://A:7777 --code ******
    ← ← ← bootstrap exchange (single-use, authenticated) → → →

                                      lfess sync http://A:7777
  ← ← ←  GET /ops  ← ← ← ← ← ← ← ← ←
  → → →  []EncryptedOp  → → → → → → →

                                      Decrypt each op
                                      engine.Merge(local, remote)
                                      Write missing ops to log
                                      engine.Replay(merged) → state

Both call engine.Replay → identical state (converged)
```

### Sync is pull-based

Device B initiates. Device A only needs to be serving (`lfess serve`). For bidirectional sync, both devices call `lfess sync` against each other.

### Full log on every sync

MVP sends the full log every time. Deduplication on the receiving side is cheap. Optimise to delta sync after MVP is proven.

---

## Build Order & Milestones

Build strictly in dependency order — each layer is testable before the next is started.

| Milestone | Package | Done when |
|---|---|---|
| **M1** | `internal/model` + `internal/crypto` | Types compile; encrypt/decrypt round-trip passes unit tests |
| **M2** | `internal/store` | Append and read work; two concurrent appends produce no interleaving |
| **M3** | `internal/engine` | All table-driven tests pass including both spec scenarios |
| **M4** | `server/` | Server starts; `curl http://127.0.0.1:7777/ops` returns valid JSON |
| **M5** | `server/` + `internal/crypto` + `cmd/` | One-time authenticated bootstrap provisions `key.age` to a new device using single-use expiring sessions |
| **M6** | `server/` + `cmd/` | Two machines on the same local network sync directly with no central server and no manual key copy |
| **M7** | `cmd/` | All subcommands wired, including pairing, local state, and LAN sync |
| **M8** | Integration | Scripted two-device scenario (pair then sync) passes end-to-end |

---

## Key Libraries

| Purpose | Library |
|---|---|
| CLI | `github.com/spf13/cobra` |
| Age encryption | `filippo.io/age` |
| Bootstrap token/session auth | `crypto/hmac`, `crypto/sha256`, `crypto/rand` (stdlib) |
| Unique IDs | `github.com/oklog/ulid/v2` |
| Assertions in tests | `github.com/stretchr/testify/assert` |
| HTTP | `net/http` (stdlib) |
| File I/O | `os`, `bufio` (stdlib) |

No ORM, no database driver, no framework. Minimal dependency surface is consistent with the local-first, no-central-server constraint.

---

## Acceptance Criteria

### AC-1 — Local operation logging

- [ ] Every `add`, `update`, and `delete` command appends exactly one operation to the local log
- [ ] The log is never mutated — only appended to; line count never decreases
- [ ] State is correctly derived by replaying the log from scratch on every read
- [ ] Every append is followed by `fsync`; a simulated crash mid-write leaves the log readable (all complete lines intact)

### AC-2 — Encryption

- [ ] Every line in `ops.log` is age-encrypted; plaintext is never written to disk
- [ ] Decryption succeeds on every line written by the same device
- [ ] A log file from Device A can be decrypted by Device B when both use the same key
- [ ] A corrupted or truncated line is **skipped with a stderr warning** — the process does not crash and all other lines are returned normally
- [ ] The warning includes the line number of the failed decryption

### AC-3 — Operation identity & deduplication

- [ ] Every operation ID is globally unique across devices (verified by generating 10,000 IDs across two simulated devices with no collision)
- [ ] Deduplication happens only inside `engine.Merge` — no other layer performs it
- [ ] `Merge(local, remote)` where remote contains IDs already in local produces a slice with no duplicate IDs
- [ ] `Replay` called with a deduplicated slice produces the same state regardless of how many times it is called

### AC-4 — Conflict resolution (delete wins, with ADD guard)

- [ ] When Device A deletes an item and Device B updates the same item, after merge both devices see the item as deleted
- [ ] The conflict rule applies regardless of which device's op has the earlier timestamp — timestamps do not influence correctness
- [ ] A `DELETE` op with no corresponding `ADD` for the same `ItemID` is silently ignored — the item does not appear in state
- [ ] A `DELETE` that arrives before an `ADD` (out-of-order import) does not suppress the `ADD` — the item exists after merge
- [ ] The rule is enforced entirely inside `Replay`; `Merge` applies no conflict logic

### AC-5 — Sync: Scenario 1 (independent adds)

- [ ] Device A adds item X
- [ ] Device B adds item Y (independently, no sync yet)
- [ ] Device B runs `lfess sync http://A:port`
- [ ] Device B's state contains both X and Y
- [ ] Device A's state (after its own sync or serve) also contains both X and Y

### AC-6 — Sync: Scenario 2 (delete vs update)

- [ ] Device A deletes item X
- [ ] Device B updates item X with new content (independently)
- [ ] After sync in either direction, both devices see item X as deleted
- [ ] No data from Device B's update leaks back into the item content after merge

### AC-7 — HTTP API

- [ ] `GET /ops` returns a valid JSON array of base64-encoded ciphertexts in append order (no sorting, no filtering, no deduplication server-side)
- [ ] `POST /ops` accepts operation payloads from peers and imports missing operations locally
- [ ] The response contains exactly the same lines as `ops.log`, in the same order
- [ ] `GET /health` returns HTTP 200
- [ ] Server handles concurrent requests without data races (verified with `-race` flag)
- [ ] Server shuts down cleanly on `SIGINT` without dropping in-flight requests

### AC-8 — One-time authenticated LAN bootstrap

- [ ] Trusted device can start a bootstrap session that emits a single-use pairing code and expires automatically
- [ ] New device can join using valid pairing details and receive `key.age` securely
- [ ] Invalid, expired, or already-used pairing sessions are rejected with clear errors
- [ ] Bootstrap success writes `key.age` with mode `0600`, and subsequent runs reuse it
- [ ] After bootstrap, same-network sync works without manual key copy

### AC-9 — CLI usability

- [ ] `lfess list` shows only non-deleted items with their IDs and content
- [ ] `lfess list` output is consistent with a fresh replay of the local log
- [ ] `lfess sync <addr>` exits non-zero and prints a clear error if the peer is unreachable
- [ ] `lfess sync <addr>` exits non-zero if decryption of any received op fails
- [ ] All commands respect `--data-dir` and do not read from or write to `~/.lfess` when the flag is set

### AC-10 — Offline operation

- [ ] All commands except `sync` and `serve` work with no network access
- [ ] State is consistent after 100 add/update/delete operations with no sync (stress test)
- [ ] Two instances running with separate `--data-dir` values do not share any state until `sync` is explicitly called

### AC-11 — No history mutation

- [ ] The line count of `ops.log` never decreases after any operation
- [ ] No existing line in `ops.log` is ever modified after it is written (verified by checksumming all lines before and after a `sync`)
- [ ] A `DELETE` operation does not remove earlier `ADD`/`UPDATE` ops from the log
- [ ] A sync that imports zero new ops leaves `ops.log` byte-for-byte identical to before the sync

### AC-12 — Same-network peer-to-peer sync

- [ ] Device A and Device B on the same local network can sync directly using `lfess sync http://<peer-ip>:<port>`
- [ ] Both devices can serve and sync without any centralized coordinator
- [ ] Host binding supports both loopback default (`127.0.0.1`) and LAN mode (`0.0.0.0`)
- [ ] Scenario 1 and Scenario 2 converge identically in same-network mode

---

*MVP is complete when all twelve acceptance criteria are green.*
