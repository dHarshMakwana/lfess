# LFESS

> Local-first encrypted sync for small list data across devices.

## Core Idea

LFESS does not treat items as mutable rows. Every change is recorded as an encrypted operation in an append-only log, and the current view is rebuilt by replaying those operations in a deterministic order. That means each device keeps a local source of truth, writes once, and syncs by exchanging only the missing operations.

## Example

**Input:**
```bash
go run . --data-dir /tmp/lfess-demo add "buy oat milk"
go run . --data-dir /tmp/lfess-demo list
```

**Transformation:**
`add` generates a random 16-byte hex item ID, appends an encrypted ADD operation to `/tmp/lfess-demo/ops.log`, and `list` decrypts and replays the log to reconstruct visible items.

**Output:**
```text
<item-id>
<item-id>    buy oat milk
```

## Architecture

- `cmd/`: Cobra CLI commands for add, update, delete, list, pair, sync, discover, and serve.
- `internal/store`: creates `device.id`, writes encrypted operations to `ops.log`, and imports missing peer operations.
- `internal/engine`: merges logs and replays operations into the current item view.
- `internal/crypto`: manages the local age identity in `key.age` and encrypts or decrypts operation payloads.
- `server/`: exposes `/health`, `/ops`, `/bootstrap/session`, and `/bootstrap/exchange` over HTTP.
- `internal/discovery/mdns`: advertises or discovers peers on the LAN via `_lfess._tcp`.
- `internal/bootstrap`: performs the one-time pair-code exchange and installs shared key material atomically.
- How they connect: the CLI writes local operations, the server exposes encrypted log lines, `sync` pulls missing operations from a peer, and `list` replays the merged log back into readable items.

## Key Design Decisions

**Append-only operations instead of direct state mutation**
- Why: sync becomes a merge of operation history, and local writes stay simple and durable.
- Alternative considered: storing mutable item rows directly.
- Tradeoff: state must be reconstructed by replaying the log, so reads depend on replay instead of a single current record.

**Age-encrypted local storage**
- Why: data at rest stays encrypted while still being easy to move and exchange between devices.
- Alternative considered: plaintext files or a database with built-in encryption.
- Tradeoff: the log is not human-readable, and key management becomes part of the workflow.

**HTTP pull sync with explicit pairing**
- Why: it keeps the sync surface small and makes trust establishment a deliberate step.
- Alternative considered: always-on central server, push-based sync, or automatic trust discovery.
- Tradeoff: sync is manual and LAN-oriented, and the current transport is HTTP only.

## What This Is NOT

- Not a multi-user collaboration system.
- Not a real-time sync engine.
- Not a general-purpose database or notes app.
- Not a replacement for cloud backup or hosted sync.

## Status / Scope

**Current state:** MVP

**What works:**
- Add, update, delete, and list items from the CLI.
- Encrypted local persistence in `~/.lfess` by default, with `--data-dir` support.
- `serve`, `discover`, `pair start`, `pair join`, and `sync` for LAN-based device bootstrap and convergence.
- Deterministic replay of operations, including delete-vs-update convergence.

**Known limitations:**
- Sync is manual and pull-based, not continuous.
- The transport is HTTP only.
- The current model is a simple item list with `content` and `deleted` state.
- There is no UI beyond the CLI.

**Out of scope (intentionally):**
- Multi-user accounts and permissions.
- Real-time sync or WebSocket-based replication.
- Advanced schemas, attachments, or nested data types.
- Centralized backend services.

## Future Work

- [ ] Add richer sync feedback and conflict reporting in the CLI.
- [ ] Add stronger transport security if the threat model expands beyond trusted LANs.
- [ ] Add backup and export tooling for the local data directory.

These are the obvious follow-ons from the current MVP, not a committed roadmap.

## How to Run

```bash
go run . --help
go run . add "buy oat milk"
go run . list
go run . serve
go run . discover
go run . pair start
go run . pair join http://127.0.0.1:7777 --code <pair-code>
go run . sync http://127.0.0.1:7777
```

**Prerequisites:**
- Go 1.24.6 or newer.
- LAN connectivity for discovery and peer sync.

By default, LFESS stores its state in `~/.lfess`. If you want to inspect or reset the data easily, point it at a throwaway directory:

```bash
go run . --data-dir /tmp/lfess-demo add "Ada Lovelace"
go run . --data-dir /tmp/lfess-demo list
```

## Why This Exists

The goal is to keep the sync model small enough that the data model stays obvious: changes are append-only, data stays encrypted locally, and devices converge by replaying the same log instead of negotiating a complex shared database.

---

**Last updated:** 2026-04-09  
**License:** MIT (see [LICENSE](LICENSE))
