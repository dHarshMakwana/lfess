# LFESS — Phase 2 Implementation Plan (Append-Only JSON Log)

> Phase 2 — `internal/store`
>
> This document is a *separate* implementation plan for Phase 2 only. It does not change or replace the original plan; it elaborates the exact same Phase 2 design into actionable steps.

---

## Scope

### Goal

Implement the append-only encrypted operation log in `internal/store`.

- Provide safe, append-only persistence for:
  - `ops.log` (newline-delimited base64-encoded age ciphertext, one op per line)
  - `device.id` (persisted device identity)

### Non-goals (MVP)

- No compaction, pruning, or rewriting
- No deduplication (dedup happens only inside `engine.Merge`)
- No sync/HTTP logic (later phases)

---

## Contract (what `internal/store` must do)

- **Append**
  - Input: `model.Operation`
  - JSON encode → age encrypt → base64 encode
  - Append **exactly one** line `"<base64>\n"` to `ops.log`
  - Call `fsync` (`f.Sync()`) **after every write** before returning success

- **Read**
  - Read `ops.log` line-by-line in file order
  - For each line: base64 decode → age decrypt → JSON unmarshal
  - Output: `[]model.Operation` containing **only successfully decrypted/unmarshaled ops**, in the same order they appear in the file

- **Corruption tolerance**
  - Any unreadable line (base64/decrypt/unmarshal failure, or empty line) is **skipped**
  - A warning is printed to **stderr** and must include the **1-based line number**

- **Append-only guarantee**
  - No seeks, truncation, rewriting, temp files, or rename-based updates

---

## Files & responsibilities (Phase 2)

Keep `internal/store` small and focused.

- `internal/store/store.go`
  - Store type / constructor
  - path helpers for `device.id` and `ops.log`
  - ensure `dataDir` exists

- `internal/store/opslog.go`
  - append and read mechanics for `ops.log`

- `internal/store/identity.go`
  - device identity persistence (`device.id`)

- `internal/store/opslog_test.go`
  - append/read tests
  - corruption behavior tests

---

## Implementation steps (do in this order)

### 1) Data directory & secure paths

- Ensure `dataDir` exists: `os.MkdirAll(dataDir, 0700)`
- Centralize filenames:
  - `device.id`
  - `ops.log`
- Ensure created files are `0600`.

### 2) Device identity persistence (`device.id`)

Provide read-or-create semantics:

- If `device.id` exists: read and return trimmed content
- Otherwise:
  - generate a new device ID
  - write to `device.id` with mode `0600`
  - return it

Notes:

- This phase is disk persistence only.
- Operation ID generation remains per original plan: `deviceID + ":" + ulid.Make()` (used by CLI later).

### 3) Append operation (`ops.log`) — strict append-only + fsync

Mechanics must match the original plan:

- `json.Marshal(op)`
- `crypto.Encrypt(plaintext)`
- `base64.StdEncoding.EncodeToString(ciphertext)`
- `os.OpenFile(opsPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)`
- `WriteString(encoded + "\n")`
- `f.Sync()`
- `f.Close()`

Constraints:

- Do not use temp files.
- Do not attempt to “repair” the log.

### 4) Read operations (`ops.log`) — line-by-line decode/decrypt/unmarshal

- If `ops.log` doesn’t exist: return `[]model.Operation{}`
- Use `bufio.Scanner` to read lines
  - Set a larger scanner buffer so normal encrypted lines never overflow in MVP
- For each **1-based** line number:
  - if empty → warn + continue
  - base64 decode
  - decrypt (`crypto.Decrypt`)
  - unmarshal JSON into `model.Operation`
  - append to results
- On any per-line failure:
  - warn to stderr with the line number
  - continue reading

### 5) Tests (table-driven)

Focus on Phase 2 behaviors only.

Must-haves:

- Empty/missing log → empty slice
- Append then read → round-trip equality of op
- Multiple appends preserve order
- Corrupted/un-decryptable line is skipped, later valid lines still load

Nice-to-have:

- Concurrency append stress test:
  - run many goroutines appending
  - then read file lines and validate each line base64-decodes (avoid flaky ordering assumptions)

---

## Edge cases to explicitly handle

- `ops.log` missing (first run)
- `dataDir` missing / not creatable
- empty lines in `ops.log`
- corrupted base64
- wrong key / decryption failure on a single line
- large line length (scanner buffer)

---

## Public API shape (minimal)

Keep surface area small and aligned with later phases:

- `AppendOperation(op model.Operation) error`
- `ReadOperations() ([]model.Operation, error)`
- *(Optional but needed for Phase 5 server contract)* `ReadEncryptedLines() ([]string, error)`
  - returns raw base64 ciphertext lines in append order, exactly as stored

---

## Acceptance Criteria (Phase 2)

- [ ] Each `Append` writes exactly one new line to `ops.log` (line count increases by 1).
- [ ] `ops.log` is append-only: no existing bytes are modified and the file is never rewritten.
- [ ] Each append is followed by `fsync` before returning success.
- [ ] Every `ops.log` line is base64-encoded age ciphertext (no plaintext JSON on disk).
- [ ] Reading returns operations in the same order as lines appear in `ops.log`.
- [ ] The store does **not** deduplicate operations; duplicates are returned as-is.
- [ ] A single corrupted/unreadable line is skipped with a stderr warning that includes the 1-based line number.
- [ ] Data dir is created with 0700; created files are 0600.

---

## Checklist (Phase 2)

- [ ] Add path helpers for `device.id` and `ops.log`
- [ ] Ensure `dataDir` exists (`MkdirAll` 0700)
- [ ] Implement device ID read-or-create persistence
- [ ] Implement append: JSON → age encrypt → base64 → `O_APPEND` write + newline + `Sync`
- [ ] Implement read: scanner + buffer, per-line decode/decrypt/unmarshal, skip+warn on failure
- [ ] Add tests: empty/missing log, round-trip, order preservation, corrupted-line skip
- [ ] Add a concurrency append stress test
