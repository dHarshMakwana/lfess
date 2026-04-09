# LFESS — Phase 9 Implementation Plan (Sync Flow End-to-End)

> Phase 9 goal: validate and harden the complete operator sync path end-to-end, from one-time bootstrap through repeat sync convergence, with strict no-assumption verification.
>
> This document is the authoritative execution and validation contract for Phase 9.

---

## 1. Objective

Deliver a production-quality end-to-end sync flow that proves:

- pairing and key bootstrap work as an operator-facing command flow
- independent device operations converge after bidirectional sync
- delete-vs-update conflicts converge deterministically (delete wins)
- repeated no-op syncs are idempotent and do not mutate log history
- peer address validation is strict and explicit (`http://host:port` only)

---

## 2. Scope

### In scope

- New Phase 9 plan and strict acceptance matrix
- CLI-level end-to-end tests for pair + sync + convergence behavior
- Strict peer address validation improvements if current behavior is weaker than contract
- Evidence-based validation (targeted tests, full suite, race checks)

### Out of scope

- New transport protocols or auth schemes
- Changes to log encryption format
- Changes to merge/replay conflict semantics
- UI changes

---

## 3. Phase 9 Flow Contract

Authoritative command-level flow for this phase:

1. Device A serves and starts pairing (`pair start`)
2. Device B joins using pair code (`pair join`) and installs shared key
3. Device A and B make independent local operations
4. Device B syncs from A, then A syncs from B
5. Both devices replay to identical final state
6. Repeated sync rounds import zero operations and do not mutate `ops.log`

Conflict requirement in this flow:

- If A deletes an item while B updates it, both devices must converge to deleted state after sync in both directions.

Peer address requirement in this flow:

- Sync/bootstrap peer addresses must be explicit `http://host:port`
- Missing port, non-http scheme, paths, query strings, and fragments are rejected

---

## 4. Architecture Boundaries

1. `cmd/*`
- command orchestration and user-facing output only

2. `internal/peersync`
- peer address validation and pull/import orchestration

3. `internal/store`, `internal/engine`, `server`
- existing append/import/replay/server behavior remains source of truth

Phase 9 invariant:

- no business-logic duplication in CLI handlers; end-to-end tests must exercise existing internals through command flows.

---

## 5. Implementation Steps (Execution Order)

1. Define this Phase 9 plan document.
2. Audit current behavior against criteria and identify strictness gaps.
3. Implement peer address validation hardening if needed.
4. Add command-level end-to-end tests for:
- bootstrap key installation + sync without manual key copy
- scenario 1 convergence (independent adds)
- scenario 2 convergence (delete vs update)
- idempotent no-op sync with byte-identical logs
5. Run targeted tests for changed packages.
6. Run full regression suite.
7. Run race checks for touched sync/command/server paths.
8. Fill verification matrix and mark criteria complete only after strict evidence.

---

## 6. Acceptance Criteria (Phase 9)

### AC-9.1 Plan-defined execution

- [x] A dedicated Phase 9 plan document exists and is used to drive implementation.

### AC-9.2 Bootstrap to sync operator flow

- [x] `pair start` emits usable session information including pair code.
- [x] `pair join` installs `key.age` with mode `0600`.
- [x] Sync after join works without any manual key copy.

### AC-9.3 Scenario 1 convergence (independent adds)

- [x] After independent adds on A and B, bidirectional sync converges to identical visible state.
- [x] Both devices list both items after convergence.

### AC-9.4 Scenario 2 convergence (delete vs update)

- [x] If A deletes and B updates the same item, both converge to deleted state.
- [x] Deleted item is absent from `list` output on both devices after convergence.

### AC-9.5 Idempotency and no-history-mutation on no-op sync

- [x] After convergence, repeated sync rounds report `imported 0 operations`.
- [x] `ops.log` remains byte-identical across no-op sync rounds.

### AC-9.6 Strict peer address contract

