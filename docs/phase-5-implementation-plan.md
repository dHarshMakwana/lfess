# LFESS — Phase 5 Implementation Plan (HTTP Server)

> Phase 5 — `server/`
>
> This document is a *separate* implementation plan for Phase 5 only. It does not change or replace the original plan; it elaborates the exact same Phase 5 design into actionable steps.

---

## Scope

### Goal

Implement the minimal HTTP server in `server/` so one device can expose its local encrypted operation log and a peer can pull it.

- Use only the Go standard library (`net/http`)
- Keep server logic thin: read encrypted lines from store and return them as JSON
- Keep all business logic (decrypt, dedup, conflict rules) outside `server/`

### Non-goals (MVP)

- No authentication/authorization layer in MVP
- No real-time sync or push protocol
- No server-side deduplication, replay, or merge logic
- No operation log mutation from `GET /ops`
- No framework/middleware stack

---

## Contract (what `server/` must do)

### `GET /ops`

- Returns the **full encrypted operation log** in append order
- Response shape is a JSON array of base64 ciphertext strings:

```json
["base64encodedciphertext1", "base64encodedciphertext2", "..."]
```

- Each array element must match exactly one non-empty line from `ops.log`
- No sorting, filtering, deduplication, decryption, or transformation

### `GET /health`

- Returns HTTP `200 OK`
- Used for liveness checks only

### `POST /ops`

- Required endpoint for peer imports
- Accepts operation payload from peer and merges locally through store + engine flow
- Must not bypass merge and replay invariants defined in earlier phases

---

## Server Configuration & Lifecycle

### Bind and port

- Host bind is configurable with safe default:
  - default host `127.0.0.1`
  - `--host` flag / `LFESS_HOST` environment variable override
- Default port: `7777`
- Port override via `--port` flag / `LFESS_PORT` environment variable
- For same-network peers in the next phase, bind host can be set to `0.0.0.0`

### HTTP server settings

Use the original plan configuration:

```go
srv := &http.Server{
    Addr:         fmt.Sprintf("%s:%d", host, port),
    ReadTimeout:  10 * time.Second,
    WriteTimeout: 10 * time.Second,
}
```

### Startup and shutdown

- Start the HTTP server in a goroutine
- Listen for `SIGINT` and `SIGTERM`
- On signal, call `srv.Shutdown(ctx)` for graceful shutdown

---

## Files & responsibilities (Phase 5)

- `server/server.go`
  - route registration
  - handlers for `GET /ops`, `POST /ops`, and `GET /health`
  - server startup/shutdown lifecycle
  - minimal dependency wiring to store read API

- `server/server_test.go`
  - handler contract tests
  - concurrency/race-focused tests
  - graceful shutdown behavior tests

---

## Implementation steps (do in this order)

### 1) Define minimal server dependencies

Keep the server thin by depending only on what it needs to serve `/ops`:

- A read method that returns encrypted log lines in append order
- No decryption, merge, or replay logic inside handlers

### 2) Build route mux

Register only the required endpoints for Phase 5:

- `GET /ops`
- `POST /ops`
- `GET /health`

Return method errors cleanly for unsupported methods.

### 3) Implement `GET /health`

- Always respond `200 OK`
- Small constant body is fine (`ok`)

### 4) Implement `GET /ops`

- Call store log reader for encrypted lines (already append-order)
- Set `Content-Type: application/json`
- Encode `[]string` directly to response body
- On read/encode failure, return `500` with a clear error

### 5) Implement `POST /ops`

- Parse request payload from peer
- Delegate merge/import to existing store + engine flow
- Return clear status codes for invalid payload vs internal failures

### 6) Add startup + graceful shutdown wiring

- Construct `http.Server` with configurable host bind and timeouts
- Start serving in a goroutine
- Handle interrupt/terminate signals
- Shutdown with context timeout

### 7) Add tests before CLI wiring

Phase 5 tests should validate the server layer in isolation:

- `/health` returns `200`
- `/ops` returns valid JSON array
- `/ops` preserves append order exactly
- `/ops` with empty/missing log returns `[]`
- `POST /ops` accepts valid payload and imports ops
- `POST /ops` rejects invalid payload with non-2xx status
- concurrent requests pass under race detector
- graceful shutdown path returns cleanly

### 8) Verify with manual smoke checks

- Run server on `127.0.0.1:7777` (or LAN bind for peer tests)
- `curl /health` returns `200`
- `curl /ops` returns JSON array of encrypted lines

---

## Edge cases to explicitly handle

- `ops.log` missing on first run (must return empty array, not error)
- Store read failure (return HTTP `500`)
- JSON encoding failure (return HTTP `500`)
- `POST /ops` invalid JSON or invalid operation payload
- `POST /ops` merge/import failure path
- Unsupported HTTP method on known routes
- Concurrent `/ops` requests while log grows
- Shutdown signal during active requests

---

## Acceptance Criteria (Phase 5)

- [x] `GET /ops` returns a valid JSON array of base64 ciphertexts in append order.
- [x] `GET /ops` applies no sorting, filtering, deduplication, or business logic.
- [x] `/ops` response contains exactly the same non-empty ciphertext lines as `ops.log`, in the same order.
- [x] `POST /ops` is implemented and imports peer operations via store + engine flow.
- [x] `GET /health` returns HTTP `200`.
- [x] Server host bind is configurable (default `127.0.0.1`, supports `0.0.0.0` for same-network peers).
- [x] Default port is `7777`, with override support for `--port` and `LFESS_PORT`.
- [x] `ReadTimeout` and `WriteTimeout` are both `10s`.
- [x] Server handles concurrent requests without data races (`go test -race ./...`).
- [x] Server shuts down cleanly on `SIGINT`/`SIGTERM` using `srv.Shutdown(ctx)`.

---

## Checklist (Phase 5)

- [x] Create `server/server.go` HTTP mux and handler wiring
- [x] Implement `GET /health` (`200 OK`)
- [x] Implement `GET /ops` returning raw encrypted lines as `[]string` JSON
- [x] Implement `POST /ops` import path
- [x] Add configurable host bind (`--host` / `LFESS_HOST`) with safe default `127.0.0.1`
- [x] Add timeout configuration (`ReadTimeout`, `WriteTimeout`)
- [x] Add graceful shutdown on `SIGINT`/`SIGTERM`
- [x] Add `server/server_test.go` for handler, ordering, and error-path tests
- [x] Run `go test ./...`
- [x] Run `go test -race ./...`
- [x] Manual smoke check with `curl /health` and `curl /ops`