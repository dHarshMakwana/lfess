# LFESS — Phase 4 Implementation Plan (Engine)

> Phase 4 — `internal/engine`
>
> This document is a *separate* implementation plan for Phase 4 only. It does not change or replace the original plan; it elaborates the exact same Phase 4 design into actionable steps.

---

## Scope

### Goal

Implement the **pure** merge + replay logic in `internal/engine`, exactly as defined in the original LFESS plan.

- Deterministically merge two operation logs **without data loss**
- Derive current item state by replaying operations
- Enforce the conflict rule: **delete wins (with ADD guard)**

### Non-goals (MVP)

- No I/O (no files, no network, no stdout/stderr)
- No encryption/base64/JSON (store + crypto handle those)
- No server endpoints (Phase 5)
- No CLI wiring (Phase 6)
- No compaction, pruning, or operation log rewriting

---

## Contract (what `internal/engine` must do)

### `Merge`

- Input: `local []model.Operation`, `remote []model.Operation`
- Behavior:
  - Deduplicate **by `OperationID` only**
  - Preserve local append order
  - Append remote ops not already present in local, preserving remote order
- Output: merged slice containing **no duplicate** OperationIDs

> Correctness note (per original plan): timestamps are **not** used for merge correctness.

### `Replay`

- Input: `ops []model.Operation` that is already deduplicated (i.e., output of `Merge`, or otherwise known-clean)
- Behavior: build the current read model by replaying ops and applying conflict rules.

#### Conflict rule — delete wins (with ADD guard)

For each `ItemID`:

1. If **no `ADD` exists** for that item → ignore all ops for that `ItemID` (orphan `UPDATE`/`DELETE`)
2. If an `ADD` exists and **any `DELETE` exists** → item is deleted
3. Otherwise:
   - Apply `ADD`
   - Apply `UPDATE`s (content)

> Guard behavior preserved from the original plan:
>
> - `DELETE` without an `ADD` is ignored
> - `DELETE` arriving before `ADD` must **not** suppress a later valid `ADD`

- Output: `map[string]model.Item` keyed by `ItemID`

---

## Responsibilities & strict boundaries

To avoid “leaking” logic into CLI/server/store:

- `internal/engine` **must contain all business rules** for:
  - deduplication (Merge)
  - conflict resolution (Replay)
  - rebuild/read model derivation (Replay)

- `internal/engine` must **not**:
  - read/write `ops.log`
  - decrypt/encrypt
  - log warnings
  - perform HTTP requests

---

## Files & responsibilities (Phase 4)

- `internal/engine/engine.go`
  - `Merge(local, remote []model.Operation) []model.Operation`
  - `Replay(ops []model.Operation) map[string]model.Item`
  - any small internal helpers (keep unexported)

- `internal/engine/engine_test.go`
  - all table-driven engine tests described in the original plan

---

## Implementation steps (do in this order)

### 1) Confirm model types and invariants

Before coding the engine itself, confirm `internal/model` provides:

- `model.Operation` with:
  - `OperationID` (string)
  - `ItemID` (string)
  - `DeviceID` (string)
  - `Type` (ADD/UPDATE/DELETE)
  - `Payload` (`map[string]any`)
  - `Timestamp` (`time.Time`)
- `model.Item` with:
  - `ID` (string)
  - `Content` (string)
  - `Deleted` (bool)

Engine assumes those exist and does not “heal” malformed operations.

### 2) Implement `Merge` (dedup by OperationID only)

Algorithm (must match original plan):

1. Create a `map[string]struct{}` seen set from **local** `OperationID`s
2. Create output slice initialized with local ops (preserve order)
3. Iterate remote in order; if not seen, append and mark seen
4. Return output

Edge cases:

- `nil` slices must behave like empty
- Empty `OperationID` values should still participate in the seen-set.
  - If multiple ops have an empty `OperationID`, they will be treated as duplicates.
  - (This is acceptable for MVP and keeps the rule “dedup by ID only”.)

