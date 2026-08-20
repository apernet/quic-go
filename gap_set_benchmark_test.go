package quic

import (
	"testing"

	"github.com/apernet/quic-go/internal/protocol"
	"github.com/apernet/quic-go/internal/utils"
)

func BenchmarkGapSetMatchMaxGaps(b *testing.B) {
	set := newGapSetWithSparseGaps(b, protocol.MaxStreamFrameSorterGaps)
	matches := make([]utils.ByteInterval, 0, protocol.MaxStreamFrameSorterGaps)
	query := utils.ByteInterval{Start: 0, End: protocol.MaxByteCount}
	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		matches = set.MatchInto(query, matches[:0])
		if len(matches) != protocol.MaxStreamFrameSorterGaps {
			b.Fatalf("matched %d gaps, want %d", len(matches), protocol.MaxStreamFrameSorterGaps)
		}
	}
}

func BenchmarkGapSetUpdateMaxGaps(b *testing.B) {
	set := newGapSetWithSparseGaps(b, protocol.MaxStreamFrameSorterGaps)
	middle := protocol.MaxStreamFrameSorterGaps / 2
	start := protocol.ByteCount(middle * 10)
	oldGap := utils.ByteInterval{Start: start, End: start + 5}
	newGap := utils.ByteInterval{Start: start + 1, End: start + 5}
	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		if !set.UpdatePreservingOrder(oldGap, newGap) {
			b.Fatal("failed to move gap boundary forward")
		}
		if !set.UpdatePreservingOrder(newGap, oldGap) {
			b.Fatal("failed to restore gap boundary")
		}
	}
}

func BenchmarkGapSetInsertMaxGapsShuffled(b *testing.B) {
	b.ReportAllocs()
	b.SetBytes(int64(protocol.MaxStreamFrameSorterGaps * 16))

	for b.Loop() {
		set := &gapSet{hint: -1}
		for step := range protocol.MaxStreamFrameSorterGaps {
			index := (step * 7919) % protocol.MaxStreamFrameSorterGaps
			start := protocol.ByteCount(index * 10)
			set.Insert(utils.ByteInterval{Start: start, End: start + 5})
		}
		if set.Len() != protocol.MaxStreamFrameSorterGaps {
			b.Fatalf("gap count = %d, want %d", set.Len(), protocol.MaxStreamFrameSorterGaps)
		}
	}
}

func BenchmarkGapSetInsertDeleteMaxGapsShuffled(b *testing.B) {
	b.ReportAllocs()
	b.SetBytes(int64(protocol.MaxStreamFrameSorterGaps * 32))

	for b.Loop() {
		set := &gapSet{hint: -1}
		for step := range protocol.MaxStreamFrameSorterGaps {
			index := (step * 7919) % protocol.MaxStreamFrameSorterGaps
			start := protocol.ByteCount(index * 10)
			set.Insert(utils.ByteInterval{Start: start, End: start + 5})
		}
		for step := range protocol.MaxStreamFrameSorterGaps {
			index := (step * 6151) % protocol.MaxStreamFrameSorterGaps
			start := protocol.ByteCount(index * 10)
			set.Delete(utils.ByteInterval{Start: start, End: start + 5})
		}
		if set.Len() != 0 {
			b.Fatalf("gap count = %d, want 0", set.Len())
		}
	}
}

func newGapSetWithSparseGaps(tb testing.TB, count int) *gapSet {
	tb.Helper()
	set := &gapSet{hint: -1}
	for index := range count {
		start := protocol.ByteCount(index * 10)
		set.Insert(utils.ByteInterval{Start: start, End: start + 5})
	}
	return set
}
