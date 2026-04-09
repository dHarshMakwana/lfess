# LFESS — Phase 8 Implementation Plan (CLI)

> Phase 8 goal: complete and validate the CLI surface that operators use for local changes, listing current state, peer bootstrap, sync, and serving.
>
> This document is the authoritative plan for Phase 8 execution. Implementation and validation must follow this plan first, then criteria are marked complete only after direct code-path review and executable evidence.

---

## 1. Objective

Deliver a production-quality CLI for MVP operation with strict guarantees:

- all required commands are available from the root command
- command handlers remain thin orchestration layers
- list output is derived from replayed state, not mutable snapshots
- all command behavior is test-verified and data-dir scoped

---

## 2. Scope

### In scope

- Root command wiring for required Phase 8 commands
- `add`, `update`, `delete` command correctness (append operation flow)
- `list` command implementation and deterministic output contract
- `--data-dir` isolation behavior validation for command execution
- Regression validation for `pair`, `sync`, `serve`, and `discover`

### Out of scope

- New transport/auth features (covered in earlier phases)
- Changes to engine merge/replay conflict semantics
- Changes to encrypted ops log format
- UI or interactive terminal features beyond Cobra command behavior

---

## 3. CLI Contract for Phase 8

Required command set under `lfess`:

- `add <content>`
- `update <id> <content>`
- `delete <id>`
- `list`
- `pair start`
- `pair join`
- `sync <peer-addr>`
- `serve`

`list` output contract for this phase:

- only non-deleted items are printed
- each item is one line in the format `<item_id>\t<content>`
- output order is deterministic (sorted by item id)

---

## 4. Architecture and Boundaries

1. `cmd`
- handles argument parsing, command routing, and user-facing output
- must not contain merge/replay/conflict business logic

2. `internal/store`
- owns append/read/import of encrypted operation log

3. `internal/engine`
- owns replay-derived state and conflict semantics

Phase 8 invariant: command handlers call store/engine helpers and avoid embedding business rules.

---

## 5. Implementation Steps (Execution Order)

1. Define Phase 8 plan (this document).
2. Add `list` command implementation in `cmd` using `store.ReadAll` + `engine.Replay`.
3. Wire `list` into root command registration.
4. Add command tests covering:
- add/update/delete append-path behavior
- list output correctness and deterministic order
- `--data-dir` isolation and no implicit fallback writes
5. Run targeted command tests.
6. Run full suite regression (`go test ./...`).
7. Perform manual runtime behavior checks with real CLI invocations.
8. Fill verification matrix and mark criteria complete only after all evidence is green.

---

## 6. Acceptance Criteria (Phase 8)

### AC-8.1 Command surface completeness

- [x] Root command exposes all required Phase 8 commands including `list`.

### AC-8.2 Local mutation command correctness

- [x] `add` appends exactly one valid `ADD` operation.
- [x] `update` appends exactly one valid `UPDATE` operation.
- [x] `delete` appends exactly one valid `DELETE` operation.

### AC-8.3 List derivation and output contract

- [x] `list` derives state from replayed ops, not separate mutable state.
- [x] `list` prints only non-deleted items.
- [x] `list` output is deterministic and includes item ID + content.

### AC-8.4 Data-dir isolation

- [x] Commands honor `--data-dir` for read/write paths.
- [x] Commands do not implicitly write into default home data-dir when `--data-dir` is set.

### AC-8.5 Sync and error usability regression

- [x] `sync <peer-addr>` continues to reject invalid/non-explicit peer addresses.
- [x] `sync <peer-addr>` returns non-zero with clear error on unreachable peers/import failures.

### AC-8.6 Phase regression safety

- [x] Existing pair/bootstrap, discovery, and serve command behaviors remain passing.
- [x] Full repository test suite passes with Phase 8 changes.

---

## 7. Strict Validation Protocol (No-Assumption Rule)

For every criterion:

1. Review exact implementation path in code.
2. Execute targeted automated tests for that path.
3. Execute at least one manual runtime behavior check where applicable.
4. Mark complete only when all checks are green.
5. If any check fails:
- patch immediately
- rerun the same checks
- keep criterion open until validated

---

## 8. Verification Matrix (To Be Filled During Execution)

| Criterion | Code Path Reviewed | Test Evidence | Manual Behavior Check | Status |
|---|---|---|---|---|
| AC-8.1 | `cmd/root.go` (`root.AddCommand(newListCmd())`), `cmd/list.go` | `TestCommands_AddUpdateDeleteAndList_ReplayConsistent` | `go run . --help` shows `list` along with required command set | ☑ |
| AC-8.2 | `cmd/add.go`, `cmd/update.go`, `cmd/delete.go`, `internal/store/opslog.go` append path | `TestCommands_AddUpdateDeleteAndList_ReplayConsistent` validates 4-op sequence and operation types/item IDs | Manual flow: add/add/update/delete produces expected IDs and operation effects | ☑ |
| AC-8.3 | `cmd/list.go` (`store.NewOpsLog` -> `ReadAll` -> `engine.Replay` -> sorted print) | `TestCommands_AddUpdateDeleteAndList_ReplayConsistent`, `TestListCommand_EmptyStateProducesNoOutput` | Manual list output from populated dir contains only non-deleted updated item; empty dir prints no items | ☑ |
| AC-8.4 | `cmd/root.go` persistent `--data-dir` and command handlers using shared `dataDir` | `TestCommands_DataDirFlagIsolation_NoDefaultDirWrites` | Manual two-dir run: list in second dir is empty while first dir shows item; no default dir ops file created | ☑ |
| AC-8.5 | `cmd/sync.go`, `internal/peersync/peersync.go` (`NormalizePeerAddress`, `PullAndImport`) | `TestSyncCommand_RejectsNonExplicitAddress`, `TestSyncCommand_UnreachablePeerReturnsError` | Manual `sync 127.0.0.1:7777` and `sync http://127.0.0.1:1` both return clear non-zero failures | ☑ |
| AC-8.6 | Regression surfaces in `cmd/pair.go`, `cmd/discover.go`, `cmd/serve.go`, and full repo | Full suite `go test ./...` and race check `go test -race ./cmd` are green; existing pair/discover/serve tests remain passing | Root help and command invocations continue to function after Phase 8 edits | ☑ |

---

## 9. Definition of Done

Phase 8 is complete only when:

- all AC-8.1 through AC-8.6 are marked complete in the matrix
- command-level behavior is verified by both tests and manual checks
- full test suite is green with no unresolved issues
- command architecture boundaries remain intact (thin `cmd`, logic in internal packages)
