# LFESS Core Architecture Validation Checklist (Hardened)

## Goal
Validate with strict, measurable conditions that:
- Merge is deterministic and convergent
- Replay is correct and deterministic
- Sync is idempotent under retries and races
- No silent data loss occurs under realistic failures

---

## Global Validation Metrics (apply to all tests)
For each test capture these values before and after every sync/restart:
- `state_hash`: SHA-256 of canonical replay output (items sorted by item ID, deterministic JSON)
- `op_set_hash`: SHA-256 of sorted unique `OperationID` values
- `unique_op_count`: distinct `OperationID` count in persisted log
- `log_line_count`: physical line count in `ops.log`
- `log_bytes`: full file bytes of `ops.log` (for atomicity checks)
- `imported`: server-reported imported operation count per sync request

If a test says "same state", require equal `state_hash` and equal `op_set_hash`.

---

## Test 1: Baseline Divergence + Double Sync

### Steps
1. Device A appends `ADD(Task A)` as `A1`.
2. Device B appends `ADD(Task B)` as `B1`.
3. Sync A <- B and B <- A.
4. Repeat the same bidirectional sync once more.

### Expected Output
- Both devices contain Task A and Task B.

### Strict Result Conditions
- `state_hash(A) == state_hash(B)`.
- `op_set_hash(A) == op_set_hash(B)`.
- `unique_op_count` on each device is exactly 2.
- Second sync round returns `imported = 0` both directions.

---

## Test 2: Sequential Duplicate Import Idempotency

### Steps
1. Prepare payload with exactly one valid operation `O1`.
2. Import the same payload 10 times on the same device.

### Expected Output
- Item appears exactly once in replayed state.

### Strict Result Conditions
- First import returns `imported = 1`; next 9 imports return `imported = 0`.
- `unique_op_count == 1` and `log_line_count == 1`.
- `state_hash` remains unchanged across 10 process restarts.

---

## Test 3: Out-of-Order Update Before Add

### Steps
1. Create `ADD(itemX, v1)` as `O1` with timestamp `T1`.
2. Create `UPDATE(itemX, v2)` as `O2` with timestamp `T2`, where `T2 > T1`.
3. Deliver `O2` first, then `O1`.
4. Replay state.

### Expected Output
- `itemX` exists with content `v2`.

### Strict Result Conditions
- Final content is exactly `v2`.
- Item is not marked deleted.
- Replaying same log multiple times yields identical `state_hash`.

---

## Test 4: Delete vs Update Conflict (Two Perspectives)

### Steps
1. Start both devices with shared `ADD(itemX)` as `O0`.
2. Device A appends `DELETE(itemX)` as `O1`.
3. Device B appends `UPDATE(itemX, v2)` as `O2`.
4. Run A <- B and B <- A.

### Expected Output
- Both devices show `itemX` as deleted.

### Strict Result Conditions
- `state_hash(A) == state_hash(B)` and deleted flag is true for `itemX` on both.
- `op_set_hash(A) == op_set_hash(B)`.
- Second sync round returns `imported = 0` both directions.

---

## Test 5: Same Op-Set, Different Merge Order

### Steps
1. Use two operations for the same item: `Odel = DELETE(itemY)` and `Oadd = ADD(itemY, v1)`.
2. Node X merges in order: local `[Odel]`, remote `[Oadd]`.
3. Node Y merges in order: local `[Oadd]`, remote `[Odel]`.
4. Replay on both nodes.

### Expected Output
- Both nodes must converge to identical final state.

### Strict Result Conditions
- `op_set_hash(X) == op_set_hash(Y)`.
- `state_hash(X) == state_hash(Y)`.
- Any mismatch is a hard failure (non-deterministic merge+replay semantics).

---

## Test 6: Equal Timestamp Concurrent Updates

### Steps
1. Start with `ADD(itemZ, base)` as `O0`.
2. Create `UPDATE(itemZ, A)` as `O1` and `UPDATE(itemZ, B)` as `O2` with identical timestamps.
3. Sync in both directions with varying import order.

### Expected Output
- One deterministic winner is selected consistently.

### Strict Result Conditions
- Across 100 reruns with different import orders/restarts, `state_hash` is identical.
- Winner does not depend on request timing.