- [x] Peer address validation rejects missing port.
- [x] Peer address validation rejects non-http schemes.
- [x] Peer address validation rejects path/query/fragment payloads.

### AC-9.7 Regression and race safety

- [x] Targeted tests for changed paths pass.
- [x] `go test ./...` passes.
- [x] Race checks for relevant packages pass.

---

## 7. Strict Validation Protocol (No-Assumption Rule)

For every criterion:

1. Review exact code path manually.
2. Execute targeted tests for that code path.
3. Validate observable behavior (command output and/or durable log evidence).
4. Mark complete only when all evidence is green.
5. If any check fails:
- patch immediately
- rerun same checks
- keep criterion open until fully validated

---

## 8. Verification Matrix (Fill During Execution)

| Criterion | Code Path Reviewed | Test Evidence | Behavioral Evidence | Status |
|---|---|---|---|---|
| AC-9.1 | `docs/phase-9-implementation-plan.md` | N/A | Document created first, then implementation executed against this plan | ☑ |
| AC-9.2 | `cmd/pair.go`, `cmd/sync.go`, `cmd/phase9_sync_flow_test.go`, `server/server.go`, `internal/bootstrap/bootstrap.go` | `go test -count=1 -run 'TestPhase9_CLIBootstrapThenSync_StrictConvergenceAndIdempotency|TestPairJoinThenSync_WorksWithoutManualKeyCopy' ./cmd` | Manual CLI run: `pair start` produced pair code, `pair join` returned `bootstrap completed`, sync succeeded without manual key copy | ☑ |
| AC-9.3 | `cmd/add.go`, `cmd/list.go`, `cmd/sync.go`, `internal/store/opslog.go`, `internal/engine/engine.go`, `cmd/phase9_sync_flow_test.go` | `go test -count=1 -run TestPhase9_CLIBootstrapThenSync_StrictConvergenceAndIdempotency ./cmd` | Manual CLI run showed both item IDs in `list` on both devices after bidirectional sync | ☑ |
| AC-9.4 | `cmd/delete.go`, `cmd/update.go`, `cmd/list.go`, `internal/engine/engine.go`, `cmd/phase9_sync_flow_test.go` | `go test -count=1 -run TestPhase9_CLIBootstrapThenSync_StrictConvergenceAndIdempotency ./cmd` | Manual CLI run: post-conflict lists on both devices excluded deleted item and retained only surviving item | ☑ |
| AC-9.5 | `cmd/sync.go`, `internal/peersync/peersync.go`, `internal/store/opslog.go`, `cmd/phase9_sync_flow_test.go` | `go test -count=1 -run TestPhase9_CLIBootstrapThenSync_StrictConvergenceAndIdempotency ./cmd` | Manual CLI run: second sync round returned `imported 0 operations`; SHA-256 of both `ops.log` files remained unchanged | ☑ |
| AC-9.6 | `internal/peersync/peersync.go` (explicit port/userinfo rejection), `cmd/sync.go`, `cmd/pair.go`, `internal/peersync/peersync_test.go`, `cmd/sync_test.go`, `cmd/pair_test.go` | `go test -count=1 -run TestNormalizePeerAddress ./internal/peersync`; `go test -count=1 -run 'TestSyncCommand_RejectsAddressWithoutPort|TestPairJoinCommand_RejectsAddressWithoutPort' ./cmd` | Manual CLI run: `sync http://127.0.0.1` and `pair join http://127.0.0.1 --code a.b` both failed with `missing port` | ☑ |
| AC-9.7 | changed package set (`cmd`, `internal/peersync`) and regression surface (`server`, `internal/store`) | `go test ./...`; `go test -race ./cmd ./internal/peersync ./server ./internal/store` | Full suite and race suite completed with exit code 0 and no failures | ☑ |

---

## 9. Definition of Done

Phase 9 is complete only when:

- AC-9.1 through AC-9.7 are all marked complete
- verification matrix contains concrete evidence entries
- no unresolved test or race failures remain
- no acceptance criterion depends on assumptions or unverified behavior
