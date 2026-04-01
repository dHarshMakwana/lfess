# LFESS - Implementation Plan (Traceable)

## Overview

This master plan implements the LFESS MVP in phases with strict requirement traceability.

Reference requirement source: docs/LFESS-prd.md (R1-R52).

## Global Constraints

- Every task must map to at least one requirement ID.
- No requirement may remain unmapped.
- Tasks must remain atomic and execution-ready.
- This plan must not add scope beyond R1-R52.

## Phase 0 - Project Setup

### Tasks

- T0.1: Initialize the Go module and repository scaffold.
- T0.2: Create the package directory structure.
- T0.3: Add and pin required dependencies.
- T0.4: Create minimal compilable stubs and verify build and tests run.
- T0.5: Add .gitignore and create the baseline commit.
- T0.6: Record MVP scope constraints in project docs (single user, no real-time, no advanced UI).

### Requirement Mapping

- T0.1 -> R4, R42
- T0.2 -> R45, R46
- T0.3 -> R8, R45
- T0.4 -> R45
- T0.5 -> R45
- T0.6 -> R4, R9, R10, R11, R12, R43, R44

### Constraints

- No setup task is valid unless linked to at least one R#.
- Scope-constraint requirements (R9-R12, R43-R44) must be preserved as explicit non-goals.

## Phase 1 - Data Model and Identity

### Tasks

- T1.1: Define Item with id, content, and deleted fields.
- T1.2: Define Operation with operation_id, item_id, type, payload, and timestamp fields.
- T1.3: Define operation types ADD, UPDATE, and DELETE.
- T1.4: Implement device identity persistence in device.id (read-or-create behavior).
- T1.5: Implement operation ID generation as deviceID:ULID.
- T1.6: Document conflict-rule preconditions for replay (delete wins with ADD guard).

### Requirement Mapping

- T1.1 -> R6, R13, R14, R15
- T1.2 -> R19, R20, R21, R22, R23
- T1.3 -> R16, R17, R18
- T1.4 -> R4, R5, R49
- T1.5 -> R19, R49
- T1.6 -> R12, R41

### Constraints

- Model and identity tasks must not include store, server, or CLI behavior.
- Operation ID uniqueness must remain compatible with multi-device simulation.

## Phase 2 - Append-Only JSON Log

### Tasks

- T2.1: Create and secure data directory and store paths.
- T2.2: Append exactly one encrypted operation line per write and call fsync.
- T2.3: Read log lines in file order and return decoded operations.
- T2.4: Skip unreadable lines with stderr warnings that include line numbers.
- T2.5: Expose raw encrypted line reading API for sync export.
- T2.6: Add tests for append-only behavior, ordering, corruption handling, and concurrent append stress.

### Requirement Mapping

- T2.1 -> R24, R46
- T2.2 -> R24, R26, R34, R46
- T2.3 -> R25, R35
- T2.4 -> R25, R35
- T2.5 -> R31, R36
- T2.6 -> R24, R25, R26, R34, R35, R50

### Constraints

- Store tasks must not perform deduplication.
- Append-only guarantees must be preserved across all updates.

## Phase 3 - Encryption

### Tasks

- T3.1: Generate/load key.age identity with secure persistence.
- T3.2: Implement Encrypt for operation bytes before storage.
- T3.3: Implement Decrypt for operation bytes before apply.
- T3.4: Enforce crypto boundary (no JSON/base64/logging responsibilities in crypto package).
- T3.5: Add tests for round-trip, persistence, wrong-key failure, and empty payload.

### Requirement Mapping

- T3.1 -> R8, R34, R35
- T3.2 -> R8, R34
- T3.3 -> R8, R35
- T3.4 -> R24, R34, R35
- T3.5 -> R8, R34, R35, R50

### Constraints

- Crypto tasks must not change operation semantics.
- Encryption behavior must remain minimal MVP scope.

## Phase 4 - Engine

### Tasks

- T4.1: Implement Merge to combine local and remote logs.
- T4.2: Deduplicate merged operations by operation_id only.
- T4.3: Preserve deterministic merge/replay behavior.
- T4.4: Implement Replay with delete-wins conflict logic and ADD guard.
- T4.5: Ignore orphan UPDATE/DELETE operations without ADD.
- T4.6: Add table-driven tests for required merge and conflict scenarios.

