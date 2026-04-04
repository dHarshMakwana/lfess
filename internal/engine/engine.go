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
//  1. If no ADD exists for an ItemID, the item does not appear in state.
//  2. A DELETE is effective only after at least one ADD in canonical op order.
//  3. Canonical op order is deterministic per item:
//     Timestamp asc, then OperationID asc, then DeviceID asc, then Type asc.
//     This prevents merge-order-dependent replay outcomes.
//  4. If an effective DELETE exists, the item is returned as deleted.
//  5. Otherwise, ADD sets base content and UPDATE overwrites content while item
//     exists in canonical order.
func Replay(ops []model.Operation) map[string]model.Item {
	byItem := make(map[string][]model.Operation)
	for _, op := range ops {
		byItem[op.ItemID] = append(byItem[op.ItemID], op)
	}

	out := make(map[string]model.Item, len(byItem))
	for itemID, itemOps := range byItem {
		sort.Slice(itemOps, func(i, j int) bool {
			left := itemOps[i]
			right := itemOps[j]

			if !left.Timestamp.Equal(right.Timestamp) {
				return left.Timestamp.Before(right.Timestamp)
			}
			if left.OperationID != right.OperationID {
				return left.OperationID < right.OperationID
			}
			if left.DeviceID != right.DeviceID {
				return left.DeviceID < right.DeviceID
			}
			return string(left.Type) < string(right.Type)
		})

		hasADD := false
		effectiveDelete := false
		content := ""

		for _, op := range itemOps {
			switch op.Type {
			case model.OperationAdd:
				hasADD = true
				if c, ok := op.Payload["content"].(string); ok {
					content = c
				}
			case model.OperationUpdate:
				if !hasADD {
					continue
				}
				if c, ok := op.Payload["content"].(string); ok {
					content = c
				}
			case model.OperationDelete:
				if hasADD {
					effectiveDelete = true
				}
			}
		}

		if !hasADD {
			// Rule 1: no ADD → item does not appear in state
			continue
		}
		if effectiveDelete {
			// Rule 2: delete wins
			out[itemID] = model.Item{ID: itemID, Deleted: true}
			continue
		}
		out[itemID] = model.Item{ID: itemID, Content: content}
	}
	return out
}
