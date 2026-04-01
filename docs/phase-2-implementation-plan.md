# LFESS - Phase 2 Implementation Plan (Append-Only JSON Log)

## Scope

Implement append-only encrypted operation storage in internal/store for ops.log and device.id.

Non-goals for this phase:
- No deduplication logic.
- No sync or HTTP behavior.
- No log compaction or rewrite behavior.

## Contract

- Append operation path: JSON encode -> age encrypt -> base64 encode -> append one line to ops.log -> fsync.
- Read operation path: read each line in order -> base64 decode -> decrypt -> unmarshal -> return valid operations in order.
- Corruption tolerance: unreadable lines are skipped with stderr warnings that include 1-based line number.
- Append-only guarantee: no seek/truncate/rename rewrite flow.

## Tasks

- T2.1: Ensure data directory creation and permissions (0700 directory, 0600 files).
- T2.2: Implement canonical path helpers for device.id and ops.log.
- T2.3: Implement AppendOperation using strict append-only write semantics and fsync.
- T2.4: Implement ReadOperations that preserves file order for valid lines.
- T2.5: Implement line-level skip-and-warn handling for empty/corrupt/undecryptable lines.
- T2.6: Implement ReadEncryptedLines to return raw non-empty ciphertext lines in append order.
- T2.7: Implement device identity read-or-create persistence in device.id.
- T2.8: Add table-driven tests for append, read, corruption tolerance, and concurrent append stress.

## Requirement Mapping

- T2.1 -> R24, R46
- T2.2 -> R24, R46
- T2.3 -> R24, R26, R34, R46
- T2.4 -> R25, R35
- T2.5 -> R25, R35
- T2.6 -> R31, R36, R46
- T2.7 -> R49
- T2.8 -> R24, R25, R26, R34, R35, R50

## Acceptance Criteria (STRICT)

- AC2.1 (R24, R26, R46): Executing one append call increases non-empty line count in ops.log by exactly one.
- AC2.2 (R24, R46): Append writes one newline-terminated base64 string per call and does not modify existing bytes in earlier lines.
- AC2.3 (R24): Append success is returned only after fsync completes without error.
- AC2.4 (R34): No plaintext JSON operation body appears on disk in ops.log after append.
- AC2.5 (R25, R35): ReadOperations returns valid operations in the same order as their corresponding lines in ops.log.
- AC2.6 (R24, R25): Reading when ops.log is missing returns an empty slice and no fatal error.
- AC2.7 (R25, R35): A corrupted line is skipped, stderr contains its 1-based line number, and later valid lines are still processed.
- AC2.8 (R25): ReadOperations does not deduplicate repeated operations with distinct lines.
- AC2.9 (R31, R36, R46): ReadEncryptedLines returns exactly the same non-empty ciphertext lines as ops.log in the same order.
- AC2.10 (R24, R46): Data directory permissions are 0700 and new store-managed files are created with 0600 permissions.
- AC2.11 (R50): Under concurrent append stress, resulting non-empty lines remain parseable as base64 records.

## Task -> AC Mapping

- T2.1 -> AC2.10
- T2.2 -> AC2.10
- T2.3 -> AC2.1, AC2.2, AC2.3, AC2.4
- T2.4 -> AC2.5, AC2.6, AC2.8
- T2.5 -> AC2.7
- T2.6 -> AC2.9
- T2.7 -> AC2.10
- T2.8 -> AC2.1, AC2.5, AC2.7, AC2.11

## Definition of Done

Phase 2 is done only when all AC2.x criteria are satisfied and every mapped requirement (R24, R25, R26, R31, R34, R35, R36, R46, R49, R50) is covered by at least one completed task and one passing acceptance check.

## Validation Rules

- If a requirement is not covered, the phase is incomplete.
- If AC is vague, rewrite it before execution continues.
- If any task has no mapped requirement ID, the phase contract is invalid.
- If any mapped AC has no reproducible verification method, the AC is invalid.
