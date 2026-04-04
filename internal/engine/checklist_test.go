package engine_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"testing"
	"time"

	"github.com/dHarshMakwana/lfess/internal/engine"
	"github.com/dHarshMakwana/lfess/internal/model"
	"github.com/stretchr/testify/require"
)

type checklistStateItem struct {
	ID      string `json:"id"`
	Content string `json:"content"`
	Deleted bool   `json:"deleted"`
}

func checklistStateHash(state map[string]model.Item) string {
	ids := make([]string, 0, len(state))
	for id := range state {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	canonical := make([]checklistStateItem, 0, len(ids))
	for _, id := range ids {
		item := state[id]
		canonical = append(canonical, checklistStateItem{
			ID:      id,
			Content: item.Content,
			Deleted: item.Deleted,
		})
	}

	b, _ := json.Marshal(canonical)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func checklistOpSetHash(ops []model.Operation) string {
	ids := make(map[string]struct{}, len(ops))
	for _, op := range ops {
		ids[op.OperationID] = struct{}{}
	}
	sorted := make([]string, 0, len(ids))
	for id := range ids {
		sorted = append(sorted, id)
	}
	sort.Strings(sorted)

	b, _ := json.Marshal(sorted)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func TestChecklistT3_OutOfOrderUpdateBeforeAdd(t *testing.T) {
	t1 := time.Date(2026, 1, 1, 0, 0, 1, 0, time.UTC)
	t2 := t1.Add(time.Second)

	o1 := addOp("O1", "itemX", "v1", t1)
	o2 := updateOp("O2", "itemX", "v2", t2)

	state := engine.Replay([]model.Operation{o2, o1})
	require.Contains(t, state, "itemX")
	require.Equal(t, "v2", state["itemX"].Content)
	require.False(t, state["itemX"].Deleted)

	h := checklistStateHash(state)
	for i := 0; i < 25; i++ {
		replayed := engine.Replay([]model.Operation{o2, o1})
		require.Equal(t, h, checklistStateHash(replayed))
	}
}

func TestChecklistT5_SameOpSetDifferentMergeOrderConverges(t *testing.T) {
	t1 := time.Date(2026, 1, 1, 0, 0, 1, 0, time.UTC)
	t2 := t1.Add(time.Second)

	oDel := deleteOp("Odel", "itemY", t2)
	oAdd := addOp("Oadd", "itemY", "v1", t1)

	nodeX := engine.Merge([]model.Operation{oDel}, []model.Operation{oAdd})
	nodeY := engine.Merge([]model.Operation{oAdd}, []model.Operation{oDel})

	require.Equal(t, checklistOpSetHash(nodeX), checklistOpSetHash(nodeY))
	require.Equal(t, checklistStateHash(engine.Replay(nodeX)), checklistStateHash(engine.Replay(nodeY)))
}

func TestChecklistT6_EqualTimestampConcurrentUpdatesDeterministic(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Second)

	o0 := addOp("O0", "itemZ", "base", t0)
	o1 := updateOp("O1", "itemZ", "A", t1)
	o2 := updateOp("O2", "itemZ", "B", t1)

	orders := [][]model.Operation{
		{o0, o1, o2},
		{o0, o2, o1},
		{o1, o0, o2},
		{o2, o1, o0},
	}

	var baselineHash string
	var winner string
	for i := 0; i < 100; i++ {
		state := engine.Replay(orders[i%len(orders)])
		require.Contains(t, state, "itemZ")

		h := checklistStateHash(state)
		if i == 0 {
			baselineHash = h
			winner = state["itemZ"].Content
			continue
		}
		require.Equal(t, baselineHash, h)
		require.Equal(t, winner, state["itemZ"].Content)
	}

	require.Equal(t, "B", winner)
}
