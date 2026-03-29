\
# LFESS — Phase 3 Implementation Plan (Encryption)

> Phase 3 — `internal/crypto`
>
> This document is a *separate* implementation plan for Phase 3 only. It does not change or replace the original plan; it elaborates the exact same Phase 3 design into actionable steps.

---

## Scope

### Goal

Implement age-based encryption in `internal/crypto` exactly as described in the original LFESS plan.

- Provide a small, stable crypto API:
  - `Encrypt(plaintext []byte) ([]byte, error)`
  - `Decrypt(ciphertext []byte) ([]byte, error)`
- Persist an age X25519 identity in `key.age` (mode `0600`).

### Non-goals (MVP)

- No passphrase-protected keys
- No key rotation
- No multi-recipient encryption
- No KMS / OS keychains
- No sync/HTTP logic (later phases)
- No changes to store mechanics (base64/logging/corruption tolerance remain in `internal/store`)

---

## Contract (what `internal/crypto` must do)

### Key management

- On first run, generate an age X25519 identity and persist it to disk (`key.age`, mode `0600`).
- On subsequent runs, load the identity from disk.
- The same key file must support:
  - encryption (derive recipient from the identity’s public key)
  - decryption (use the identity)

> Note: Per the original plan, two devices can decrypt each other’s logs by sharing/copying the same `key.age` file (or by pointing at the same `--data-dir`). Phase 3 is disk persistence + primitives.

### Encrypt

- Input: plaintext bytes
- Output: age ciphertext bytes
- Use stream encryption (`age.Encrypt`) and finalize by closing the writer.

### Decrypt

- Input: age ciphertext bytes
- Output: plaintext bytes
- Use `age.Decrypt` and read all decrypted bytes.

### Responsibility boundaries

- `internal/crypto` must **not**:
  - JSON marshal/unmarshal `model.Operation`
  - base64-encode/decode
  - print warnings to stderr

Those responsibilities belong to `internal/store` per Phase 2 and the original plan.

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

## Files & responsibilities (Phase 3)

Keep `internal/crypto` small and focused.

- `internal/crypto/crypto.go`
  - key load-or-create (read-or-create `key.age`)
  - `Encrypt` / `Decrypt` wrappers
  - path helpers for `key.age`

- `internal/crypto/crypto_test.go`
  - round-trip tests
  - key persistence tests
  - wrong-key failure tests

---

## Implementation steps (do in this order)

### 1) Key file path (`key.age`)

Match the original plan’s file layout:

```
~/.lfess/           (or --data-dir)
└── key.age         # persisted age identity (chmod 600)
```

Implementation detail (repo alignment without design changes):

- Prefer taking a `dataDir` value from existing wiring (store/CLI/server) rather than hard-coding `~/.lfess` inside crypto.
- Keep a single helper for computing the full path to `key.age`.

### 2) Implement identity load-or-create

Provide read-or-create semantics for `key.age`:

- If `key.age` exists:
  - read file contents
  - parse identity with `age.ParseX25519Identity`
  - return the identity
- Otherwise:
  - generate with `age.GenerateX25519Identity`
  - write the identity string to `key.age` with mode `0600`
  - return the identity

Constraints:

- Never rewrite an existing `key.age`.
- Return clear errors when the file exists but is unreadable or malformed.

### 3) Implement `Encrypt(plaintext []byte) ([]byte, error)`

Mechanics must match the original plan:

- Load identity (from `key.age`).
- Derive recipient from identity public key: `identity.Recipient()`.
- Use `age.Encrypt(dst, recipient)`.
- Write plaintext.
- Close the encrypting writer.
- Return ciphertext bytes.

Constraints:

- No base64 here (store does base64 encoding).
- No JSON here (store marshals operations).

### 4) Implement `Decrypt(ciphertext []byte) ([]byte, error)`

Mechanics must match the original plan:

- Load identity (from `key.age`).
- Use `age.Decrypt(src, identity)`.
- Read all decrypted bytes and return.

Constraints:

- Don’t print warnings in crypto.
- Don’t attempt “partial recovery” on bad ciphertext; just return error.

### 5) Tests (table-driven)

Focus on Phase 3 behaviors only.

Must-haves:

- Encrypt/Decrypt round-trip
  - `Decrypt(Encrypt(plaintext)) == plaintext`
- Key persistence
  - first use creates `key.age`
  - second use reuses it (no regeneration)
- Wrong key fails
  - encrypt with key A, decrypt with key B → must return an error

Nice-to-have:

- Empty plaintext round-trip
  - encrypt/decrypt of `[]byte{}` should succeed

---

## Edge cases to explicitly handle

- `key.age` missing (first run)
- `key.age` exists but is unreadable (permissions)
- `key.age` exists but is malformed
- empty plaintext
- ciphertext that is not valid age payload
- decrypt with the wrong key

---

## Public API shape (minimal)

Keep surface area small and aligned with later phases:

- `Encrypt(plaintext []byte) ([]byte, error)`
- `Decrypt(ciphertext []byte) ([]byte, error)`

---

## Acceptance Criteria (Phase 3)

- [ ] On first run, the app generates a new age X25519 identity and writes it to `key.age`.
- [ ] `key.age` is written with mode `0600`.
- [ ] On subsequent runs, the same `key.age` is reused (no regeneration).
- [ ] `Encrypt` produces age ciphertext bytes (plaintext is not stored).
- [ ] `Decrypt(Encrypt(x))` returns `x`.
- [ ] Decrypting with the wrong key returns an error.
- [ ] The crypto layer does not do JSON or base64 responsibilities (those remain in `internal/store`).

---

## Checklist (Phase 3)

- [ ] Add key path helper for `key.age` (consistent with `~/.lfess` / `--data-dir`)
- [ ] Implement identity load-or-create (`age.GenerateX25519Identity`, `age.ParseX25519Identity`)
- [ ] Implement `Encrypt` using `age.Encrypt` and streaming I/O
- [ ] Implement `Decrypt` using `age.Decrypt` and streaming I/O
- [ ] Add unit tests: round-trip, persistence, wrong-key failure (plus empty plaintext nice-to-have)
