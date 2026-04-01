# LFESS - Phase 4 Implementation Plan (Engine)

## Scope

Implement deterministic merge and replay logic in internal/engine as pure functions.

Non-goals for this phase:
- No file I/O.
- No network behavior.
- No encryption/base64/log formatting responsibilities.

## Contract

- Merge combines local and remote operation slices.
- Merge deduplicates by operation_id only.
- Replay derives current item state from deduplicated operations.
- Replay applies delete-wins conflict behavior with ADD guard.
- Replay ignores orphan operations without required ADD context.

## Tasks

- T4.1: Verify engine inputs use required model fields and operation types.
- T4.2: Implement Merge(local, remote) with operation_id deduplication.
- T4.3: Preserve local-first deterministic ordering during merge.
- T4.4: Implement Replay(ops) with ADD, UPDATE, and DELETE handling.
- T4.5: Enforce ADD guard so orphan/out-of-order deletes do not suppress valid later ADD.
- T4.6: Enforce orphan UPDATE/DELETE ignore behavior when ADD is absent.
- T4.7: Add table-driven tests for empty input, duplicates, conflict scenarios, and convergence.
- T4.8: Enforce pure-function boundary in engine package.

## Requirement Mapping

- T4.1 -> R16, R17, R18, R19, R20, R21, R22, R23
- T4.2 -> R2, R3, R28, R29, R38
- T4.3 -> R30, R39
- T4.4 -> R25, R30, R41
- T4.5 -> R12, R41
- T4.6 -> R41
- T4.7 -> R3, R30, R39, R40, R41, R50
- T4.8 -> R27

## Acceptance Criteria (STRICT)

- AC4.1 (R28, R29): Merge output contains no duplicate operation_id values when local and remote inputs overlap.
- AC4.2 (R30): Merge preserves local order and appends only unseen remote operations in remote order.
- AC4.3 (R25, R41): Replay ignores UPDATE operations for an item_id with no ADD.
- AC4.4 (R25, R41): Replay ignores DELETE operations for an item_id with no ADD.
- AC4.5 (R41): Replay marks an item as deleted when ADD exists and a valid DELETE is present.
- AC4.6 (R12, R41): Sequence DELETE then ADD leaves the item present (delete ignored by ADD guard).
- AC4.7 (R40, R39): Scenario 1 test converges so both independently added items appear after merge/replay.
- AC4.8 (R41, R39, R50): Scenario 2 test converges with delete-wins result for the contested item.
- AC4.9 (R27): engine package has no file, network, or console I/O behavior.
- AC4.10 (R30): Replay output is deterministic for identical input slices.

## Task -> AC Mapping

- T4.1 -> AC4.1, AC4.3, AC4.4
- T4.2 -> AC4.1
- T4.3 -> AC4.2
- T4.4 -> AC4.3, AC4.4, AC4.5, AC4.10
- T4.5 -> AC4.6
- T4.6 -> AC4.3, AC4.4
- T4.7 -> AC4.7, AC4.8, AC4.10
- T4.8 -> AC4.9

## Definition of Done

Phase 4 is done only when all AC4.x criteria are satisfied and every mapped requirement (R2, R3, R12, R16, R17, R18, R19, R20, R21, R22, R23, R25, R27, R28, R29, R30, R38, R39, R40, R41, R50) is covered by at least one completed task and one passing acceptance check.

## Validation Rules

- If a requirement is not covered, the phase is incomplete.
- If AC is vague, rewrite it before execution continues.
- If any task has no mapped requirement ID, the phase contract is invalid.
- If any mapped AC cannot be verified with a deterministic test or objective inspection, the AC is invalid.
