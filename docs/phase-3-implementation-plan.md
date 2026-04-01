# LFESS - Phase 3 Implementation Plan (Encryption)

## Scope

Implement age-based encryption primitives in internal/crypto with persisted key.age identity.

Non-goals for this phase:
- No key rotation.
- No multi-recipient encryption.
- No JSON/base64/store warning logic in crypto package.

## Contract

- First run generates key.age; later runs reuse key.age.
- Encrypt accepts plaintext bytes and returns age ciphertext bytes.
- Decrypt accepts age ciphertext bytes and returns plaintext bytes or error.
- Crypto package boundaries remain strict: no JSON marshaling, no base64 processing, no stderr warnings.

## Tasks

- T3.1: Implement path resolution and read-or-create handling for key.age.
- T3.2: Implement secure key persistence with 0600 file mode.
- T3.3: Implement Encrypt(plaintext []byte) ([]byte, error) using age stream encryption.
- T3.4: Implement Decrypt(ciphertext []byte) ([]byte, error) using age stream decryption.
- T3.5: Enforce package boundaries so crypto does not absorb store responsibilities.
- T3.6: Add table-driven tests for key lifecycle, round-trip correctness, wrong-key failure, and empty payload behavior.

## Requirement Mapping

- T3.1 -> R8, R34, R35
- T3.2 -> R8, R34, R35
- T3.3 -> R8, R34
- T3.4 -> R8, R35
- T3.5 -> R24, R34, R35
- T3.6 -> R8, R34, R35, R50

## Acceptance Criteria (STRICT)

- AC3.1 (R8, R34, R35): On first use without key.age, a new age identity file is created.
- AC3.2 (R8): key.age is created with 0600 permissions.
- AC3.3 (R8, R34, R35): On repeated use, key.age is reused and not regenerated.
- AC3.4 (R34): Encrypt returns non-plaintext age ciphertext bytes for non-empty plaintext input.
- AC3.5 (R35): Decrypt(Encrypt(x)) returns x for representative payloads.
- AC3.6 (R35): Decrypt with a different key returns an error.
- AC3.7 (R34, R35): Empty plaintext round-trip succeeds without panics.
- AC3.8 (R24, R34, R35): internal/crypto contains no JSON marshal/unmarshal or base64 encode/decode behavior.

## Task -> AC Mapping

- T3.1 -> AC3.1, AC3.3
- T3.2 -> AC3.2, AC3.3
- T3.3 -> AC3.4, AC3.5, AC3.7
- T3.4 -> AC3.5, AC3.6, AC3.7
- T3.5 -> AC3.8
- T3.6 -> AC3.1, AC3.3, AC3.5, AC3.6, AC3.7

## Definition of Done

Phase 3 is done only when all AC3.x criteria are satisfied and every mapped requirement (R8, R24, R34, R35, R50) is covered by at least one completed task and one passing acceptance check.

## Validation Rules

- If a requirement is not covered, the phase is incomplete.
- If AC is vague, rewrite it before execution continues.
- If any task has no mapped requirement ID, the phase contract is invalid.
- If any mapped AC has no objective verification method, the AC is invalid.