### Requirement Mapping

- T4.1 -> R2, R3, R28, R38
- T4.2 -> R29
- T4.3 -> R30, R39
- T4.4 -> R12, R41
- T4.5 -> R41
- T4.6 -> R3, R30, R39, R40, R41, R50

### Constraints

- Engine tasks must remain pure functions without I/O.
- Replay must not call merge implicitly.

## Phase 5 - HTTP Server

### Tasks

- T5.1: Implement GET /ops to return full encrypted log lines in append order.
- T5.2: Implement POST /ops to import peer operations via existing store+engine flow.
- T5.3: Implement GET /health returning HTTP 200.
- T5.4: Implement host/port configuration with defaults and env overrides.
- T5.5: Implement read/write timeouts and graceful shutdown handling.
- T5.6: Add server tests for contract behavior, errors, concurrency, and race safety.

### Requirement Mapping

- T5.1 -> R2, R31, R36, R52
- T5.2 -> R2, R32, R33, R37, R38, R52
- T5.3 -> R45, R48
- T5.4 -> R5, R42
- T5.5 -> R42, R43
- T5.6 -> R2, R39, R50

### Constraints

- Server tasks must not perform business-rule conflict resolution.
- GET /ops must not filter, sort, decrypt, or deduplicate lines.

## Phase 6 - Peer-to-Peer LAN Sync

### Tasks

- T6.1: Support host binding for loopback and LAN usage.
- T6.2: Validate same-network sync using explicit peer addresses.
- T6.3: Keep architecture peer-to-peer with no central coordinator.
- T6.4: Confirm encrypted log exchange remains compatible in LAN mode.

### Requirement Mapping

- T6.1 -> R5, R42
- T6.2 -> R2, R7, R39, R52
- T6.3 -> R42, R43
- T6.4 -> R8, R31, R32

### Constraints

- LAN support must not introduce centralized infrastructure.
- Sync remains pull-based and manual/local.

## Phase 7 - CLI

### Tasks

- T7.1: Implement add command that appends an ADD operation.
- T7.2: Implement update command that appends an UPDATE operation.
- T7.3: Implement delete command that appends a DELETE operation.
- T7.4: Implement list command by replaying log and showing non-deleted items.
- T7.5: Implement sync command that pulls from peer and imports missing operations.
- T7.6: Implement serve command to run HTTP server.
- T7.7: Implement global flags for data-dir, host, and port wiring.
- T7.8: Implement non-zero exit behavior for sync/import failures.

### Requirement Mapping

- T7.1 -> R16, R24, R26, R45
- T7.2 -> R17, R24, R26, R45
- T7.3 -> R18, R24, R26, R45
- T7.4 -> R25, R27, R45
- T7.5 -> R2, R7, R31, R32, R33, R37, R38, R52
- T7.6 -> R2, R42, R45
- T7.7 -> R5, R7, R45, R51
- T7.8 -> R45, R50

### Constraints

- CLI commands must remain thin wrappers over internal logic.
- Local commands must function without network connectivity.

## Phase 8 - End-to-End Sync Validation

### Tasks

- T8.1: Execute Scenario 1 (independent adds) and verify both devices converge.
- T8.2: Execute Scenario 2 (delete vs update) and verify rule-consistent convergence.
- T8.3: Validate independent offline operation before sync and no data loss after merge.
- T8.4: Demonstrate manual/local sync flow across two instances.
- T8.5: Confirm deliverable evidence for CLI, merge, and sync demonstration.

### Requirement Mapping

- T8.1 -> R2, R39, R40, R47
- T8.2 -> R3, R39, R41, R47, R50
- T8.3 -> R1, R49, R50, R51
- T8.4 -> R7, R48, R52
- T8.5 -> R45, R46, R47, R48

### Constraints

- End-to-end tasks are validation tasks and must not redefine behavior.
- Both success scenarios must be demonstrably reproducible.

## Acceptance Criteria Mapping

