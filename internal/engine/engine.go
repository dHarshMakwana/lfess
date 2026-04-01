package engine

import (
	"sort"

	"github.com/dHarshMakwana/lfess/internal/model"
)

// Merge combines two op slices, deduplicates by OperationID, and returns the
// result in append order (local first, then remote ops not already seen).
// This is the only place deduplication occurs.
func Merge(local, remote []model.Operation) []model.Operation {
	seen := make(map[string]struct{}, len(local)+len(remote))
	out := make([]model.Operation, 0, len(local)+len(remote))

	for _, op := range local {
		if _, exists := seen[op.OperationID]; exists {
			continue
		}
		seen[op.OperationID] = struct{}{}
		out = append(out, op)
	}

	for _, op := range remote {
		if _, exists := seen[op.OperationID]; !exists {
			seen[op.OperationID] = struct{}{}
			out = append(out, op)
		}
	}
	return out
}

// Replay builds current item state from an already-merged (deduplicated) op
// slice. The caller is responsible for deduplication (i.e. the output of
// Merge). Replay does NOT call Merge internally.
//
// Rules (enforced here; Merge is conflict-logic-free):
//  1. If no ADD exists for an ItemID → the item does not appear in state
//     (orphan UPDATEs and DELETEs are silently ignored).
//  2. If an ADD exists and a DELETE that follows an ADD exists → Item{Deleted: true}.
//     A DELETE is only "effective" when at least one ADD precedes it in op order;
//     a DELETE that arrives before any ADD for the same ItemID is treated as an
//     orphan and does not suppress a subsequent ADD (AC-4 out-of-order import).
//  3. Otherwise → content is taken from the ADD, then UPDATEs are applied in
//     ascending Timestamp order (last UPDATE wins within a single item).
func Replay(ops []model.Operation) map[string]model.Item {
	type itemState struct {
		hasADD          bool
		effectiveDelete bool // DELETE seen after at least one ADD in op order
		content         string
		updates         []model.Operation
	}

	states := make(map[string]*itemState)

	for _, op := range ops {
		st, ok := states[op.ItemID]
		if !ok {
			st = &itemState{}
			states[op.ItemID] = st
		}
		switch op.Type {
		case model.OperationAdd:
			st.hasADD = true
			if c, ok := op.Payload["content"].(string); ok {
				st.content = c
			}
		case model.OperationUpdate:
			st.updates = append(st.updates, op)
		case model.OperationDelete:
			// Only effective when an ADD already exists in op order; otherwise
			// this is an orphan DELETE (out-of-order import) and is ignored.
			if st.hasADD {
				st.effectiveDelete = true
			}
		}
	}

	out := make(map[string]model.Item, len(states))
	for itemID, st := range states {
		if !st.hasADD {
			// Rule 1: no ADD → item does not appear in state
			continue
		}
		if st.effectiveDelete {
			// Rule 2: delete wins
			out[itemID] = model.Item{ID: itemID, Deleted: true}
			continue
		}
		// Rule 3: apply UPDATEs in ascending Timestamp order
		sort.Slice(st.updates, func(i, j int) bool {
			return st.updates[i].Timestamp.Before(st.updates[j].Timestamp)
		})
		content := st.content
		for _, u := range st.updates {
			if c, ok := u.Payload["content"].(string); ok {
				content = c
			}
		}
		out[itemID] = model.Item{ID: itemID, Content: content}
	}
	return out
}
