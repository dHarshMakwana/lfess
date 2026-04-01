# LFESS - Phase 5 Implementation Plan (HTTP Server)

## Scope

Implement the minimal HTTP server in server/ for encrypted log export and peer import.

Non-goals for this phase:
- No authentication/authorization.
- No server-side merge conflict logic.
- No decryption/deduplication/filtering in GET /ops response path.

## Contract

- GET /ops returns full encrypted operation log lines in append order.
- POST /ops accepts peer payload and imports operations through existing store+engine flow.
- GET /health returns 200 status for liveness.
- Server supports safe defaults and configurable host/port.
- Server shuts down gracefully on termination signals.

## Tasks

- T5.1: Define minimal server dependencies for reading encrypted lines and importing peer operations.
- T5.2: Register /ops and /health routes with method handling behavior.
- T5.3: Implement GET /health handler.
- T5.4: Implement GET /ops handler returning raw encrypted lines as JSON.
- T5.5: Implement POST /ops handler with payload parsing, validation, and delegated import.
- T5.6: Implement host/port configuration defaults and overrides (flags and env vars).
- T5.7: Configure server timeouts and graceful shutdown flow.
- T5.8: Add tests for endpoint contracts, error paths, ordering, concurrency, and race safety.

## Requirement Mapping

- T5.1 -> R2, R31, R32, R33
- T5.2 -> R31, R32, R36, R37
- T5.3 -> R45, R48
- T5.4 -> R2, R31, R36, R52
- T5.5 -> R2, R32, R33, R37, R38, R52
- T5.6 -> R5, R42
- T5.7 -> R42, R43
- T5.8 -> R2, R39, R50

## Acceptance Criteria (STRICT)

- AC5.1 (R31, R36): GET /ops returns HTTP 200 and a valid JSON array of ciphertext strings.
- AC5.2 (R31): GET /ops response order exactly matches non-empty line order in ops.log.
- AC5.3 (R31): GET /ops returns [] for missing ops.log and for empty ops.log.
- AC5.4 (R31, R52): GET /ops applies no decryption, deduplication, filtering, or sorting.
- AC5.5 (R32, R33, R37, R38): POST /ops with valid payload imports operations through store+engine flow.
- AC5.6 (R32, R33): POST /ops with invalid JSON or invalid payload returns non-2xx status.
- AC5.7 (R45, R48): GET /health returns HTTP 200 consistently.
- AC5.8 (R42): Default bind address is 127.0.0.1:7777 when no overrides are provided.
- AC5.9 (R5, R42): Host and port override via flags and LFESS_HOST/LFESS_PORT works as configured.
- AC5.10 (R43): ReadTimeout and WriteTimeout are both set to 10 seconds.
- AC5.11 (R42): SIGINT/SIGTERM triggers graceful shutdown via server shutdown path.
- AC5.12 (R2, R39, R50): Concurrent endpoint tests pass with race detector enabled.

## Task -> AC Mapping

- T5.1 -> AC5.4, AC5.5
- T5.2 -> AC5.1, AC5.5, AC5.6, AC5.7
- T5.3 -> AC5.7
- T5.4 -> AC5.1, AC5.2, AC5.3, AC5.4
- T5.5 -> AC5.5, AC5.6
- T5.6 -> AC5.8, AC5.9
- T5.7 -> AC5.10, AC5.11
- T5.8 -> AC5.1, AC5.2, AC5.3, AC5.5, AC5.6, AC5.11, AC5.12

## Definition of Done

Phase 5 is done only when all AC5.x criteria are satisfied and every mapped requirement (R2, R5, R31, R32, R33, R36, R37, R38, R39, R42, R43, R45, R48, R50, R52) is covered by at least one completed task and one passing acceptance check.

## Validation Rules

- If a requirement is not covered, the phase is incomplete.
- If AC is vague, rewrite it before execution continues.
- If any task has no mapped requirement ID, the phase contract is invalid.
- If any mapped AC has no objective test or inspection path, the AC is invalid.
