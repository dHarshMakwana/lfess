# LFESS — Phase 6 Implementation Plan (Peer-to-Peer LAN Sync + mDNS Discovery)

> Phase 6 — LAN peer-to-peer sync over HTTP, with optional mDNS discovery.
>
> This phase extends networking ergonomics only. Core sync correctness remains defined by existing store + engine invariants.

---

## 1. Objective

Deliver reliable same-LAN peer-to-peer sync with **HTTP as the core protocol**, preserving all existing invariants:

- no central coordinator
- append-only encrypted log remains source of truth
- merge and replay logic remains in `internal/engine`
- mDNS is only a convenience layer for peer discovery

---

## 2. Scope

### In scope

- Manual sync by explicit peer address: `http://<peer-ip>:<port>`
- Host binding support:
  - default `127.0.0.1`
  - LAN mode via `0.0.0.0`
- Optional mDNS peer discovery using HashiCorp `mdns`
- CLI wiring for manual sync and peer discovery
- Integration tests validating two-device convergence over HTTP

### Out of scope

- Any change to merge semantics
- Any change to operation log format
- Any change to encryption/key invariants
- Auth, trust, identity exchange, NAT traversal
- Replacing HTTP sync with mDNS transport

---

## 3. Architecture and Responsibilities

### Core sync protocol (authoritative)

- Transport: HTTP only
- Source endpoint: `GET /ops` returns raw encrypted lines in append order
- Import endpoint: `POST /ops` imports encrypted lines via store import flow
- Merge and conflict resolution stay in engine/store path only

### Component boundaries

1. `internal/peersync` (new)
- Responsibility: orchestrate manual pull sync over HTTP
- Allowed to:
  - GET peer `/ops`
  - decode `[]string` payload
  - call local `OpsLog.ImportEncryptedLines`
- Not allowed to:
  - deduplicate or replay operations itself
  - implement conflict rules

2. `internal/discovery` (new)
- Responsibility: define discovery abstraction (`Discoverer`) and peer model
- Must be independent of engine/store/protocol logic

3. `internal/discovery/mdns` (new adapter)
- Responsibility: HashiCorp mdns-backed discover/announce implementation
- Discovery-only concern, fully replaceable adapter
- Must not alter HTTP sync request/response flow

4. CLI (`cmd/`)
- `sync <peer-addr>`: manual explicit-address sync (core path)
- `discover`: optional mDNS lookup and display of candidate peer addresses
- `serve`: host bind + optional non-blocking mDNS announce wiring

5. Existing packages (`internal/engine`, `internal/store`, `server`)
- No business logic relocation
- No protocol shape changes

---

## 4. Implementation Plan

### Step 1 — Manual HTTP peer sync command

- Add `lfess sync <peer-addr>` command.
- Validate explicit address includes `http://` and host.
- Pull peer ops (`GET /ops`), import locally via `OpsLog.ImportEncryptedLines`.
- Return imported count and non-zero error on:
  - unreachable peer
  - non-200 response
  - invalid payload
  - decrypt/import failure

### Step 2 — mDNS discovery abstraction and adapter

- Add `internal/discovery` interface + peer type.
- Add `internal/discovery/mdns` implementation with timeout-aware query.
- Keep it independent from sync protocol and store/engine internals.

### Step 3 — Optional non-blocking mDNS announce in serve path

- Add optional announce flag in `serve` command.
- Start mdns announcer only when enabled.
- If mdns announce setup fails, log warning and continue serving HTTP.
- Ensure shutdown cleans up announcer without affecting sync correctness.

### Step 4 — Verification-first test coverage

- Manual sync integration tests with two data dirs and two HTTP servers.
- Host binding tests for loopback and LAN bind path.
- Discovery tests for separation and non-blocking behavior.
- Re-run existing store/engine/server tests to prove invariants unchanged.

---

## 5. Acceptance Criteria (Phase 6)

### AC-6.1 Core P2P HTTP Sync

- [x] Two devices on same LAN can sync via direct HTTP (`sync http://<peer-ip>:<port>`)
- [x] No central coordinator is required
- [x] Sync converges via existing merge/replay semantics

### AC-6.2 Host Binding Modes

- [x] Default serve bind is `127.0.0.1`
- [x] `--host 0.0.0.0` binds for LAN access
- [x] Existing host/port config contracts remain intact

### AC-6.3 Manual Sync Reliability

- [x] `sync` requires explicit address input
- [x] Unreachable peer returns non-zero error
- [x] Invalid response/import failure returns non-zero error
- [x] Repeated sync is idempotent (`imported = 0` once fully converged)

### AC-6.4 mDNS Discovery Layer Constraints

- [x] mDNS only discovers peers on local network
- [x] mDNS does not replace or alter HTTP sync protocol
- [x] mDNS has no coupling to engine/store conflict logic
- [x] mDNS component is replaceable behind discovery abstraction
- [x] mDNS remains optional and non-blocking for core sync

### AC-6.5 Invariant Preservation

- [x] No business logic leaks outside engine
- [x] Append-only log behavior unchanged
- [x] Encryption-at-rest behavior unchanged
- [x] Merge dedup + replay conflict invariants unchanged

---

## 6. Validation Protocol (Strict Zero-Assumption)

For each acceptance criterion, use this cycle:

1. Inspect implementation path directly (no assumptions).
2. Execute targeted tests that exercise the exact path.
3. Validate observed runtime behavior and outputs.
4. If any gap exists:
- identify precise failure point
- patch immediately
- re-run same validation until green

Evidence will include:
- test names mapped to acceptance criteria
- runtime assertions (status codes, imported counts, state hashes)
- explicit confirmation of separation-of-concerns boundaries

---

## 7. Definition of Done

Phase 6 is done only when all are true:

- Two devices sync over same-LAN HTTP with no central server.
- Host binding works for both localhost and LAN modes.
- Manual peer-address sync is reliable and validated.
- mDNS discovery works as optional convenience and is non-blocking.
- No business logic moved outside engine.
- Append-only, encryption, and merge invariants remain intact.

---

## 8. Validation Evidence

### Core and manual sync behavior

- `go test ./...` passes, including:
  - `internal/peersync`: scenario convergence, idempotency, unreachable peer, wrong-key failure
  - `cmd/sync_test.go`: explicit-address validation and command-level error behavior

### mDNS behavior and separation

- `internal/discovery/mdns/mdns_test.go` validates:
  - announce config validation
  - non-blocking timeout behavior
  - successful discovery of announced peer
- `cmd/serve_test.go` validates mDNS is optional and non-blocking:
  - serve continues when announcer setup fails
  - announcer lifecycle cleanup on exit

### Concurrency safety checks

- Race checks passed for changed and concurrency-critical paths:
  - `go test -race ./cmd ./internal/discovery/mdns ./internal/peersync ./internal/store`
  - `go test -race ./server -run 'TestOps_ConcurrentRequests|TestChecklistT9_ConcurrentDuplicateImportRace|TestRun_GracefulShutdownOnSignal|TestRun_GracefulShutdownWaitsForInflightRequest'`