- AC-1 Local operation logging -> R24, R25, R26, R27, R46; tasks T2.1-T2.6, T7.1-T7.4.
- AC-2 Encryption -> R8, R34, R35; tasks T3.1-T3.5, T2.2-T2.4.
- AC-3 Operation identity and deduplication -> R19, R29, R49; tasks T1.5, T4.2.
- AC-4 Conflict resolution (delete wins with ADD guard) -> R12, R41; tasks T4.4-T4.6.
- AC-5 Sync Scenario 1 -> R40, R39; tasks T8.1.
- AC-6 Sync Scenario 2 -> R41, R39, R50; tasks T8.2.
- AC-7 HTTP API -> R31, R32, R33, R36, R37, R38; tasks T5.1-T5.6.
- AC-8 CLI usability -> R45, R51; tasks T7.1-T7.8.
- AC-9 Offline operation -> R1, R49, R51; tasks T7.1-T7.4, T8.3.
- AC-10 No history mutation -> R24, R26, R50; tasks T2.2, T2.6.
- AC-11 Same-network peer-to-peer sync -> R2, R5, R42, R52; tasks T6.1-T6.4, T7.5.

## Requirement Coverage Matrix

- R1 -> T8.3
- R2 -> T4.1, T5.1, T5.2, T6.2, T7.5, T8.1
- R3 -> T4.1, T4.6, T8.2
- R4 -> T0.1, T0.6, T1.4
- R5 -> T1.4, T5.4, T6.1, T7.7
- R6 -> T1.1
- R7 -> T6.2, T7.5, T8.4
- R8 -> T0.3, T3.1, T3.2, T3.3, T6.4
- R9 -> T0.6
- R10 -> T0.6
- R11 -> T0.6
- R12 -> T0.6, T1.6, T4.4
- R13 -> T1.1
- R14 -> T1.1
- R15 -> T1.1
- R16 -> T1.3, T7.1
- R17 -> T1.3, T7.2
- R18 -> T1.3, T7.3
- R19 -> T1.2, T1.5
- R20 -> T1.2
- R21 -> T1.2
- R22 -> T1.2
- R23 -> T1.2
- R24 -> T2.1, T2.2, T3.4, T7.1, T7.2, T7.3
- R25 -> T2.3, T2.4, T7.4
- R26 -> T2.2, T2.6, T7.1, T7.2, T7.3
- R27 -> T7.4
- R28 -> T4.1
- R29 -> T4.2
- R30 -> T4.3, T4.6
- R31 -> T2.5, T5.1, T6.4, T7.5
- R32 -> T5.2, T6.4, T7.5
- R33 -> T5.2, T7.5
- R34 -> T2.2, T3.1, T3.2, T3.4
- R35 -> T2.3, T2.4, T3.1, T3.3, T3.4
- R36 -> T2.5, T5.1
- R37 -> T5.2, T7.5
- R38 -> T4.1, T5.2, T7.5
- R39 -> T4.3, T4.6, T5.6, T6.2, T8.1, T8.2
- R40 -> T4.6, T8.1
- R41 -> T1.6, T4.4, T4.5, T4.6, T8.2
- R42 -> T0.1, T5.4, T5.5, T6.1, T6.3, T7.6
- R43 -> T0.6, T5.5, T6.3
- R44 -> T0.6
- R45 -> T0.2, T0.3, T0.4, T0.5, T5.3, T7.1, T7.2, T7.3, T7.4, T7.6, T7.7, T7.8, T8.5
- R46 -> T0.2, T2.1, T2.2, T8.5
- R47 -> T8.1, T8.2, T8.5
- R48 -> T5.3, T8.4, T8.5
- R49 -> T1.4, T1.5, T8.3
- R50 -> T2.6, T3.5, T4.6, T5.6, T7.8, T8.2, T8.3
- R51 -> T7.7, T8.3
- R52 -> T5.1, T5.2, T6.2, T7.5, T8.4

## Master Validation Rules

- If any task lacks Requirement Mapping, the plan is invalid.
- If any R# is missing from the Requirement Coverage Matrix, the plan is incomplete.
- If any AC cannot be traced to both requirement IDs and task IDs, acceptance evidence is incomplete.
- If any task description is non-atomic or non-executable, it must be rewritten before execution.
