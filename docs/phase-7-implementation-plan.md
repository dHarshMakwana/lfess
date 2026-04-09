# LFESS — Phase 7 Implementation Plan (One-Time Authenticated LAN Bootstrap)

> Phase 7 goal: remove normal-path manual key copying by adding a one-time, authenticated bootstrap flow over LAN.
>
> This document is the authoritative execution plan for Phase 7. Implementation must follow this plan first, and validation must be strict: no criterion is considered complete without direct code-path review and executable evidence.

---

## 1. Objective

Deliver one-time authenticated onboarding so a new device can securely obtain `key.age` from a trusted device over LAN, then immediately participate in normal sync.

Required outcomes:

- bootstrap sessions are single-use and time-bounded
- bootstrap secrets are memory-only on the trusted device
- key material is transmitted only as age-encrypted payload to requester ephemeral recipient
- post-bootstrap sync works with no manual key copying

---

## 2. Scope

### In scope

- New bootstrap manager in `internal/bootstrap`
- New HTTP endpoints in `server` for bootstrap start and exchange
- New CLI commands in `cmd`:
  - `lfess pair start --ttl <duration>`
  - `lfess pair join <peer-addr> --code <pair-code>`
- Atomic key installation (`0600`) on joining device
- Strict acceptance tests and validation evidence

### Out of scope

- Long-term multi-user auth model
- TLS/certificate PKI rollout
- Changes to core `/ops` sync protocol semantics
- Changes to merge/replay conflict logic

---

## 3. Architecture and Boundaries

### 3.1 Component ownership

1. `internal/bootstrap`
- owns session lifecycle, code verification, expiry, and single-use state
- owns attempt limiting (rate limiting) semantics
- owns payload encryption to requester ephemeral age recipient

2. `server`
- thin transport shell over `internal/bootstrap`
- validates request shape and translates manager errors to HTTP status codes
- must not contain bootstrap business logic

3. `cmd`
- `pair start` invokes local server bootstrap-start endpoint
- `pair join` invokes peer bootstrap-exchange endpoint, decrypts payload, writes `key.age` atomically
- command layer remains orchestration only

4. `internal/crypto`
- reused for key path constants and existing age primitives
- no weakening of existing key semantics

### 3.2 Session model

- Session fields:
  - `session_id` (random, globally unique)
  - `code_proof_secret` (random secret used to verify proof)
  - `expires_at`
  - `used`
  - `attempt_count`
- Session state is in-memory only in the running server process.
- Session is invalid when any of the following is true:
  - not found
  - expired
  - already used
  - exceeded failed-attempt limit

### 3.3 Pair code model

Pair code format:

- `<session_id>.<code_secret>`

Start command prints this one-time code (user shares it out-of-band).

Join command derives proof:

- `proof = HMAC-SHA256(key=code_secret, message=session_id)` (hex-encoded)

Server validates proof in constant time against session secret-derived expected value.

### 3.4 Bootstrap payload

- Trusted device reads local `key.age` bytes.
- Trusted device encrypts those bytes to joiner-provided ephemeral recipient (`age.X25519Recipient`).
- Response payload contains ciphertext as base64.
- Joiner decrypts with ephemeral private key and installs `key.age` atomically with mode `0600`.

---

## 4. HTTP Contract

All endpoints are under existing server process.

### 4.1 `POST /bootstrap/session`

Purpose: start a one-time bootstrap session.

Request body:

```json
{
  "ttl_seconds": 120
}
```

Response body (`200`):

```json
{
  "session_id": "...",
  "pair_code": "<session_id>.<code_secret>",
  "expires_at": "RFC3339 timestamp"
}
```

Error behavior:

- invalid TTL/request shape -> `400`
- internal failures -> `500`

### 4.2 `POST /bootstrap/exchange`

Purpose: exchange pairing proof for encrypted key payload.

Request body:

```json
{
  "session_id": "...",
  "proof": "hex-hmac",
  "ephemeral_recipient": "age1..."
}
```

Response body (`200`):

```json
{
  "payload": "base64-age-ciphertext"
}
```

Error behavior:

- malformed request -> `400`
- invalid/expired/used/rate-limited session -> `401` or `429` as applicable
- internal failures -> `500`

---

## 5. CLI Contract

### 5.1 `lfess pair start --ttl 2m`

- calls local server `POST /bootstrap/session`
- prints one-time code and expiry
- requires running local server reachable via configurable local bootstrap address

### 5.2 `lfess pair join <peer-addr> --code <session_id.code_secret>`

- validates `peer-addr` format (explicit `http://host:port`)
- parses code into `session_id` + `code_secret`
- creates ephemeral age identity and recipient
- computes proof and calls peer `POST /bootstrap/exchange`
- decrypts payload using ephemeral identity
- writes `key.age` atomically (`0600`)
- returns success only when key write completes

---

## 6. Implementation Steps (Execution Order)

