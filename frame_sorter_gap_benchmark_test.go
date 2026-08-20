package quic

import (
	"testing"

	"github.com/apernet/quic-go/internal/protocol"
)

func BenchmarkFrameSorterPushManyGaps(b *testing.B) {
	separator := make([]byte, benchmarkFrameSize)
	partial := make([]byte, benchmarkFrameSize/2)
	b.ReportAllocs()
	b.SetBytes(int64(benchmarkFrameCount * (len(separator) + len(partial))))

	for b.Loop() {
		sorter := newFrameSorter()
		for index := range benchmarkFrameCount {
			offset := protocol.ByteCount((2*index + 1) * benchmarkFrameSize)
			if err := sorter.Push(separator, offset, nil); err != nil {
				b.Fatal(err)
			}
		}
		for index := range benchmarkFrameCount {
			offset := protocol.ByteCount(2 * index * benchmarkFrameSize)
			if err := sorter.Push(partial, offset, nil); err != nil {
				b.Fatal(err)
			}
		}
		if sorter.gapTree.Len() != benchmarkFrameCount+1 {
			b.Fatalf("gap count = %d, want %d", sorter.gapTree.Len(), benchmarkFrameCount+1)
		}
	}
}

func BenchmarkFrameSorterPushMaxGaps(b *testing.B) {
	payload := make([]byte, 6)
	b.ReportAllocs()
	b.SetBytes(int64(protocol.MaxStreamFrameSorterGaps * len(payload)))

	for b.Loop() {
		sorter := newFrameSorter()
		for index := range protocol.MaxStreamFrameSorterGaps {
			offset := protocol.ByteCount(index * 7)
			if err := sorter.Push(payload, offset, nil); err != nil {
				b.Fatal(err)
			}
		}
		if sorter.gapTree.Len() != protocol.MaxStreamFrameSorterGaps {
			b.Fatalf("gap count = %d, want %d", sorter.gapTree.Len(), protocol.MaxStreamFrameSorterGaps)
		}
	}
}
