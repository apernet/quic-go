package quic

import (
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/apernet/quic-go/internal/protocol"
	"github.com/apernet/quic-go/internal/utils"
	"github.com/apernet/quic-go/internal/utils/tree"
)

func newTestGapSet() *gapSet {
	return &gapSet{hint: -1}
}

func (s *gapSet) Head() *utils.ByteInterval {
	if s.length == 0 {
		return nil
	}
	if s.blocks == nil {
		return &s.inline[0]
	}
	return &s.blocks[0].gaps[0]
}

func (s *gapSet) Values() []utils.ByteInterval {
	values := make([]utils.ByteInterval, 0, s.length)
	if s.blocks == nil {
		return append(values, s.inline[:s.length]...)
	}
	for _, block := range s.blocks {
		values = append(values, block.values()...)
	}
	return values
}

func TestGapSetMatchesTouchingIntervalsInOrder(t *testing.T) {
	set := newTestGapSet()
	set.Insert(utils.ByteInterval{Start: 40, End: 50})
	set.Insert(utils.ByteInterval{Start: 0, End: 10})
	set.Insert(utils.ByteInterval{Start: 20, End: 30})

	prefix := []utils.ByteInterval{{Start: 100, End: 110}}
	got := set.MatchInto(utils.ByteInterval{Start: 10, End: 40}, prefix)
	require.Equal(t, []utils.ByteInterval{
		{Start: 100, End: 110},
		{Start: 0, End: 10},
		{Start: 20, End: 30},
		{Start: 40, End: 50},
	}, got)
}

func TestGapSetUpdatePreservingOrderRejectsCrossingNeighbor(t *testing.T) {
	set := newTestGapSet()
	for _, interval := range []utils.ByteInterval{
		{Start: 0, End: 10},
		{Start: 20, End: 30},
		{Start: 40, End: 50},
	} {
		set.Insert(interval)
	}

	require.True(t, set.UpdatePreservingOrder(
		utils.ByteInterval{Start: 20, End: 30},
		utils.ByteInterval{Start: 21, End: 31},
	))
	require.False(t, set.UpdatePreservingOrder(
		utils.ByteInterval{Start: 21, End: 31},
		utils.ByteInterval{Start: 41, End: 45},
	))
	require.False(t, set.UpdatePreservingOrder(
		utils.ByteInterval{Start: 60, End: 70},
		utils.ByteInterval{Start: 61, End: 71},
	))
	require.Equal(t, []utils.ByteInterval{
		{Start: 0, End: 10},
		{Start: 21, End: 31},
		{Start: 40, End: 50},
	}, set.Values())
}

func TestGapSetPromotesAndKeepsSortedValues(t *testing.T) {
	set := newTestGapSet()
	count := gapBlockCapacity*3 + 5
	for index := count - 1; index >= 0; index-- {
		start := protocol.ByteCount(index * 4)
		set.Insert(utils.ByteInterval{Start: start, End: start + 2})
	}

	require.Equal(t, count, set.Len())
	for index, interval := range set.Values() {
		require.Equal(t, protocol.ByteCount(index*4), interval.Start)
	}

	for index := 0; index < count; index += 3 {
		start := protocol.ByteCount(index * 4)
		set.Delete(utils.ByteInterval{Start: start, End: start + 2})
	}
	require.Equal(t, count-(count+2)/3, set.Len())
	require.Equal(t, protocol.ByteCount(4), set.Head().Start)
}

func TestGapSetUpdatesAcrossLeafBoundaryAndShrinksToInlineSize(t *testing.T) {
	set := newTestGapSet()
	for index := range gapBlockCapacity*2 + 3 {
		start := protocol.ByteCount(index * 10)
		set.Insert(utils.ByteInterval{Start: start, End: start + 5})
	}

	oldBoundary := set.blocks[0].last()
	newBoundary := utils.ByteInterval{Start: oldBoundary.Start + 1, End: oldBoundary.End}
	require.True(t, set.UpdatePreservingOrder(oldBoundary, newBoundary))
	require.Contains(t, set.Values(), newBoundary)

	for set.Len() > gapInlineCapacity {
		set.Delete(set.Values()[0])
	}
	values := set.Values()
	require.Len(t, values, gapInlineCapacity)
	require.Equal(t, values, set.MatchInto(utils.ByteInterval{Start: 0, End: protocol.MaxByteCount}, nil))
}

func TestGapSetMatchesAVLReferenceAfterMutations(t *testing.T) {
	set := newTestGapSet()
	reference := tree.New[utils.ByteInterval]()
	const count = 257
	order := rand.New(rand.NewPCG(7, 11)).Perm(count)
	for _, index := range order {
		start := protocol.ByteCount(index * 10)
		interval := utils.ByteInterval{Start: start, End: start + 5}
		set.Insert(interval)
		reference.Insert(interval)
	}

	for index := 0; index < count; index += 5 {
		start := protocol.ByteCount(index * 10)
		oldInterval := utils.ByteInterval{Start: start, End: start + 5}
		newInterval := utils.ByteInterval{Start: start + 1, End: start + 5}
		require.True(t, set.UpdatePreservingOrder(oldInterval, newInterval))
		reference.Delete(oldInterval)
		reference.Insert(newInterval)
	}
	for index := 2; index < count; index += 7 {
		start := protocol.ByteCount(index * 10)
		interval := utils.ByteInterval{Start: start, End: start + 5}
		set.Delete(interval)
		reference.Delete(interval)
	}

	require.Equal(t, reference.Values(), set.Values())
	rng := rand.New(rand.NewPCG(13, 17))
	for range 1_000 {
		start := protocol.ByteCount(rng.IntN(count * 10))
		query := utils.ByteInterval{Start: start, End: start + protocol.ByteCount(rng.IntN(40))}
		require.Equal(t, reference.Match(query), set.MatchInto(query, nil), "query %v", query)
	}
}
