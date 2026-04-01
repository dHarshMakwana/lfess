package engine_test

import (
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/dHarshMakwana/lfess/internal/engine"
	"github.com/dHarshMakwana/lfess/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── helpers ──────────────────────────────────────────────────────────────────

var epoch = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

func addOp(opID, itemID, content string, t time.Time) model.Operation {
	return model.Operation{
		OperationID: opID,
		ItemID:      itemID,
		DeviceID:    "dev",
		Type:        model.OperationAdd,
		Payload:     map[string]any{"content": content},
		Timestamp:   t,
	}
}

func updateOp(opID, itemID, content string, t time.Time) model.Operation {
	return model.Operation{
		OperationID: opID,
		ItemID:      itemID,
		DeviceID:    "dev",
		Type:        model.OperationUpdate,
		Payload:     map[string]any{"content": content},
		Timestamp:   t,
	}
}

func deleteOp(opID, itemID string, t time.Time) model.Operation {
	return model.Operation{
		OperationID: opID,
		ItemID:      itemID,
		DeviceID:    "dev",
		Type:        model.OperationDelete,
		Payload:     map[string]any{"deleted": true},
		Timestamp:   t,
	}
}

// sortedIDs returns sorted keys from the map for deterministic assertions.
func sortedIDs(m map[string]model.Item) []string {
	ids := make([]string, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// ─── Replay tests ─────────────────────────────────────────────────────────────

func TestReplay_EmptyLog(t *testing.T) {
	state := engine.Replay(nil)
	assert.Empty(t, state, "empty op log must produce empty state")
}

func TestReplay_SingleADD(t *testing.T) {
	ops := []model.Operation{
		addOp("op1", "item1", "hello", epoch),
	}
	state := engine.Replay(ops)
	require.Len(t, state, 1)
	item := state["item1"]
	assert.Equal(t, "item1", item.ID)
	assert.Equal(t, "hello", item.Content)
	assert.False(t, item.Deleted)
}

func TestReplay_AddThenUpdate(t *testing.T) {
	ops := []model.Operation{
		addOp("op1", "item1", "original", epoch),
		updateOp("op2", "item1", "updated", epoch.Add(time.Second)),
	}
	state := engine.Replay(ops)
	require.Len(t, state, 1)
	assert.Equal(t, "updated", state["item1"].Content)
	assert.False(t, state["item1"].Deleted)
}

func TestReplay_AddThenDelete(t *testing.T) {
	// AC-4: ADD then DELETE → item is deleted
	ops := []model.Operation{
		addOp("op1", "item1", "hello", epoch),
		deleteOp("op2", "item1", epoch.Add(time.Second)),
	}
	state := engine.Replay(ops)
	require.Len(t, state, 1)
	item := state["item1"]
	assert.True(t, item.Deleted)
	assert.Equal(t, "item1", item.ID)
}

func TestReplay_DeleteWithNoADD_OrphanIgnored(t *testing.T) {
	// AC-4: DELETE op with no ADD for the same ItemID is silently ignored
	ops := []model.Operation{
		deleteOp("op1", "item1", epoch),
	}
	state := engine.Replay(ops)
	assert.Empty(t, state, "orphan DELETE must not produce any item")
	assert.NotContains(t, state, "item1")
}

func TestReplay_UpdateWithNoADD_OrphanIgnored(t *testing.T) {
	// Orphan UPDATE (no ADD) must not appear in state
	ops := []model.Operation{
		updateOp("op1", "item1", "ghost", epoch),
	}
	state := engine.Replay(ops)
	assert.Empty(t, state)
}

func TestReplay_DeleteBeforeADD_OutOfOrder_ItemExists(t *testing.T) {
	// AC-4: A DELETE that arrives before an ADD (out-of-order import) does not
	// suppress the ADD — the item exists after merge.
	ops := []model.Operation{
		deleteOp("op1", "item1", epoch),
		addOp("op2", "item1", "late add", epoch.Add(time.Second)),
	}
	state := engine.Replay(ops)
	// DELETE has no effect because there is an ADD that arrived later;
	// the ADD guard requires an ADD to exist — and a DELETE wins only when
	// an ADD also exists. Here, both exist, so delete-wins applies.
	// Re-reading the spec: "DELETE that arrives before an ADD does not suppress the ADD"
	// means the item still exists.  The spec says delete-wins only when ADD is present,
	// but specifically calls out that out-of-order DELETE before ADD should NOT suppress.
	// Per the plan: "DELETE then ADD (out of order) → item exists"
	require.Len(t, state, 1)
	assert.False(t, state["item1"].Deleted, "out-of-order DELETE before ADD: item must exist")
	assert.Equal(t, "late add", state["item1"].Content)
}

func TestReplay_DeleteWins_TimestampIrrelevant(t *testing.T) {
	// AC-4: The conflict rule applies regardless of which device's op has the
	// earlier timestamp — timestamps do not influence correctness.
	//
	// Device B's UPDATE has a timestamp BEFORE Device A's DELETE, but delete
	// must still win.
	ops := []model.Operation{
		addOp("op-add", "item1", "original", epoch),
		updateOp("op-update", "item1", "new content", epoch.Add(2*time.Second)), // later ts
		deleteOp("op-delete", "item1", epoch.Add(time.Second)),                  // earlier ts than UPDATE
	}
	state := engine.Replay(ops)
	require.Len(t, state, 1)
	assert.True(t, state["item1"].Deleted, "delete must win regardless of timestamp order")
}

func TestReplay_UpdateOrdering_LatestTimestampWins(t *testing.T) {
	// Multiple UPDATEs — the one with the latest Timestamp should win.
	ops := []model.Operation{
		addOp("op1", "item1", "v0", epoch),
		updateOp("op2", "item1", "v2", epoch.Add(2*time.Second)),
		updateOp("op3", "item1", "v1", epoch.Add(time.Second)),
	}
	state := engine.Replay(ops)
	require.Len(t, state, 1)
	assert.Equal(t, "v2", state["item1"].Content)
}

func TestReplay_RuleEnforcedInReplay_NotMerge(t *testing.T) {
	// AC-4: The rule is enforced entirely inside Replay; Merge applies no
	// conflict logic. Verify by running Merge then Replay and ensuring
	// delete-wins is still applied.
	local := []model.Operation{
		addOp("op-add", "item1", "original", epoch),
		deleteOp("op-del", "item1", epoch.Add(time.Second)),
	}
	remote := []model.Operation{
		updateOp("op-upd", "item1", "updated content", epoch.Add(2*time.Second)),
	}
	merged := engine.Merge(local, remote)
	// Merge should have 3 ops, no conflict logic applied
	assert.Len(t, merged, 3)

	state := engine.Replay(merged)
	require.Len(t, state, 1)
	assert.True(t, state["item1"].Deleted, "delete must win after Merge+Replay")
}

// ─── Merge tests ──────────────────────────────────────────────────────────────

func TestMerge_EmptyInputs(t *testing.T) {
	merged := engine.Merge(nil, nil)
	assert.Empty(t, merged)
}

func TestMerge_LocalOnly(t *testing.T) {
	local := []model.Operation{addOp("op1", "item1", "hello", epoch)}
	merged := engine.Merge(local, nil)
	assert.Len(t, merged, 1)
	assert.Equal(t, "op1", merged[0].OperationID)
}

func TestMerge_RemoteOnly(t *testing.T) {
	remote := []model.Operation{addOp("op1", "item1", "hello", epoch)}
	merged := engine.Merge(nil, remote)
	assert.Len(t, merged, 1)
	assert.Equal(t, "op1", merged[0].OperationID)
}

func TestMerge_Deduplication_NoDuplicates(t *testing.T) {
	// AC-3: Merge(local, remote) where remote contains IDs already in local
	// produces a slice with no duplicate IDs.
	op := addOp("op1", "item1", "hello", epoch)
	local := []model.Operation{op}
	remote := []model.Operation{op} // same op
	merged := engine.Merge(local, remote)
	assert.Len(t, merged, 1, "duplicate op ID must be deduplicated")
}

func TestMerge_DeduplicatesWithinLocalInput(t *testing.T) {
	op := addOp("op1", "item1", "hello", epoch)
	merged := engine.Merge([]model.Operation{op, op}, nil)
	require.Len(t, merged, 1)
	assert.Equal(t, "op1", merged[0].OperationID)
}

func TestMerge_DeduplicatesWithinRemoteInput(t *testing.T) {
	op := addOp("op1", "item1", "hello", epoch)
	merged := engine.Merge(nil, []model.Operation{op, op})
	require.Len(t, merged, 1)
	assert.Equal(t, "op1", merged[0].OperationID)
}

func TestMerge_EmptyOperationIDDeduplication(t *testing.T) {
	// Edge case from phase plan: empty OperationID still participates in seen-set.
	op1 := addOp("", "item1", "hello", epoch)
	op2 := addOp("", "item2", "world", epoch.Add(time.Second))
	merged := engine.Merge([]model.Operation{op1}, []model.Operation{op2})

	// Empty ID collides by design for MVP; first one is kept.
	require.Len(t, merged, 1)
	assert.Equal(t, "item1", merged[0].ItemID)
}

func TestMerge_LocalOrderPreserved_RemoteAppended(t *testing.T) {
	local := []model.Operation{
		addOp("op1", "item1", "a", epoch),
		addOp("op2", "item2", "b", epoch),
	}
	remote := []model.Operation{
		addOp("op3", "item3", "c", epoch),
	}
	merged := engine.Merge(local, remote)
	require.Len(t, merged, 3)
	assert.Equal(t, "op1", merged[0].OperationID)
	assert.Equal(t, "op2", merged[1].OperationID)
	assert.Equal(t, "op3", merged[2].OperationID)
}

func TestMerge_BothSidesFullyOverlap(t *testing.T) {
	// All ops already present on both sides → Merge returns same-length slice
	ops := []model.Operation{
		addOp("op1", "item1", "a", epoch),
		addOp("op2", "item2", "b", epoch),
	}
	merged := engine.Merge(ops, ops)
	assert.Len(t, merged, 2)
}

// ─── Scenario tests ───────────────────────────────────────────────────────────

func TestScenario1_IndependentAdds_BothVisible(t *testing.T) {
	// Spec Scenario 1: Device A adds item X, Device B adds item Y.
	// After merge, both devices see both items.
	local := []model.Operation{
		addOp("op-a", "itemX", "buy oat milk", epoch),
	}
	remote := []model.Operation{
		addOp("op-b", "itemY", "finish the RFC", epoch.Add(time.Second)),
	}
	merged := engine.Merge(local, remote)
	state := engine.Replay(merged)

	assert.Len(t, state, 2)
	assert.Contains(t, state, "itemX")
	assert.Contains(t, state, "itemY")
	assert.False(t, state["itemX"].Deleted)
	assert.False(t, state["itemY"].Deleted)
}

func TestScenario2_DeleteVsUpdate_DeleteWins(t *testing.T) {
	// Spec Scenario 2: Device A deletes item X; Device B updates item X with
	// new content. After merge, both see item X as deleted.
	local := []model.Operation{
		addOp("op-add", "itemX", "original", epoch),
		deleteOp("op-del", "itemX", epoch.Add(time.Second)),
	}
	remote := []model.Operation{
		updateOp("op-upd", "itemX", "new content from B", epoch.Add(2*time.Second)),
	}
	// Merge from A's perspective (A has local, gets B's remote)
	mergedA := engine.Merge(local, remote)
	stateA := engine.Replay(mergedA)
	require.Len(t, stateA, 1)
	assert.True(t, stateA["itemX"].Deleted, "Device A must see itemX deleted")

	// Merge from B's perspective (B has remote+add, gets A's ops)
	localB := []model.Operation{
		addOp("op-add", "itemX", "original", epoch),
		updateOp("op-upd", "itemX", "new content from B", epoch.Add(2*time.Second)),
	}
	remoteA := []model.Operation{
		deleteOp("op-del", "itemX", epoch.Add(time.Second)),
	}
	mergedB := engine.Merge(localB, remoteA)
	stateB := engine.Replay(mergedB)
	require.Len(t, stateB, 1)
	assert.True(t, stateB["itemX"].Deleted, "Device B must see itemX deleted after sync")
}

// ─── Idempotency & large-scale tests ─────────────────────────────────────────

func TestReplay_Idempotent(t *testing.T) {
	// AC-3: Replay called with a deduplicated slice produces the same state
	// regardless of how many times it is called.
	ops := []model.Operation{
		addOp("op1", "item1", "hello", epoch),
		updateOp("op2", "item1", "world", epoch.Add(time.Second)),
		deleteOp("op3", "item2", epoch),         // DELETE appears before ADD in op order (same timestamp)
		addOp("op4", "item2", "present", epoch), // ADD follows in op order; position-based rule means DELETE is orphan
	}
	// op3 DELETE for item2 arrives before op4 ADD (out-of-order)
	state1 := engine.Replay(ops)
	state2 := engine.Replay(ops)
	assert.Equal(t, state1, state2)
}

func TestReplay_InterleavedItems_WithOrphans(t *testing.T) {
	// Multiple interleaved items in one slice, including orphan ops.
	ops := []model.Operation{
		addOp("op1", "itemA", "a0", epoch),
		updateOp("op2", "itemB", "ghost", epoch.Add(time.Second)), // orphan UPDATE
		addOp("op3", "itemB", "b0", epoch.Add(2*time.Second)),
		updateOp("op4", "itemA", "a1", epoch.Add(3*time.Second)),
		deleteOp("op5", "itemB", epoch.Add(4*time.Second)),
		deleteOp("op6", "itemC", epoch.Add(5*time.Second)), // orphan DELETE
	}

	state := engine.Replay(ops)
	require.Len(t, state, 2)

	assert.Equal(t, "a1", state["itemA"].Content)
	assert.False(t, state["itemA"].Deleted)

	assert.True(t, state["itemB"].Deleted)
	assert.NotContains(t, state, "itemC")
}

func TestReplay_EmptyItemIDBehavior(t *testing.T) {
	// Engine does not validate IDs; it applies rules consistently even for empty item IDs.
	ops := []model.Operation{
		updateOp("op1", "", "ghost", epoch), // orphan update for empty key
		addOp("op2", "", "base", epoch.Add(time.Second)),
		updateOp("op3", "", "latest", epoch.Add(2*time.Second)),
	}

	state := engine.Replay(ops)
	require.Len(t, state, 1)
	item, ok := state[""]
	require.True(t, ok)
	assert.Equal(t, "", item.ID)
	assert.Equal(t, "latest", item.Content)
	assert.False(t, item.Deleted)
}

func TestMerge_100Ops_StateMatchesUnionReplay(t *testing.T) {
	// 100 ops across two devices — merged state must match independent replay
	// of the full union.
	const n = 50
	var localOps, remoteOps []model.Operation
	for i := 0; i < n; i++ {
		itemID := fmt.Sprintf("item-local-%d", i)
		localOps = append(localOps, addOp(fmt.Sprintf("op-l-%d", i), itemID, fmt.Sprintf("content %d", i), epoch.Add(time.Duration(i)*time.Second)))
	}
	for i := 0; i < n; i++ {
		itemID := fmt.Sprintf("item-remote-%d", i)
		remoteOps = append(remoteOps, addOp(fmt.Sprintf("op-r-%d", i), itemID, fmt.Sprintf("content %d", i), epoch.Add(time.Duration(i)*time.Second)))
	}

	merged := engine.Merge(localOps, remoteOps)
	assert.Len(t, merged, 2*n, "100 unique ops must all be present after merge")

	// Independent union replay
	union := append(append([]model.Operation{}, localOps...), remoteOps...)
	stateUnion := engine.Replay(union)
	stateMerged := engine.Replay(merged)

	// Both must contain the same item IDs with the same content
	assert.Equal(t, sortedIDs(stateUnion), sortedIDs(stateMerged))
	for id, item := range stateUnion {
		assert.Equal(t, item, stateMerged[id], "item %s differs", id)
	}
}