---

## Test 7: OperationID Collision With Different Payload

### Steps
1. Craft two valid operations with identical `OperationID` but different payload or different `ItemID`.
2. Import both into same device.

### Expected Output
- Collision is rejected explicitly.

### Strict Result Conditions
- Import fails with deterministic collision error.
- `log_bytes` unchanged from pre-import snapshot.
- `state_hash` unchanged.

---

## Test 8: Duplicate Sync Loop Stability

### Steps
1. Device A starts with 50 unique operations.
2. Device B starts with a disjoint set of 50 unique operations.
3. Run 20 full bidirectional sync rounds.

### Expected Output
- Converges once; remains stable afterward.

### Strict Result Conditions
- Final `unique_op_count` is exactly 100 on both devices.
- Rounds 2 through 20 return `imported = 0` both directions.
- `state_hash` and `op_set_hash` remain unchanged after round 1.

---

## Test 9: Concurrent Duplicate Import Race

### Steps
1. Prepare payload with one new valid operation `Orace`.
2. Send same payload to one device using 20 concurrent import requests.
3. Replay state.

### Expected Output
- Exactly one durable append for `Orace`.

### Strict Result Conditions
- `unique_op_count` increases by exactly 1.
- `log_line_count` increases by exactly 1.
- No extra duplicate lines for same `OperationID`.

---

## Test 10: Partial Batch Failure Atomicity

### Steps
1. Build one import payload: valid `O1`, valid `O2`, invalid `O3`, valid `O4`.
2. Submit as a single request.

### Expected Output
- Entire batch fails; nothing is appended.

### Strict Result Conditions
- Request fails with validation/decrypt error.
- `imported = 0`.
- `log_bytes` is byte-identical to pre-import snapshot.
- None of `O1`, `O2`, `O4` appears in persisted log.

---

## Test 11: Wrong Encryption Key Atomicity

### Steps
1. Device A and Device B use different encryption keys.
2. Device B sends encrypted payload to Device A.

### Expected Output
- Sync fails cleanly with no writes.

### Strict Result Conditions
- Deterministic decrypt/import error is returned.
- `log_bytes` unchanged.
- `state_hash` unchanged.

---

## Test 12: Crash/Truncation During Write

### Steps
1. Append N valid operations.
2. Simulate crash artifact by truncating the last line mid-ciphertext.
3. Restart and replay.

### Expected Output
- Valid prefix is recovered; truncated tail is ignored.

### Strict Result Conditions
- First `N-1` operations replay exactly.
- No earlier operation is lost.
- Parser skips only corrupted tail line.

---

## Test 13: Corruption Matrix Recovery

### Steps
1. Inject each corruption class into different lines:
  - non-base64 text
  - base64 that decrypts to error
  - decryptable non-JSON plaintext
  - JSON operation missing required fields
2. Restart and replay.

### Expected Output
- All corrupted lines are ignored.

### Strict Result Conditions
- Recovered operation IDs equal exactly the known-valid baseline set.
- No crash.
- `state_hash` equals baseline hash from valid-only log.

---

## Test 14: Large Mixed Workload + Permutation Invariance

### Steps
1. Generate 10,000 operations with mixed ADD/UPDATE/DELETE, duplicates, and out-of-order delivery.
2. Partition same unique op-set across two devices with different local/remote ordering.
3. Run sync to completion and replay on both.

### Expected Output
- Both devices converge to the exact same final state.

### Strict Result Conditions
- `op_set_hash(A) == op_set_hash(B)`.
- `state_hash(A) == state_hash(B)`.
- Additional sync round returns `imported = 0` both directions.

---

## Final System Invariants (MUST HOLD)

- [ ] Same unique operation set always yields same `state_hash`.
- [ ] Sync retries and loops do not increase `unique_op_count` after convergence.
- [ ] Failed imports are atomic (`log_bytes` unchanged).
- [ ] No operation is silently dropped by ID collision or malformed replay path.
- [ ] Corruption in one line does not erase valid neighboring operations.
- [ ] Delete/update conflicts resolve consistently across all devices.

---

## Pass Criteria

If all tests pass with strict conditions above, merge + replay + sync behavior is deterministic, idempotent, and highly resistant to realistic distributed failure modes.