1. Create `internal/bootstrap` with manager, models, and error taxonomy.
2. Implement session creation, expiry pruning, proof verification, single-use marking, and failed-attempt limiting.
3. Add payload encryption helper for key material export to ephemeral recipient.
4. Add server wiring and handlers for `/bootstrap/session` and `/bootstrap/exchange`.
5. Add `cmd/pair.go` with `pair`, `pair start`, `pair join`.
6. Register pair command in `cmd/root.go`.
7. Add unit tests for bootstrap manager and helpers.
8. Add server endpoint tests for success and all rejection paths.
9. Add CLI tests for `pair start` and `pair join` including key install mode and failure paths.
10. Run full and targeted race validation.

---

## 7. Acceptance Criteria (Phase 7)

### AC-7.1 Session lifecycle

- [x] Trusted device can start bootstrap session with one-time code and expiry.
- [x] Session state is memory-only and auto-expires.
- [x] Used session cannot be reused.

### AC-7.2 Authentication and abuse control

- [x] Exchange requires valid proof derived from pairing code.
- [x] Invalid proof is rejected with explicit error.
- [x] Failed attempts are rate-limited (bounded attempts per session).

### AC-7.3 Secure key transport

- [x] Trusted device returns key payload only when auth checks pass.
- [x] Key payload is encrypted to requester ephemeral recipient.
- [x] Joiner can decrypt payload only with matching ephemeral private key.

### AC-7.4 Safe key installation

- [x] Join command writes `key.age` atomically.
- [x] Installed key mode is exactly `0600`.
- [x] Invalid payload/decrypt/write errors fail fast with non-zero exit.

### AC-7.5 End-to-end bootstrap usability

- [x] `pair start` and `pair join` command flow succeeds across two data dirs/devices.
- [x] Post-bootstrap sync succeeds without manual key copy.
- [x] Existing `/ops` sync behavior remains unchanged.

### AC-7.6 Invariant preservation

- [x] Business logic remains in internal packages; handlers/commands stay thin.
- [x] No regression in engine/store/server phase contracts.
- [x] Concurrency safety validated under race detector for changed surfaces.

---

## 8. Strict Validation Protocol (No-Assumption Rule)

For each acceptance criterion:

1. Inspect exact implementation path in code (line-by-line reasoning).
2. Execute targeted tests hitting that path.
3. Manually confirm runtime outputs/error codes match contract.
4. Mark criterion complete only if all three checks pass.
5. If any check fails:
- patch immediately
- rerun the same checks
- do not mark complete until verified green

No criterion may be marked complete from inference alone.

---

## 9. Verification Matrix (to fill during execution)

| Criterion | Code Path Reviewed | Test Evidence | Runtime/Manual Check | Status |
|---|---|---|---|---|
| AC-7.1 | `internal/bootstrap/bootstrap.go` (`StartSession`, `Exchange`) | `TestStartSession_ReturnsPairCodeAndExpiry`, `TestExchange_ExpiredSessionRejected`, `TestExchange_SuccessIsSingleUseAndEncryptsForRecipient`, `TestBootstrapExchange_ExpiredSessionRejected` | Verified `POST /bootstrap/session` returns `session_id`, `pair_code`, `expires_at`; replay after successful exchange returns unauthorized | ☑ |
| AC-7.2 | `internal/bootstrap/bootstrap.go` (`ComputeProof`, constant-time proof compare, attempt counter) and `server/server.go` (401/429 mapping) | `TestExchange_InvalidProofRateLimited`, `TestBootstrapExchange_RateLimitedAfterFailedProofs` | Confirmed invalid proofs return unauthorized until limit; final invalid attempt returns too-many-requests | ☑ |
| AC-7.3 | `internal/bootstrap/bootstrap.go` (`readLocalKeyPayload`, `encryptToRecipient`) and `cmd/pair.go` decrypt path | `TestExchange_SuccessIsSingleUseAndEncryptsForRecipient`, `TestBootstrapSessionAndExchange_Success`, `TestPairJoinCommand_SuccessInstallsKey0600` | Verified payload only issued on valid proof; payload decrypts only with matching ephemeral private key | ☑ |
| AC-7.4 | `internal/bootstrap/bootstrap.go` (`InstallKeyAtomically`) and `cmd/pair.go` join flow | `TestInstallKeyAtomically_Writes0600AndValidatesPayload`, `TestPairJoinCommand_SuccessInstallsKey0600`, `TestPairJoinCommand_InvalidCodeFails` | Verified atomic temp-write + rename, enforced mode `0600`, and fail-fast command errors on invalid pairing inputs | ☑ |
| AC-7.5 | `cmd/pair.go`, `cmd/sync.go`, `server/server.go` bootstrap + `/ops` | `TestPairStartCommand_Success`, `TestPairJoinCommand_SuccessInstallsKey0600`, `TestPairJoinThenSync_WorksWithoutManualKeyCopy`, existing sync tests | Verified pair start/join across separate data dirs and successful sync without manual key copy | ☑ |
| AC-7.6 | `internal/bootstrap` owns bootstrap logic; `server` and `cmd` remain orchestration-only | `go test ./...` and race runs on changed surfaces | Verified no regression in full suite and race-safe results for `./cmd ./internal/bootstrap ./server ./internal/store` | ☑ |

---

## 10. Definition of Done

Phase 7 is complete only when:

- all AC-7.1 through AC-7.6 are verified and marked complete in the matrix
- full relevant tests and race checks are green
- no unresolved bootstrap defects remain
- onboarding works without manual key copy in validated end-to-end flow