### 3) Implement `Replay` (delete wins + ADD guard)

Requirement: deterministic convergence for a *set* of operations, regardless of arrival order.

Preferred single-pass approach (as in the original plan):

- Maintain a per-item accumulator:
  - `hasAdd bool`
  - `hasDelete bool`
  - `content string`
  - `id string`

While iterating all ops:

- If op.Type == ADD:
  - mark `hasAdd = true`
  - set base `content` from payload (content string)
- If op.Type == UPDATE:
  - record updated content from payload (content string)
  - do not create an item if `hasAdd` is false yet
  - note: updates might arrive before add; the ADD guard will handle final inclusion
- If op.Type == DELETE:
  - mark `hasDelete = true`

Finalization pass (over the accumulator map):

- If `hasAdd == false`: skip item entirely
- Else if `hasDelete == true`: output `model.Item{ID: itemID, Deleted: true}`
- Else: output `model.Item{ID: itemID, Content: content, Deleted: false}`

Notes (do not change the design):

- Replay does not sort the full op list globally.
- Timestamps are not used for correctness.
- If you choose to apply UPDATE ordering by timestamp for display (allowed by the original plan), do it **within one item only** and only when multiple updates exist. If you do not implement that ordering in MVP, keep tests aligned with “last applied update wins given input order”.

### 4) Write table-driven tests

The original plan requires a solid test suite before other layers depend on engine behavior.

Minimum test cases (must match original plan):

- Empty ops → empty state
- Single ADD → one item
- ADD then UPDATE → updated content
- ADD then DELETE → deleted item
- DELETE with no ADD → item absent (orphan delete ignored)
- DELETE then ADD (out of order) → item exists (delete ignored without prior ADD)
- Scenario 1: Device A adds item X, Device B adds item Y, merge → both present
- Scenario 2: Device A deletes item X, Device B updates same item, merge → deleted wins
- Duplicate op IDs in Merge input → merged output has no duplicates, replay unchanged
- All ops already present on both sides → Merge returns same slice
- Stress-ish: many ops across two devices, merged state matches replay of the union

Testing notes:

- Keep tests focused on pure functions.
- Avoid depending on filesystem, timeouts, goroutines, or randomness.
- Use stable timestamps (fixed `time.Time`) when needed.

### 5) Keep the engine API stable

Do not add new exported functions or move responsibilities to other packages.

Public API required by the original plan:

- `Merge(local, remote []model.Operation) []model.Operation`
- `Replay(ops []model.Operation) map[string]model.Item`

---

## Edge cases to explicitly cover

- orphan operations (`UPDATE`/`DELETE` without `ADD`)
- out-of-order arrival (`DELETE → ADD`)
- duplicate operations across devices
- multiple items interleaved in the same ops slice
- empty string IDs (OperationID, ItemID) — document behavior via tests where reasonable

---

## Acceptance Criteria (Phase 4)

- [ ] `internal/engine` contains **pure functions only** (no I/O).
- [ ] `Merge` deduplicates strictly by `OperationID` and preserves local order.
- [ ] `Merge` appends only remote ops whose `OperationID` isn’t already in local.
- [ ] `Replay` ignores orphan `DELETE` and `UPDATE` operations for any `ItemID` with no `ADD`.
- [ ] `Replay` enforces **delete wins** whenever an item has an `ADD` and any `DELETE`.
- [ ] `DELETE` arriving before an `ADD` does not suppress the later `ADD` (ADD guard preserved).
- [ ] Given the same merged set of operations, two devices converge to the same `map[string]Item`.
- [ ] All phase-4 table-driven tests pass.

---

## Checklist (Phase 4)

- [ ] Verify `internal/model` has the required fields and enums used by engine
- [ ] Implement `Merge` (local-first append + OperationID dedup)
- [ ] Implement `Replay` with delete-wins + ADD guard, no I/O
- [ ] Add table-driven tests for all required scenarios
- [ ] Run `go test ./...` and ensure green before moving to Phase 5
